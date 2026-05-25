package config

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportOperatorFolder_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	srcDir := filepath.Join(tmp, "lab")
	_ = os.MkdirAll(srcDir, 0755)

	srcYAML := filepath.Join(srcDir, "cluster.yaml")

	// Representative config: admin token via cluster.api, an ssh identity, a
	// shared app var, a $$-escaped runtime var (must NOT be collected), and
	// bob's own token.
	_ = os.WriteFile(srcYAML, []byte(`cluster:
  name: lab
  api:
    token_secret: ${TLL_ADMIN_MURMUR_TOKEN}
  ssh:
    identity: ${SSH_IDENTITY}
users:
  - name: bob
    role: deployer
    proxmox_user: bob@pve
    proxmox_token: murmur
    token_secret: ${BOB_TOKEN}
apps:
  - name: x
    post_deploy: |
      echo ${TWINGATE_NETWORK}
      echo $${RUNTIME_SECRET}
`), 0644)

	user := User{
		Name: "bob", Role: "deployer",
		ProxmoxUser: "bob@pve", ProxmoxToken: "murmur",
		TokenSecret: "${BOB_TOKEN}",
	}

	destDir := filepath.Join(tmp, "lab-bob")
	res, err := ExportOperatorFolder(srcYAML, user, "BOB-LIVE-SECRET", destDir, "")
	if err != nil {
		t.Fatal(err)
	}

	// Zip-only: the staging folder must be gone, the zip present.
	if _, err := os.Stat(destDir); !os.IsNotExist(err) {
		t.Errorf("staging folder %s should have been removed, stat err=%v", destDir, err)
	}
	if res.ZipPath != destDir+".zip" {
		t.Errorf("ZipPath got %q want %q", res.ZipPath, destDir+".zip")
	}

	// cluster.yaml is a verbatim copy.
	gotYAML := readZipEntry(t, res.ZipPath, "lab-bob/cluster.yaml")
	if !strings.Contains(gotYAML, "name: bob") {
		t.Errorf("exported cluster.yaml missing bob entry")
	}

	envStr := readZipEntry(t, res.ZipPath, "lab-bob/cluster.env")

	// Every loader-substituted ${VAR} must be DEFINED so the kit loads: bob's
	// own token real, SSH_IDENTITY defaulted, the rest empty. The $$-escaped
	// RUNTIME_SECRET must not appear.
	wantIn := []string{
		"MURMUR_USER=bob",
		"BOB_TOKEN=BOB-LIVE-SECRET",
		"SSH_IDENTITY=~/.ssh/id_rsa",
		"TLL_ADMIN_MURMUR_TOKEN=\n", // empty placeholder (admin value never copied)
		"TWINGATE_NETWORK=\n",       // empty placeholder
	}
	for _, w := range wantIn {
		if !strings.Contains(envStr, w) {
			t.Errorf("env missing line: %q\n--- env ---\n%s", w, envStr)
		}
	}
	mustNotContain := []string{
		"RUNTIME_SECRET",      // $$-escaped → not a loader var
		"# BOB_TOKEN=",        // bob's own token isn't a placeholder
		"# TLL_ADMIN",         // placeholders are uncommented (must load)
	}
	for _, s := range mustNotContain {
		if strings.Contains(envStr, s) {
			t.Errorf("env leaked or wrong: %q\n--- env ---\n%s", s, envStr)
		}
	}

	// README mentions the new operator's name + the extracted folder name
	// (base, not the admin's absolute path — the kit is portable).
	gotReadme := readZipEntry(t, res.ZipPath, "lab-bob/README.md")
	if !strings.Contains(gotReadme, "bob") || !strings.Contains(gotReadme, filepath.Base(destDir)) {
		t.Errorf("README missing name or folder")
	}
}

// Regression for the shipped-kit-that-can't-load bug: the kit's cluster.env
// must DEFINE every loader-substituted ${VAR} the cluster.yaml references (the
// loader fails loudly on undefined ones), exclude $$-escaped runtime vars, and
// carry the operator's own token with its real value.
func TestExportedKit_DefinesEveryReferencedVar(t *testing.T) {
	tmp := t.TempDir()
	srcDir := filepath.Join(tmp, "lab")
	_ = os.MkdirAll(srcDir, 0755)
	srcYAML := filepath.Join(srcDir, "cluster.yaml")
	_ = os.WriteFile(srcYAML, []byte(`cluster:
  api:
    token_secret: ${ADMIN_TOKEN}
  ssh:
    identity: ${SSH_IDENTITY}
users:
  - name: bob
    token_secret: ${BOB_TOKEN}
apps:
  - name: x
    post_deploy: |
      echo ${SHARED_A}
      echo ${SHARED_B}
      echo $${RUNTIME}
`), 0644)

	user := User{Name: "bob", TokenSecret: "${BOB_TOKEN}"}
	dest := filepath.Join(tmp, "lab-bob")
	res, err := ExportOperatorFolder(srcYAML, user, "real-secret", dest, "")
	if err != nil {
		t.Fatal(err)
	}

	env := readZipEntry(t, res.ZipPath, "lab-bob/cluster.env")
	defined := map[string]bool{}
	for _, line := range strings.Split(env, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.IndexByte(line, '='); i > 0 {
			defined[line[:i]] = true
		}
	}
	for _, v := range []string{"ADMIN_TOKEN", "SSH_IDENTITY", "BOB_TOKEN", "SHARED_A", "SHARED_B"} {
		if !defined[v] {
			t.Errorf("kit cluster.env missing required var %q\n--- env ---\n%s", v, env)
		}
	}
	if defined["RUNTIME"] {
		t.Errorf("$$-escaped RUNTIME must not be defined in kit env\n--- env ---\n%s", env)
	}
	if !strings.Contains(env, "BOB_TOKEN=real-secret") {
		t.Errorf("operator's own token must carry its real value\n--- env ---\n%s", env)
	}
}

