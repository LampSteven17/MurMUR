package config

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ExportResult describes the hand-off archive ExportOperatorFolder produced,
// suitable for surfacing in the TUI's add-user secret modal. The staging
// folder is zipped and then removed — the zip is the only artifact left.
type ExportResult struct {
	ZipPath string // single hand-off archive ("<destDir>.zip")
	Folder  string // top-level directory name inside the archive
}

// ExportOperatorFolder lays out a turnkey starter kit for a freshly-created
// operator and zips it into a single hand-off archive: a copy of cluster.yaml
// (which already contains the operator's users: entry, written by
// AppendUserToFile), a minimal cluster.env with only their identity + token +
// placeholder hints for any other vars the admin's cluster.env declared, the
// murmur binary itself, and a README with launch instructions.
//
//   srcConfigPath  — absolute path to the admin's cluster.yaml
//   newUser        — the freshly-created config.User (token_secret is the
//                    raw ${VAR} reference, e.g. "${BOB_TOKEN}")
//   tokenValue     — the freshly-minted token secret (the UUID); this is
//                    written verbatim into the new operator's cluster.env
//                    so they can launch immediately
//   destDir        — where to materialise the staging folder (defaults are
//                    computed by DefaultOperatorExportDir if blank); the zip
//                    is written as "<destDir>.zip"
//   binaryPath     — path to the murmur binary to bundle (callers pass the
//                    running binary via os.Executable). Empty skips it — the
//                    README then tells the operator to drop one in.
//
// Refuses if either the staging folder or the zip already exists (don't
// clobber existing operator state). Returns paths to the folder and the zip.
func ExportOperatorFolder(srcConfigPath string, newUser User, tokenValue, destDir, binaryPath string) (ExportResult, error) {
	if destDir == "" {
		destDir = DefaultOperatorExportDir(srcConfigPath, newUser.Name)
	}
	abs, err := filepath.Abs(destDir)
	if err != nil {
		return ExportResult{}, fmt.Errorf("export: resolve %s: %w", destDir, err)
	}
	zipPath := abs + ".zip"
	for _, p := range []string{abs, zipPath} {
		if _, err := os.Stat(p); err == nil {
			return ExportResult{}, fmt.Errorf("export: %s already exists — refusing to overwrite", p)
		} else if !os.IsNotExist(err) {
			return ExportResult{}, fmt.Errorf("export: stat %s: %w", p, err)
		}
	}
	if err := os.MkdirAll(abs, 0755); err != nil {
		return ExportResult{}, fmt.Errorf("export: mkdir %s: %w", abs, err)
	}

	// 1. Copy cluster.yaml — it already has the new user's entry appended by
	// the writeback that ran just before us.
	yamlBytes, err := os.ReadFile(srcConfigPath)
	if err != nil {
		return ExportResult{}, fmt.Errorf("export: read cluster.yaml: %w", err)
	}
	yamlDest := filepath.Join(abs, "cluster.yaml")
	if err := os.WriteFile(yamlDest, yamlBytes, 0644); err != nil {
		return ExportResult{}, fmt.Errorf("export: write cluster.yaml: %w", err)
	}

	// 2. Synthesise cluster.env. We DON'T copy admin's — it holds other
	// operators' secrets. The loader eagerly substitutes every ${VAR} in
	// cluster.yaml and fails loudly on undefined ones, so the kit must DEFINE
	// each referenced var or it won't even load. We emit the operator's own
	// token (real value), SSH_IDENTITY (a default path), and every other
	// referenced var empty — admin's values are never echoed; the operator
	// fills in only what they actually need.
	envDest := filepath.Join(abs, "cluster.env")
	referencedVars := scanConfigVarRefs(string(yamlBytes))
	envContent := renderOperatorEnv(newUser, tokenValue, referencedVars)
	if err := os.WriteFile(envDest, []byte(envContent), 0600); err != nil {
		return ExportResult{}, fmt.Errorf("export: write cluster.env: %w", err)
	}

	// 3. Bundle the murmur binary so the operator just unzips and runs.
	var binaryDest string
	if binaryPath != "" {
		binaryDest = filepath.Join(abs, "murmur")
		if err := copyFile(binaryPath, binaryDest, 0755); err != nil {
			return ExportResult{}, fmt.Errorf("export: copy murmur binary from %s: %w", binaryPath, err)
		}
	}

	// 4. Launch README.
	readmeDest := filepath.Join(abs, "README.md")
	if err := os.WriteFile(readmeDest, []byte(renderOperatorReadme(newUser, abs, binaryDest != "")), 0644); err != nil {
		return ExportResult{}, fmt.Errorf("export: write README.md: %w", err)
	}

	// 5. Zip the staging folder into the single hand-off artifact, then
	// remove the folder — the zip is the only thing the admin keeps.
	if err := zipDir(abs, zipPath); err != nil {
		return ExportResult{}, fmt.Errorf("export: zip %s: %w", abs, err)
	}
	if err := os.RemoveAll(abs); err != nil {
		return ExportResult{}, fmt.Errorf("export: remove staging folder %s: %w", abs, err)
	}

	return ExportResult{ZipPath: zipPath, Folder: filepath.Base(abs)}, nil
}

