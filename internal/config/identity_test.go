package config

import (
	"strings"
	"testing"
)

func twoUserCfg() *Config {
	return &Config{Users: []User{
		{Name: "alice", Role: "deployer", ProxmoxUser: "alice@pve", ProxmoxToken: "murmur", TokenSecret: "${ALICE_TOKEN}"},
		{Name: "bob", Role: "admin", ProxmoxUser: "bob@pve", ProxmoxToken: "murmur", TokenSecret: "${BOB_TOKEN}"},
	}}
}

// With several operators configured but only one operator's token present in
// the environment, identity is inferred without --as / MURMUR_USER.
func TestResolveActive_InfersFromLoneEnvToken(t *testing.T) {
	cfg := twoUserCfg()
	t.Setenv("MURMUR_USER", "")
	t.Setenv("ALICE_TOKEN", "") // present-but-empty counts as absent
	t.Setenv("BOB_TOKEN", "bob-secret")

	a, err := cfg.ResolveActive("")
	if err != nil {
		t.Fatalf("expected to infer bob, got err: %v", err)
	}
	if a.Name != "bob" {
		t.Errorf("inferred %q, want bob", a.Name)
	}
}

func TestResolveActive_AmbiguousWhenMultipleTokens(t *testing.T) {
	cfg := twoUserCfg()
	t.Setenv("MURMUR_USER", "")
	t.Setenv("ALICE_TOKEN", "a")
	t.Setenv("BOB_TOKEN", "b")

	_, err := cfg.ResolveActive("")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous error, got: %v", err)
	}
}

func TestResolveActive_ErrorsWhenNoTokenPresent(t *testing.T) {
	cfg := twoUserCfg()
	t.Setenv("MURMUR_USER", "")
	t.Setenv("ALICE_TOKEN", "")
	t.Setenv("BOB_TOKEN", "")

	_, err := cfg.ResolveActive("")
	if err == nil || !strings.Contains(err.Error(), "no operator selected") {
		t.Fatalf("expected no-operator error, got: %v", err)
	}
}

// Explicit selection wins over inference, even when it would be ambiguous.
func TestResolveActive_AsFlagOverridesInference(t *testing.T) {
	cfg := twoUserCfg()
	t.Setenv("MURMUR_USER", "")
	t.Setenv("ALICE_TOKEN", "a")
	t.Setenv("BOB_TOKEN", "b")

	a, err := cfg.ResolveActive("alice")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "alice" {
		t.Errorf("got %q, want alice", a.Name)
	}
}

func TestResolveActive_MurmurUserOverridesInference(t *testing.T) {
	cfg := twoUserCfg()
	t.Setenv("ALICE_TOKEN", "a")
	t.Setenv("BOB_TOKEN", "b")
	t.Setenv("MURMUR_USER", "alice")

	a, err := cfg.ResolveActive("")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "alice" {
		t.Errorf("got %q, want alice", a.Name)
	}
}
