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
	if c.Cluster.API.TokenID == "" {
		errs = append(errs, "cluster.api.token_id is required")
	}
	if c.Cluster.API.TokenSecret == "" {
		errs = append(errs, "cluster.api.token_secret is required (use ${VAR} + cluster.env)")
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

	if len(errs) > 0 {
		return fmt.Errorf("config invalid:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}
