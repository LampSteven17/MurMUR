package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendUserToFile_PreservesComments(t *testing.T) {
	src := `# top-level comment
cluster:
  name: lab
  domain: lab.test
  api:
    endpoint: https://1.2.3.4:8006
    token_id: x@pve!y
    token_secret: stub

# Operators.
users:
  - name: alice
    role: admin
    proxmox_user: alice@pve
    proxmox_token: murmur
    token_secret: ${ALICE_TOKEN}
`
	path := filepath.Join(t.TempDir(), "cluster.yaml")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	err := AppendUserToFile(path, User{
		Name: "bob", Role: "deployer",
		ProxmoxUser: "bob@pve", ProxmoxToken: "murmur",
		TokenSecret: "${BOB_TOKEN}",
		Comment:     "Bob — deployer",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(path)
	s := string(got)
	if !strings.Contains(s, "# top-level comment") {
		t.Errorf("top-level comment lost")
	}
	if !strings.Contains(s, "# Operators.") {
		t.Errorf("users-section comment lost")
	}
	if !strings.Contains(s, "name: alice") {
		t.Errorf("existing alice entry lost")
	}
	if !strings.Contains(s, "name: bob") {
		t.Errorf("new bob entry not added")
	}
	if !strings.Contains(s, "${BOB_TOKEN}") {
		t.Errorf("token_secret ${VAR} not preserved verbatim")
	}
	if !strings.Contains(s, "Bob — deployer") {
		t.Errorf("comment field missing")
	}
}

func TestAppendUserToFile_WritesSSHPubKey(t *testing.T) {
	src := "cluster:\n  name: lab\n"
	path := filepath.Join(t.TempDir(), "cluster.yaml")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	const key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5 bob@laptop"
	if err := AppendUserToFile(path, User{
		Name: "bob", Role: "deployer",
		ProxmoxUser: "bob@pve", ProxmoxToken: "murmur",
		TokenSecret: "${BOB_TOKEN}", SSHPubKey: key,
	}); err != nil {
		t.Fatal(err)
	}
	s, _ := os.ReadFile(path)
	if !strings.Contains(string(s), "ssh_pubkey: "+key) {
		t.Errorf("ssh_pubkey not written; got:\n%s", s)
	}
	// Blank pubkey must be omitted, not written as an empty scalar.
	path2 := filepath.Join(t.TempDir(), "cluster.yaml")
	_ = os.WriteFile(path2, []byte(src), 0644)
	if err := AppendUserToFile(path2, User{
		Name: "alice", Role: "deployer",
		ProxmoxUser: "alice@pve", ProxmoxToken: "murmur",
		TokenSecret: "${ALICE_TOKEN}",
	}); err != nil {
		t.Fatal(err)
	}
	s2, _ := os.ReadFile(path2)
	if strings.Contains(string(s2), "ssh_pubkey") {
		t.Errorf("blank ssh_pubkey should be omitted; got:\n%s", s2)
	}
}

func TestAppendUserToFile_RejectsDuplicate(t *testing.T) {
	src := `cluster:
  name: c
  domain: d
  api: {endpoint: "https://x:8006", token_id: x@pve!y, token_secret: stub}
users:
  - name: alice
    role: admin
    proxmox_user: alice@pve
    proxmox_token: murmur
    token_secret: ${ALICE_TOKEN}
`
	path := filepath.Join(t.TempDir(), "cluster.yaml")
	_ = os.WriteFile(path, []byte(src), 0644)
	err := AppendUserToFile(path, User{
		Name: "alice", Role: "deployer",
		ProxmoxUser: "alice@pve", ProxmoxToken: "murmur",
		TokenSecret: "${X}",
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestAppendUserToFile_AddsUsersSectionWhenAbsent(t *testing.T) {
	src := `cluster:
  name: c
  domain: d
  api: {endpoint: "https://x:8006", token_id: x@pve!y, token_secret: stub}
`
	path := filepath.Join(t.TempDir(), "cluster.yaml")
	_ = os.WriteFile(path, []byte(src), 0644)
	err := AppendUserToFile(path, User{
		Name: "alice", Role: "admin",
		ProxmoxUser: "alice@pve", ProxmoxToken: "murmur",
		TokenSecret: "${ALICE_TOKEN}",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "users:") || !strings.Contains(string(got), "name: alice") {
		t.Errorf("expected new users: section with alice entry, got:\n%s", got)
	}
}

func TestRemoveUserFromFile(t *testing.T) {
	src := `cluster:
  name: c
  domain: d
  api: {endpoint: "https://x:8006", token_id: x@pve!y, token_secret: stub}
users:
  - name: alice
    role: admin
    proxmox_user: alice@pve
    proxmox_token: murmur
    token_secret: ${ALICE_TOKEN}
  - name: bob
    role: deployer
    proxmox_user: bob@pve
    proxmox_token: murmur
    token_secret: ${BOB_TOKEN}
`
	path := filepath.Join(t.TempDir(), "cluster.yaml")
	_ = os.WriteFile(path, []byte(src), 0644)
	if err := RemoveUserFromFile(path, "bob"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	s := string(got)
	if strings.Contains(s, "name: bob") {
		t.Errorf("bob still present after remove")
	}
	if !strings.Contains(s, "name: alice") {
		t.Errorf("alice incorrectly removed")
	}
	// Idempotent: removing again should be a no-op.
	if err := RemoveUserFromFile(path, "bob"); err != nil {
		t.Errorf("idempotent remove failed: %v", err)
	}
}
