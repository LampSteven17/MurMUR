package tui

import (
	"strings"

	"github.com/rtx-monster/murmur/internal/config"
	"github.com/rtx-monster/murmur/internal/proxmox"
)

// ownerFilter returns the subset of resources the active operator is allowed
// to see in their list views. When role.guests == "all" (admin default) or
// the fallback admin path is in effect, returns the input unchanged. When
// role.guests == "own", keeps only rows tagged `murmur-owner-<name>`.
//
// Resources whose Type is neither "qemu" nor "lxc" (e.g. nodes, storage,
// pools, sdn) bypass the filter — the owner concept only applies to guests.
func ownerFilter(active *config.ActiveUser, in []proxmox.Resource) []proxmox.Resource {
	if active == nil || active.Fallback || active.Role.Guests != "own" {
		return in
	}
	owner := active.Name
	if owner == "" {
		return in
	}
	out := make([]proxmox.Resource, 0, len(in))
	for _, r := range in {
		if r.Type != "qemu" && r.Type != "lxc" {
			out = append(out, r)
			continue
		}
		if resourceHasOwner(r, owner) {
			out = append(out, r)
		}
	}
	return out
}

// resourceHasOwner reports whether resource r carries `murmur-owner-<name>`
// in its Tags string. PVE serialises tags as semicolon-separated in the API
// payload returned from /cluster/resources, but our local Resource model
// reads them verbatim (semicolon or comma is normalised on the way in by
// PVE itself). Match on a parsed token list to be robust to either.
func resourceHasOwner(r proxmox.Resource, owner string) bool {
	if r.Tags == "" {
		return false
	}
	want := "murmur-owner-" + owner
	for _, tag := range splitTags(r.Tags) {
		if tag == want {
			return true
		}
	}
	return false
}

func splitTags(s string) []string {
	out := []string{}
	for _, t := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ';' || r == ',' || r == ' '
	}) {
		t = strings.TrimSpace(t)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// appAllowed reports whether the active operator's role permits acting on the
// named app (deploy, patch, teardown). Fallback admin / no active user → all
// apps allowed. Role.Apps with "*" → all. Empty Role.Apps → none.
func appAllowed(active *config.ActiveUser, appName string) bool {
	if active == nil || active.Fallback {
		return true
	}
	return active.Role.Allows(active.Role.Apps, appName)
}
