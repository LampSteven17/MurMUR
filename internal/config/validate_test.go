package config

import (
	"strings"
	"testing"
)

// validBase returns a Config that passes Validate, minus credentials/users —
// callers tweak those fields per case.
func validBase() *Config {
	return &Config{
		Cluster: Cluster{
			Name:    "c",
			Domain:  "d",
			API:     API{Endpoint: "https://h:8006"},
			Nodes:   []Node{{Name: "n1", Address: "1.2.3.4"}},
			Storage: Storage{VMDisk: "vd", Shared: "sh"},
			Network: Network{DefaultBridge: "vmbr0"},
		},
		Roles: []Role{{Name: "admin"}},
	}
}

// cluster.api.token_* is only required for the implicit-admin fallback (no
// users:). With operators defined, an empty cluster.api credential is fine —
// that's exactly what an operator's kit ships.
func TestValidate_APICredentialOptionalWithUsers(t *testing.T) {
	cfg := validBase()
	cfg.Users = []User{{Name: "bob", Role: "admin", ProxmoxUser: "bob@pve", ProxmoxToken: "murmur", TokenSecret: "${BOB}"}}
	// cluster.api.token_id / token_secret left empty.
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid with users + empty api creds, got: %v", err)
	}
}

func TestValidate_APICredentialRequiredWithoutUsers(t *testing.T) {
	cfg := validBase() // no Users
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "cluster.api.token_secret is required") {
		t.Fatalf("expected token_secret-required error, got: %v", err)
	}
}

func TestLooksLikeSSHPubKey(t *testing.T) {
	ok := []string{
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5 user@host",
		"ssh-rsa AAAAB3NzaC1yc2E",
		"ecdsa-sha2-nistp256 AAAAE2Vj",
		"  ssh-ed25519 AAAA trailing-space-trimmed  ",
		"sk-ssh-ed25519@openssh.com AAAA",
	}
	for _, s := range ok {
		if !LooksLikeSSHPubKey(s) {
			t.Errorf("expected valid: %q", s)
		}
	}
	bad := []string{
		"",
		"not a key",
		"ssh-ed25519",                       // type only, no body
		"-----BEGIN OPENSSH PRIVATE KEY-----", // a private key pasted by mistake
		"AAAAC3NzaC1lZDI1NTE5",              // body only, no type
	}
	for _, s := range bad {
		if LooksLikeSSHPubKey(s) {
			t.Errorf("expected invalid: %q", s)
		}
	}
}
