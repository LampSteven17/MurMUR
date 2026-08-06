// Package mcpserver exposes murmur's cluster operations as an MCP (Model
// Context Protocol) server, so AI operators can manage the cluster through
// a guarded, audited tool surface instead of raw shell access.
//
// The rails model:
//   - Read-only tools are available to every operator role.
//   - Mutating tools are gated on the operator's role actions, and every call
//     is recorded on murmur's event spine (kind=audit) — locally always, and
//     forwarded to the console hub when MURMUR_EVENTS_URL is set.
//   - Destructive operations (deploy, teardown) are deliberately NOT exposed
//     yet; they need explicit-confirmation semantics before they get tools.
package mcpserver

import (
	"slices"

	"github.com/rtx-monster/murmur/internal/config"
)

// actionAllowed reports whether the active operator's role permits a mutating
// action ("patch", "deploy", "teardown", "host-update"). The implicit-admin
// fallback and the builtin admin role allow everything; other roles must list
// the action (or "*") in role.actions.
func actionAllowed(a *config.ActiveUser, action string) bool {
	if a == nil || a.Fallback || a.Role.Name == "admin" {
		return true
	}
	return slices.Contains(a.Role.Actions, action) || slices.Contains(a.Role.Actions, "*")
}

// appAllowed reports whether the role's apps: allowlist covers the app.
func appAllowed(a *config.ActiveUser, appName string) bool {
	if a == nil || a.Fallback || a.Role.Name == "admin" {
		return true
	}
	return slices.Contains(a.Role.Apps, appName) || slices.Contains(a.Role.Apps, "*")
}

// ownScoped reports whether list views and guest targeting must be filtered
// to guests carrying the operator's murmur-owner tag.
func ownScoped(a *config.ActiveUser) bool {
	return a != nil && !a.Fallback && a.Role.Guests == "own"
}
