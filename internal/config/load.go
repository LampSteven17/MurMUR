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
	if err := substituteScalars(&root); err != nil {
		return nil, fmt.Errorf("interpolate config %s: %w", path, err)
	}

	var cfg Config
	if err := root.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config %s: %w", path, err)
	}

	cfg.Flavors = mergeFlavors(cfg.Flavors)
	cfg.Images = mergeImages(cfg.Images)
	cfg.Cluster.SSH.Users = mergeSSHUsers(cfg.Cluster.SSH.Users)

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
// Supports: comments (#), blank lines, optional `export ` prefix,
// optional single- or double-quoted values. No multi-line values.
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
		if len(val) >= 2 {
			first, last := val[0], val[len(val)-1]
			if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if err := os.Setenv(key, val); err != nil {
			return err
		}
	}
	return nil
}

var varRE = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// substituteScalars walks the YAML AST and replaces ${VAR} in every scalar
// value (string, int, bool — yaml.v3 reads them all as scalar nodes).
// Comments are stored separately on Node and are never visited.
// Missing variables fail loudly with the union of names.
func substituteScalars(n *yaml.Node) error {
	var missing []string
	walkScalars(n, func(s *yaml.Node) {
		orig := s.Value
		s.Value = varRE.ReplaceAllStringFunc(s.Value, func(m string) string {
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