// DefaultOperatorExportDir computes the sibling-folder path for a new
// operator. Given srcConfigPath like "/home/u/TheLightLab/cluster.yaml" and
// name "bob", returns "/home/u/TheLightLab-bob".
func DefaultOperatorExportDir(srcConfigPath, name string) string {
	parent := filepath.Dir(srcConfigPath)
	abs, err := filepath.Abs(parent)
	if err != nil {
		abs = parent
	}
	return abs + "-" + name
}

// copyFile reads src and writes its bytes to dst with the given mode.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// zipDir writes a zip archive of srcDir to destZip. Entries are nested under
// a top-level folder named after srcDir's base so extraction yields a single
// clean directory rather than loose files. File modes are preserved, so the
// bundled murmur binary keeps its exec bit.
func zipDir(srcDir, destZip string) error {
	out, err := os.Create(destZip)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	base := filepath.Base(srcDir)
	walkErr := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name := filepath.ToSlash(filepath.Join(base, rel))
		if info.IsDir() {
			_, err := zw.Create(name + "/")
			return err
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = name
		hdr.Method = zip.Deflate
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
	if walkErr != nil {
		zw.Close()
		return walkErr
	}
	return zw.Close()
}

// singleVarRE matches a loader-substituted `${VAR}` reference. The `$$`
// escape (a runtime/shell var the loader leaves alone) is neutralised before
// matching so it isn't collected.
var singleVarRE = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// scanConfigVarRefs returns the sorted, unique set of ${VAR} names the loader
// will demand from the environment for this cluster.yaml. `$${VAR}` escapes
// are excluded — the loader passes those through to the guest shell, so they
// don't need to be defined in cluster.env.
func scanConfigVarRefs(yamlText string) []string {
	cleaned := strings.ReplaceAll(yamlText, "$$", "\x00") // neutralise $${...}
	seen := map[string]bool{}
	var out []string
	for _, m := range singleVarRE.FindAllStringSubmatch(cleaned, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

// varNameOf extracts NAME from a "${NAME}" reference, or "" if s isn't one.
func varNameOf(s string) string {
	m := varRE.FindStringSubmatch(strings.TrimSpace(s))
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

// renderOperatorEnv builds the kit's cluster.env. It must DEFINE every var the
// config references (the loader fails loudly on undefined ones): the operator's
// own token gets its real value, SSH_IDENTITY a default path, and every other
// referenced var is emitted empty — admin's values are never copied. The
// operator fills in only the empties they actually need.
func renderOperatorEnv(u User, tokenValue string, referencedVars []string) string {
	ownTokenVar := varNameOf(u.TokenSecret)
	if ownTokenVar == "" {
		ownTokenVar = suggestOperatorEnvName(u)
	}

	var b bytes.Buffer
	fmt.Fprintf(&b, "# Operator: %s\n", u.Name)
	fmt.Fprintf(&b, "# Generated by murmur on %s.\n", time.Now().UTC().Format("2006-01-02"))
	fmt.Fprintf(&b, "# DO NOT COMMIT — this file contains your API token secret.\n\n")
	fmt.Fprintf(&b, "MURMUR_USER=%s\n", u.Name)
	fmt.Fprintf(&b, "%s=%s\n", ownTokenVar, tokenValue)

	var empties []string
	hasSSH := false
	for _, v := range referencedVars {
		switch {
		case v == ownTokenVar:
			// already written with its real value
		case v == "SSH_IDENTITY":
			hasSSH = true
		default:
			empties = append(empties, v)
		}
	}
	if hasSSH {
		fmt.Fprintf(&b, "\n# Private key for guest access — its .pub is injected into guests you\n")
		fmt.Fprintf(&b, "# deploy, and the private half is used for patch/update. Point at your key.\n")
		fmt.Fprintf(&b, "SSH_IDENTITY=~/.ssh/id_rsa\n")
	}
	if len(empties) > 0 {
		fmt.Fprintf(&b, "\n# === Other vars cluster.yaml references — defined empty so the config\n")
		fmt.Fprintf(&b, "# loads. Fill any you need (e.g. to deploy apps that use them). Admin's\n")
		fmt.Fprintf(&b, "# values are NOT shown here — ask admin or the relevant service.\n")
		for _, k := range empties {
			fmt.Fprintf(&b, "%s=\n", k)
		}
	}
	return b.String()
}

func renderOperatorReadme(u User, dir string, hasBinary bool) string {
	envName := suggestOperatorEnvName(u)
	folder := filepath.Base(dir)
	var b bytes.Buffer
	fmt.Fprintf(&b, "# Operator setup — %s\n\n", u.Name)
	fmt.Fprintf(&b, "This kit was auto-generated by murmur for the new operator `%s`\n", u.Name)
	fmt.Fprintf(&b, "(role: `%s`). Unzip it anywhere and launch.\n\n", u.Role)
	fmt.Fprintf(&b, "## What's here\n\n")
	fmt.Fprintf(&b, "- `cluster.yaml` — full cluster config (same as admin's; safe to commit).\n")
	fmt.Fprintf(&b, "- `cluster.env`  — your token + identity. **DO NOT commit.**\n")
	if hasBinary {
		fmt.Fprintf(&b, "- `murmur`       — the murmur binary, built for the admin's OS/arch.\n")
	}
	fmt.Fprintf(&b, "- `README.md`    — this file.\n\n")
	fmt.Fprintf(&b, "## Launch\n\n")
	fmt.Fprintf(&b, "```\ncd %s\n", folder)
	if hasBinary {
		fmt.Fprintf(&b, "chmod +x ./murmur        # unzip may drop the exec bit\n")
	} else {
		fmt.Fprintf(&b, "cp /path/to/murmur ./murmur   # drop in a murmur binary first\n")
	}
	fmt.Fprintf(&b, "./murmur whoami\n./murmur status\n./murmur tui\n```\n\n")
	if hasBinary {
		fmt.Fprintf(&b, "The bundled binary matches the admin's platform. On a different\n")
		fmt.Fprintf(&b, "OS/arch, replace `murmur` with a matching build.\n\n")
	}
	fmt.Fprintf(&b, "Before launching, verify the `SSH_IDENTITY` path in `cluster.env`\n")
	fmt.Fprintf(&b, "(default: `~/.ssh/id_rsa`).\n\n")
	fmt.Fprintf(&b, "## Notes\n\n")
	fmt.Fprintf(&b, "- Your role grants the actions and tabs listed under `roles:` in cluster.yaml.\n")
	fmt.Fprintf(&b, "- Guests you deploy land in ProxMox pool `murmur-%s` with tag `murmur-owner-%s`.\n", u.Name, u.Name)
	fmt.Fprintf(&b, "- Your token (`%s`) is the only proof of identity — protect it like a password.\n", envName)
	fmt.Fprintf(&b, "- To rotate the token, ask an admin to run [r]otate from the [x]users tab.\n")
	if u.SSHPubKey != "" {
		fmt.Fprintf(&b, "- Guests you deploy will trust the `ssh_pubkey` recorded in cluster.yaml.\n")
		fmt.Fprintf(&b, "  Keep its matching **private** key at the `SSH_IDENTITY` path so patch/update can SSH in.\n")
	} else {
		fmt.Fprintf(&b, "- Guests you deploy use the key at `SSH_IDENTITY`: its `.pub` is injected at deploy,\n")
		fmt.Fprintf(&b, "  the private half is used for patch/update.\n")
	}
	return b.String()
}

func suggestOperatorEnvName(u User) string {
	upper := strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(u.Name))
	return upper + "_TOKEN"
}
