// Package mcpserver exposes murmur's cluster operations as an MCP (Model
// Context Protocol) server, so AI operators can manage the cluster through
// a guarded, audited tool surface instead of raw shell access.
//
// The rails model:
//   - Read-only tools are available to every operator role.
//   - Mutating tools are gated on the operator's role actions and every call
//     is appended to a JSONL audit log.
//   - Destructive operations (deploy, teardown) are deliberately NOT exposed
//     yet; they need explicit-confirmation semantics before they get tools.
package mcpserver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

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

// auditEntry is one line of the JSONL audit log.
type auditEntry struct {
	TS       string `json:"ts"`
	Operator string `json:"operator"`
	Role     string `json:"role"`
	Tool     string `json:"tool"`
	Args     any    `json:"args,omitempty"`
	Outcome  string `json:"outcome"` // allowed | denied | ok | error
	Detail   string `json:"detail,omitempty"`
}

// auditLog appends JSONL entries to a single file. Writes are best-effort for
// reads but MANDATORY for mutations: a mutating tool refuses to run if its
// pre-call entry cannot be written (no audit, no action).
type auditLog struct {
	mu   sync.Mutex
	path string
}

// AuditPath resolves the audit log location: $MURMUR_AUDIT_LOG, else
// ~/.local/state/murmur/audit.jsonl.
func AuditPath() (string, error) {
	if p := os.Getenv("MURMUR_AUDIT_LOG"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve audit log path: %w", err)
	}
	return filepath.Join(home, ".local", "state", "murmur", "audit.jsonl"), nil
}

func newAuditLog(path string) (*auditLog, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create audit log dir: %w", err)
	}
	return &auditLog{path: path}, nil
}

func (l *auditLog) write(e auditEntry) error {
	e.TS = time.Now().UTC().Format(time.RFC3339)
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}
