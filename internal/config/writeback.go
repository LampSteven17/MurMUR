package config

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// AppendUserToFile reads cluster.yaml at path, surgically appends a new entry
// under the top-level `users:` sequence (creating the key if absent), and
// writes the file back. Comments and key ordering survive the round-trip
// because we operate on yaml.v3's Node tree rather than re-encoding from a
// decoded struct.
//
// If a user with the same name already exists the function errors loudly.
// The operator-facing TokenSecret must be a `${VAR}` reference; raw secret
// values shouldn't land in cluster.yaml.
func AppendUserToFile(path string, u User) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("writeback: read %s: %w", path, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("writeback: parse %s: %w", path, err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return fmt.Errorf("writeback: %s is not a YAML document", path)
	}
	top := root.Content[0]
	if top.Kind != yaml.MappingNode {
		return fmt.Errorf("writeback: %s top level is not a mapping", path)
	}

	users := findMappingChild(top, "users")
	if users == nil {
		// No users: section yet — append one and recurse on the now-present
		// mapping child rather than duplicating the entry-build code.
		users = &yaml.Node{Kind: yaml.SequenceNode}
		key := &yaml.Node{Kind: yaml.ScalarNode, Value: "users", Tag: "!!str",
			HeadComment: "\nOperators (multi-user mode). Auto-extended by the [U]sers tab."}
		top.Content = append(top.Content, key, users)
	}
	if users.Kind != yaml.SequenceNode {
		return fmt.Errorf("writeback: users: must be a sequence, got kind=%d", users.Kind)
	}

	// Dup check.
	for _, entry := range users.Content {
		if entry.Kind != yaml.MappingNode {
			continue
		}
		if v := findMappingChild(entry, "name"); v != nil && v.Value == u.Name {
			return fmt.Errorf("writeback: user %q already exists in %s", u.Name, path)
		}
	}

	entry := buildUserEntryNode(u)
	users.Content = append(users.Content, entry)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return fmt.Errorf("writeback: encode %s: %w", path, err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("writeback: close encoder: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("writeback: write %s: %w", path, err)
	}
	return nil
}

// RemoveUserFromFile is the symmetric inverse — drops the entry matching
// `name` from cluster.yaml's users: sequence. No-op if the entry is missing
// (idempotent — useful when delete is called after a partial-failure state).
func RemoveUserFromFile(path, name string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("writeback: read %s: %w", path, err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("writeback: parse %s: %w", path, err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil // empty file → nothing to remove
	}
	top := root.Content[0]
	if top.Kind != yaml.MappingNode {
		return nil
	}
	users := findMappingChild(top, "users")
	if users == nil || users.Kind != yaml.SequenceNode {
		return nil
	}
	filtered := users.Content[:0]
	for _, entry := range users.Content {
		if entry.Kind == yaml.MappingNode {
			if v := findMappingChild(entry, "name"); v != nil && v.Value == name {
				continue // drop
			}
		}
		filtered = append(filtered, entry)
	}
	if len(filtered) == len(users.Content) {
		return nil // not found
	}
	users.Content = filtered

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return fmt.Errorf("writeback: encode %s: %w", path, err)
	}
	_ = enc.Close()
	return os.WriteFile(path, buf.Bytes(), 0644)
}

// findMappingChild returns the value Node paired with key under a Mapping
// node, or nil if absent. Mapping nodes interleave keys + values in Content.
func findMappingChild(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func buildUserEntryNode(u User) *yaml.Node {
	mk := func(s string) *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
	}
	entry := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	add := func(k, v string) {
		if v == "" {
			return
		}
		entry.Content = append(entry.Content, mk(k), mk(v))
	}
	add("name", u.Name)
	add("role", u.Role)
	add("proxmox_user", u.ProxmoxUser)
	add("proxmox_token", u.ProxmoxToken)
	add("token_secret", u.TokenSecret) // should be a ${VAR} reference
	add("ssh_pubkey", u.SSHPubKey)     // omitted when blank
	add("comment", u.Comment)
	return entry
}