// readZipEntry returns the contents of a single named entry in a zip archive,
// failing the test if it can't be opened or found.
func readZipEntry(t *testing.T, zipPath, name string) string {
	t.Helper()
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("open zip %s: %v", zipPath, err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open entry %s: %v", name, err)
		}
		defer rc.Close()
		b, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read entry %s: %v", name, err)
		}
		return string(b)
	}
	t.Fatalf("zip %s missing entry %q", zipPath, name)
	return ""
}

func TestExportOperatorFolder_RefusesIfDirExists(t *testing.T) {
	tmp := t.TempDir()
	srcYAML := filepath.Join(tmp, "cluster.yaml")
	_ = os.WriteFile(srcYAML, []byte("cluster:\n  name: c\n"), 0644)
	dest := filepath.Join(tmp, "occupied")
	_ = os.MkdirAll(dest, 0755)

	_, err := ExportOperatorFolder(srcYAML, User{Name: "alice"}, "secret", dest, "")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected 'already exists' error, got %v", err)
	}
}

func TestExportOperatorFolder_BundlesBinaryAndZips(t *testing.T) {
	tmp := t.TempDir()
	srcDir := filepath.Join(tmp, "lab")
	_ = os.MkdirAll(srcDir, 0755)
	srcYAML := filepath.Join(srcDir, "cluster.yaml")
	_ = os.WriteFile(srcYAML, []byte("cluster:\n  name: lab\n"), 0644)

	// Stand-in for the murmur binary, with the exec bit set.
	fakeBin := filepath.Join(tmp, "murmur-build")
	binContent := []byte("#!/bin/sh\necho murmur\n")
	_ = os.WriteFile(fakeBin, binContent, 0755)

	user := User{Name: "bob", Role: "deployer", ProxmoxUser: "bob@pve", ProxmoxToken: "murmur"}
	destDir := filepath.Join(tmp, "lab-bob")
	res, err := ExportOperatorFolder(srcYAML, user, "SECRET", destDir, fakeBin)
	if err != nil {
		t.Fatal(err)
	}

	// Staging folder removed; zip holds the binary bytes intact.
	if _, err := os.Stat(destDir); !os.IsNotExist(err) {
		t.Errorf("staging folder %s should have been removed", destDir)
	}
	if gotBin := readZipEntry(t, res.ZipPath, "lab-bob/murmur"); gotBin != string(binContent) {
		t.Errorf("bundled binary content mismatch")
	}

	// Zip contains all four entries under a top-level folder, with the binary
	// entry keeping its exec bit.
	if res.ZipPath != destDir+".zip" {
		t.Errorf("ZipPath got %q want %q", res.ZipPath, destDir+".zip")
	}
	zr, err := zip.OpenReader(res.ZipPath)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()
	want := map[string]bool{
		"lab-bob/cluster.yaml": false,
		"lab-bob/cluster.env":  false,
		"lab-bob/README.md":    false,
		"lab-bob/murmur":       false,
	}
	for _, f := range zr.File {
		if _, ok := want[f.Name]; ok {
			want[f.Name] = true
		}
		if f.Name == "lab-bob/murmur" && f.Mode()&0100 == 0 {
			t.Errorf("bundled binary lost its exec bit in the zip (mode %v)", f.Mode())
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("zip missing entry %q", name)
		}
	}
}

func TestExportOperatorFolder_RefusesIfZipExists(t *testing.T) {
	tmp := t.TempDir()
	srcYAML := filepath.Join(tmp, "cluster.yaml")
	_ = os.WriteFile(srcYAML, []byte("cluster:\n  name: c\n"), 0644)
	destDir := filepath.Join(tmp, "lab-bob")
	_ = os.WriteFile(destDir+".zip", []byte("stale"), 0644)

	_, err := ExportOperatorFolder(srcYAML, User{Name: "bob"}, "secret", destDir, "")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected 'already exists' error for pre-existing zip, got %v", err)
	}
}

func TestDefaultOperatorExportDir(t *testing.T) {
	got := DefaultOperatorExportDir("/home/u/TheLightLab/cluster.yaml", "bob")
	want := "/home/u/TheLightLab-bob"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
