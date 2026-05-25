package config

import (
	"fmt"
	"net/url"
	"strings"
)

// Validate runs syntactic checks on the loaded config. Semantic checks
// (e.g. storage IDs exist on the cluster, nodes are reachable) happen
// after the API client connects.
func (c *Config) Validate() error {
	var errs []string

	if c.Cluster.Name == "" {
		errs = append(errs, "cluster.name is required")
	}
	if c.Cluster.Domain == "" {
		errs = append(errs, "cluster.domain is required")
	}

	if c.Cluster.API.Endpoint == "" {
		errs = append(errs, "cluster.api.endpoint is required")
	} else if u, err := url.Parse(c.Cluster.API.Endpoint); err != nil || u.Host == "" {
		errs = append(errs, fmt.Sprintf("cluster.api.endpoint %q is not a valid URL", c.Cluster.API.Endpoint))
	}
	// cluster.api.token_* is the implicit-admin credential used ONLY when no
	// users: are defined. With operators configured, each authenticates via
	// their own user token and these are ignored — an operator's kit legitimately
	// ships them empty (admin's secret is never copied into the kit).
	if len(c.Users) == 0 {
		if c.Cluster.API.TokenID == "" {
			errs = append(errs, "cluster.api.token_id is required (no users: defined — implicit-admin fallback)")
		}
		if c.Cluster.API.TokenSecret == "" {
			errs = append(errs, "cluster.api.token_secret is required (use ${VAR} + cluster.env)")
		}
	}

	if len(c.Cluster.Nodes) == 0 {
		errs = append(errs, "cluster.nodes must list at least one node")
	}
	seenNames := map[string]bool{}
	for i, n := range c.Cluster.Nodes {
		if n.Name == "" {
			errs = append(errs, fmt.Sprintf("cluster.nodes[%d].name is required", i))
		}
		if n.Address == "" {
			errs = append(errs, fmt.Sprintf("cluster.nodes[%d].address is required", i))
		}
		if seenNames[n.Name] {
			errs = append(errs, fmt.Sprintf("cluster.nodes[%d].name %q is duplicated", i, n.Name))
		}
		seenNames[n.Name] = true
	}

	if c.Cluster.Storage.VMDisk == "" {
		errs = append(errs, "cluster.storage.vm_disk is required")
	}
	if c.Cluster.Storage.Shared == "" {
		errs = append(errs, "cluster.storage.shared is required")
	}

	if c.Cluster.Network.DefaultBridge == "" {
		errs = append(errs, "cluster.network.default_bridge is required")
	}

	// Apps catalog: each app must reference an existing flavor + image, the
	// name must be unique, and Playbook/PostDeploy are mutually exclusive.
	flavorByName := map[string]bool{}
	for _, f := range c.Flavors {
		flavorByName[f.Name] = true
	}
	imageByName := map[string]bool{}
	for _, im := range c.Images {
		imageByName[im.Name] = true
	}
	seenApps := map[string]bool{}
	for i, a := range c.Apps {
		if a.Name == "" {
			errs = append(errs, fmt.Sprintf("apps[%d].name is required", i))
		}
		if seenApps[a.Name] {
			errs = append(errs, fmt.Sprintf("apps[%d].name %q is duplicated", i, a.Name))
		}
		seenApps[a.Name] = true
		if a.Image == "" {
			errs = append(errs, fmt.Sprintf("apps[%d].image is required", i))
		} else if !imageByName[a.Image] {
			errs = append(errs, fmt.Sprintf("apps[%d].image %q does not exist in the images catalog", i, a.Image))
		}
		if a.Flavor == "" {
			errs = append(errs, fmt.Sprintf("apps[%d].flavor is required", i))
		} else if !flavorByName[a.Flavor] {
			errs = append(errs, fmt.Sprintf("apps[%d].flavor %q does not exist in the flavors catalog", i, a.Flavor))
		}
		if a.Playbook != "" && a.PostDeploy != "" {
			errs = append(errs, fmt.Sprintf("apps[%d] %q: only one of playbook or post_deploy may be set", i, a.Name))
		}
		switch a.Type {
		case "", "vm", "lxc":
		default:
			errs = append(errs, fmt.Sprintf("apps[%d] %q: type %q is not supported (vm | lxc)", i, a.Name, a.Type))
		}
		seenSecret := map[string]bool{}
		for j, s := range a.Secrets {
			if s.Name == "" {
				errs = append(errs, fmt.Sprintf("apps[%d] %q: secrets[%d].name is required", i, a.Name, j))
				continue
			}
			if seenSecret[s.Name] {
				errs = append(errs, fmt.Sprintf("apps[%d] %q: secrets[%d].name %q is duplicated", i, a.Name, j, s.Name))
			}
			seenSecret[s.Name] = true
		}
	}

	for i, b := range c.StorageBackends {
		switch b.Type {
		case "smb", "nfs":
		case "":
			errs = append(errs, fmt.Sprintf("storage_backends[%d].type is required (smb | nfs)", i))
		default:
			errs = append(errs, fmt.Sprintf("storage_backends[%d].type %q is not supported (smb | nfs)", i, b.Type))
		}
		if b.Name == "" {
			errs = append(errs, fmt.Sprintf("storage_backends[%d].name is required", i))
		}
		if b.Host == "" {
			errs = append(errs, fmt.Sprintf("storage_backends[%d].host is required", i))
		}
	}

	// Users + Roles — both optional. When users: is empty, identity falls back
	// to cluster.api.token_* as an implicit admin (today's single-operator
	// shape). When users: is non-empty, every user must reference a known
	// role and identity-selection rules apply at launch.
	roleByName := map[string]Role{}
	for _, r := range c.Roles {
		roleByName[r.Name] = r
	}
	for i, r := range c.Roles {
		if r.Name == "" {
			errs = append(errs, fmt.Sprintf("roles[%d].name is required", i))
		}
		switch r.Guests {
		case "", "own", "all":
		default:
			errs = append(errs, fmt.Sprintf("roles[%d] %q: guests %q is not supported (own | all)", i, r.Name, r.Guests))
		}
	}
	seenUsers := map[string]bool{}
	for i, u := range c.Users {
		if u.Name == "" {
			errs = append(errs, fmt.Sprintf("users[%d].name is required", i))
			continue
		}
		if seenUsers[u.Name] {
			errs = append(errs, fmt.Sprintf("users[%d].name %q is duplicated", i, u.Name))
		}
		seenUsers[u.Name] = true
		if u.Role == "" {
			errs = append(errs, fmt.Sprintf("users[%d] %q: role is required (fail-closed; no implicit admin)", i, u.Name))
		} else if _, ok := roleByName[u.Role]; !ok {
			errs = append(errs, fmt.Sprintf("users[%d] %q: role %q is not defined in roles: (builtin: admin, deployer)", i, u.Name, u.Role))
		}
		if u.ProxmoxUser == "" || !strings.Contains(u.ProxmoxUser, "@") {
			errs = append(errs, fmt.Sprintf("users[%d] %q: proxmox_user must be of the form name@realm (got %q)", i, u.Name, u.ProxmoxUser))
		}
		if u.ProxmoxToken == "" {
			errs = append(errs, fmt.Sprintf("users[%d] %q: proxmox_token is required (the bit after `!` in the full PVE token id)", i, u.Name))
		}
		if u.SSHPubKey != "" && !LooksLikeSSHPubKey(u.SSHPubKey) {
			errs = append(errs, fmt.Sprintf("users[%d] %q: ssh_pubkey doesn't look like an OpenSSH public key (expected e.g. 'ssh-ed25519 AAAA…')", i, u.Name))
		}
		// token_secret may legitimately contain ${VAR} at this point; lazy
		// resolution + emptiness check happens at identity selection.
	}

	if len(errs) > 0 {
		return fmt.Errorf("config invalid:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// LooksLikeSSHPubKey reports whether s has the shape of an OpenSSH public key
// line ("<type> <base64>[ comment]"). It's a cheap format gate, not a
// cryptographic check — enough to catch a private key or random text pasted by
// mistake before it silently breaks a deploy.
func LooksLikeSSHPubKey(s string) bool {
	s = strings.TrimSpace(s)
	prefixes := []string{
		"ssh-ed25519 ", "ssh-rsa ", "ssh-dss ",
		"ecdsa-sha2-", "sk-ssh-ed25519@", "sk-ecdsa-sha2-",
	}
	hasType := false
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			hasType = true
			break
		}
	}
	// Require at least "<type> <body>" — two whitespace-separated fields.
	return hasType && len(strings.Fields(s)) >= 2
}
