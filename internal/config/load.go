package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load resolves the config path, loads any sibling env file, substitutes
// ${VAR} references in YAML scalars, decodes, merges builtin catalogs,
// and validates. Returns the resolved absolute path alongside the config.
func Load() (string, *Config, error) {
	path, err := findConfig()
	if err != nil {
		return "", nil, err
	}
	cfg, err := LoadFile(path)
	if err != nil {
		return path, nil, err
	}
	return path, cfg, nil
}

// LoadFile loads an explicit config file path.
func LoadFile(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	if envPath := findEnvFile(path); envPath != "" {
		if err := loadEnvFile(envPath); err != nil {
			return nil, fmt.Errorf("load env file %s: %w", envPath, err)
		}
	}

	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	// Stash users[*].token_secret raw values BEFORE substituteScalars runs, so
	// that operators only need their own ${TOKEN_*} in their cluster.env.
	// We restore the raw values onto cfg.Users after decode; substitution is
	// then performed at identity-selection time on the active user only.
	deferred := stashUserTokenSecrets(&root)

	if err := substituteScalars(&root); err != nil {
		return nil, fmt.Errorf("interpolate config %s: %w", path, err)
	}

	var cfg Config
	if err := root.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config %s: %w", path, err)
	}

	cfg.Flavors = mergeFlavors(cfg.Flavors)
	cfg.Images = mergeImages(cfg.Images)
	cfg.Roles = mergeRoles(cfg.Roles)
	cfg.Cluster.SSH.Users = mergeSSHUsers(cfg.Cluster.SSH.Users)

	// Restore deferred raw token_secret values (keyed by user name).
	for i := range cfg.Users {
		if raw, ok := deferred[cfg.Users[i].Name]; ok {
			cfg.Users[i].TokenSecret = raw
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// findConfig walks $MURMUR_CONFIG, ./cluster.yaml, ~/.config/murmur/cluster.yaml.
func findConfig() (string, error) {
	if p := os.Getenv("MURMUR_CONFIG"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("MURMUR_CONFIG=%s but file not readable", p)
	}
	if _, err := os.Stat("cluster.yaml"); err == nil {
		return "cluster.yaml", nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".config", "murmur", "cluster.yaml")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New("no cluster.yaml found (set $MURMUR_CONFIG, or place it in cwd or ~/.config/murmur/)")
}

// findEnvFile checks $MURMUR_ENV, then `cluster.env` alongside the config.
func findEnvFile(configPath string) string {
	if p := os.Getenv("MURMUR_ENV"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		return ""
	}
	candidate := filepath.Join(filepath.Dir(configPath), "cluster.env")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

// loadEnvFile populates os.Setenv from a KEY=value file.
// Supports: full-line comments (#), inline `<space>#` trailing comments on
// unquoted values, blank lines, optional `export ` prefix, optional single-
// or double-quoted values (which suppress inline-comment stripping so the
// value can legitimately contain `#`). No multi-line values.
func loadEnvFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for i, line := range strings.Split(string(raw), "\n") {
		ln := strings.TrimSpace(line)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		ln = strings.TrimPrefix(ln, "export ")
		eq := strings.IndexByte(ln, '=')
		if eq <= 0 {
			return fmt.Errorf("%s:%d: malformed line (expected KEY=value)", path, i+1)
		}
		key := strings.TrimSpace(ln[:eq])
		val := strings.TrimSpace(ln[eq+1:])
		// Quoted-value handling first: if val starts with " or ', find the
		// matching closing quote, and treat anything after as optional
		// whitespace + inline comment. Inside the quotes, `#` is literal.
		if len(val) > 0 && (val[0] == '"' || val[0] == '\'') {
			quote := val[0]
			end := strings.IndexByte(val[1:], quote)
			if end < 0 {
				return fmt.Errorf("%s:%d: unterminated %c quote in value", path, i+1, quote)
			}
			content := val[1 : 1+end]
			tail := strings.TrimSpace(val[2+end:])
			if tail != "" && !strings.HasPrefix(tail, "#") {
				return fmt.Errorf("%s:%d: unexpected content after closing quote: %q", path, i+1, tail)
			}
			val = content
		} else if hash := indexInlineComment(val); hash >= 0 {
			// Unquoted: strip ` #...` tail. The whitespace-before-# guard
			// keeps `#` legitimately in values (passwords, URL fragments).
			val = strings.TrimRight(val[:hash], " \t")
		}
		if err := os.Setenv(key, val); err != nil {
			return err
		}
	}
	return nil
}

// indexInlineComment returns the index of the `#` that starts an inline
// comment (one preceded by ASCII whitespace), or -1 if none. The
// whitespace-prefix requirement avoids stripping a `#` that's part of the
// value itself (e.g. URL fragments, passwords containing `#`).
func indexInlineComment(s string) int {
	for i := 1; i < len(s); i++ {
		if s[i] == '#' && (s[i-1] == ' ' || s[i-1] == '\t') {
			return i
		}
	}
	return -1
}

// varRE matches `${VAR}` (loader-time substitution) and `$${VAR}` (escape:
// emits the literal `${VAR}` so the value can be expanded later by a shell
// that runs the rendered command). The leading `\$?` is captured by the
// optional prefix `$` in the match itself, not as a group.
var varRE = regexp.MustCompile(`\$\$?\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// substituteScalars walks the YAML AST and replaces ${VAR} in every scalar
// value (string, int, bool — yaml.v3 reads them all as scalar nodes).
// Comments are stored separately on Node and are never visited.
//
// `${VAR}`  ⇒ replaced with os.Getenv("VAR"); missing → loud error.
// `$${VAR}` ⇒ emitted as the literal `${VAR}` so post_deploy / update
//             commands can reference runtime env (secrets prompted at
//             deploy time, GUEST_IP, etc.) without the loader eagerly
//             substituting them. Without this escape, every shell ${…}
//             in cluster.yaml would either need a cluster.env entry or
//             would clash with operator-supplied secrets.
//
// Missing variables fail loudly with the union of names.
func substituteScalars(n *yaml.Node) error {
	var missing []string
	walkScalars(n, func(s *yaml.Node) {
		orig := s.Value
		s.Value = varRE.ReplaceAllStringFunc(s.Value, func(m string) string {
			if strings.HasPrefix(m, "$$") {
				// Escaped — strip one leading $ and pass through literally.
				return m[1:]
			}
			name := m[2 : len(m)-1]
			v, ok := os.LookupEnv(name)
			if !ok {
				missing = append(missing, name)
				return m
			}
			return v
		})
		// If the value changed, clear the tag so the decoder re-infers
		// the type from the new content (e.g. ${PORT}=8080 → !!int).
		if s.Value != orig {
			s.Tag = ""
		}
	})
	if len(missing) > 0 {
		return fmt.Errorf("undefined env vars: %s (set in cluster.env or shell env)", strings.Join(uniq(missing), ", "))
	}
	return nil
}

func walkScalars(n *yaml.Node, fn func(*yaml.Node)) {
	if n == nil {
		return
	}
	if n.Kind == yaml.ScalarNode {
		fn(n)
		return
	}
	for _, c := range n.Content {
		walkScalars(c, fn)
	}
}

// stashUserTokenSecrets walks the YAML AST, finds every entry under the
// top-level `users:` sequence, extracts each entry's `token_secret` raw
// value (preserving ${VAR} references), maps it by the entry's `name`, and
// replaces the AST scalar with an empty string. The caller restores the raw
// strings onto the decoded cfg.Users after substitution and decode. This is
// how we keep each operator's cluster.env scoped to their own token.
func stashUserTokenSecrets(root *yaml.Node) map[string]string {
	out := map[string]string{}
	top := root
	if top != nil && top.Kind == yaml.DocumentNode && len(top.Content) > 0 {
		top = top.Content[0]
	}
	if top == nil || top.Kind != yaml.MappingNode {
		return out
	}
	// Top-level mapping: keys are at even indices.
	var users *yaml.Node
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value == "users" {
			users = top.Content[i+1]
			break
		}
	}
	if users == nil || users.Kind != yaml.SequenceNode {
		return out
	}
	for _, entry := range users.Content {
		if entry.Kind != yaml.MappingNode {
			continue
		}
		var name, secret *yaml.Node
		for i := 0; i+1 < len(entry.Content); i += 2 {
			switch entry.Content[i].Value {
			case "name":
				name = entry.Content[i+1]
			case "token_secret":
				secret = entry.Content[i+1]
			}
		}
		if name == nil || secret == nil {
			continue
		}
		out[name.Value] = secret.Value
		secret.Value = ""
		secret.Tag = "!!str"
	}
	return out
}

func uniq(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
