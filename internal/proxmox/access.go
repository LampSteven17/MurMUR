package proxmox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// User is one row from GET /access/users. Tokens is populated only when
// ListUsers is called with full=true (PVE returns nil for users with no
// tokens, a slice otherwise).
type User struct {
	UserID    string  `json:"userid"`               // "alice@pve"
	Comment   string  `json:"comment,omitempty"`
	Email     string  `json:"email,omitempty"`
	Enable    int     `json:"enable"`               // 1 = enabled, 0 = suspended
	Expire    int64   `json:"expire,omitempty"`     // 0 = no expiry
	FirstName string  `json:"firstname,omitempty"`
	LastName  string  `json:"lastname,omitempty"`
	Groups    string  `json:"groups,omitempty"`     // comma-separated when present
	RealmType string  `json:"realm-type,omitempty"`
	Tokens    []Token `json:"tokens,omitempty"`     // nil when full=false or user has no tokens
}

// Token is one row from GET /access/users/{userid}/token.
type Token struct {
	TokenID string  `json:"tokenid"`             // bare token name, e.g. "murmur"
	Comment string  `json:"comment,omitempty"`
	Expire  FlexInt `json:"expire,omitempty"`    // PVE may send "0" (string) or 0
	PrivSep FlexInt `json:"privsep"`             // 1 = privilege-separated, 0 = inherits user perms
}

// TokenCreated is the response of POST /access/users/{userid}/token/{tokenid}.
// Value is the secret — only shown at creation time, never retrievable again.
type TokenCreated struct {
	FullTokenID string `json:"full-tokenid"` // "user@realm!tokenname"
	Value       string `json:"value"`        // the secret UUID
	Info        Token  `json:"info"`
}

// Pool is one row from GET /pools.
type Pool struct {
	PoolID  string `json:"poolid"`
	Comment string `json:"comment,omitempty"`
}

// ACLEntry is one row from GET /access/acl. UGID is the user/group/token id;
// Type discriminates between them.
type ACLEntry struct {
	Path      string `json:"path"`
	RoleID    string `json:"roleid"`
	Type      string `json:"type"` // user | group | token
	UGID      string `json:"ugid"`
	Propagate int    `json:"propagate"`
}

// Permissions maps path → permission-name → 1 (granted).
// Result of GET /access/permissions?userid=...&path=...
type Permissions map[string]map[string]int

// ListUsers returns all configured users. When full is true, the Tokens count
// per row is populated (slightly more expensive on the cluster side).
func (c *Client) ListUsers(ctx context.Context, full bool) ([]User, error) {
	path := "/access/users"
	if full {
		path += "?full=1"
	}
	var out []User
	if err := c.GetJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateUser adds a new user. UserID must be of the form "name@realm" where
// realm is one of the configured auth realms (typically "pve" for internal
// service accounts, "pam" for OS-backed users). Comment is optional.
func (c *Client) CreateUser(ctx context.Context, userID, comment string) error {
	if !strings.Contains(userID, "@") {
		return fmt.Errorf("proxmox: CreateUser: userID must be name@realm, got %q", userID)
	}
	form := url.Values{}
	form.Set("userid", userID)
	if comment != "" {
		form.Set("comment", comment)
	}
	_, err := c.PostForm(ctx, "/access/users", form)
	return err
}

// SetUserEnabled suspends (enabled=false) or re-enables (enabled=true) a user.
// PVE preserves the user record either way; this is the soft-disable knob.
func (c *Client) SetUserEnabled(ctx context.Context, userID string, enabled bool) error {
	form := url.Values{}
	form.Set("enable", boolToOneZero(enabled))
	_, err := c.PutForm(ctx, fmt.Sprintf("/access/users/%s", url.PathEscape(userID)), form)
	return err
}

// DeleteUser removes a user. Their tokens and ACL entries are NOT auto-removed
// in older PVE; check ListTokens / ListACL afterward and clean up explicitly.
func (c *Client) DeleteUser(ctx context.Context, userID string) error {
	_, err := c.Delete(ctx, fmt.Sprintf("/access/users/%s", url.PathEscape(userID)), nil)
	return err
}

// ListTokens returns all API tokens owned by a user.
func (c *Client) ListTokens(ctx context.Context, userID string) ([]Token, error) {
	var out []Token
	if err := c.GetJSON(ctx, fmt.Sprintf("/access/users/%s/token", url.PathEscape(userID)), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateToken mints a new API token for the user. The returned TokenCreated.Value
// is the secret — it is shown ONCE by the API and cannot be retrieved later.
// privsep=true gives the token its own ACL (defaulting to no permissions);
// privsep=false makes it inherit the user's full permissions.
func (c *Client) CreateToken(ctx context.Context, userID, tokenName, comment string, privsep bool) (TokenCreated, error) {
	form := url.Values{}
	form.Set("privsep", boolToOneZero(privsep))
	if comment != "" {
		form.Set("comment", comment)
	}
	path := fmt.Sprintf("/access/users/%s/token/%s",
		url.PathEscape(userID), url.PathEscape(tokenName))
	raw, err := c.PostForm(ctx, path, form)
	if err != nil {
		return TokenCreated{}, err
	}
	var out TokenCreated
	if err := json.Unmarshal(raw, &out); err != nil {
		return TokenCreated{}, fmt.Errorf("proxmox: CreateToken: decode response: %w", err)
	}
	return out, nil
}

// DeleteToken revokes an existing API token. The full-tokenid is built
// internally as "<userID>!<tokenName>".
func (c *Client) DeleteToken(ctx context.Context, userID, tokenName string) error {
	path := fmt.Sprintf("/access/users/%s/token/%s",
		url.PathEscape(userID), url.PathEscape(tokenName))
	_, err := c.Delete(ctx, path, nil)
	return err
}

// ListPools returns all pools cluster-wide.
func (c *Client) ListPools(ctx context.Context) ([]Pool, error) {
	var out []Pool
	if err := c.GetJSON(ctx, "/pools", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreatePool adds a new pool.
func (c *Client) CreatePool(ctx context.Context, poolID, comment string) error {
	form := url.Values{}
	form.Set("poolid", poolID)
	if comment != "" {
		form.Set("comment", comment)
	}
	_, err := c.PostForm(ctx, "/pools", form)
	return err
}

// DeletePool removes a pool. The pool must be empty (no VM/LXC members);
// PVE returns 4xx if members remain.
func (c *Client) DeletePool(ctx context.Context, poolID string) error {
	_, err := c.Delete(ctx, fmt.Sprintf("/pools/%s", url.PathEscape(poolID)), nil)
	return err
}

// AddPoolMember assigns one or more VMIDs to a pool. PVE expects vms as a
// comma-separated list; we accept []int and serialise.
func (c *Client) AddPoolMember(ctx context.Context, poolID string, vmids []int) error {
	form := url.Values{}
	form.Set("vms", intsToCSV(vmids))
	form.Set("allow-move", "1") // implicitly move from any current pool
	_, err := c.PutForm(ctx, fmt.Sprintf("/pools/%s", url.PathEscape(poolID)), form)
	return err
}

// RemovePoolMember removes VMIDs from a pool.
func (c *Client) RemovePoolMember(ctx context.Context, poolID string, vmids []int) error {
	form := url.Values{}
	form.Set("vms", intsToCSV(vmids))
	form.Set("delete", "1")
	_, err := c.PutForm(ctx, fmt.Sprintf("/pools/%s", url.PathEscape(poolID)), form)
	return err
}

// ListACL returns every ACL entry on the cluster.
func (c *Client) ListACL(ctx context.Context) ([]ACLEntry, error) {
	var out []ACLEntry
	if err := c.GetJSON(ctx, "/access/acl", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetACL grants a role on a path to a principal (user, group, or token).
// Exactly one of users/groups/tokens should be non-empty.
func (c *Client) SetACL(ctx context.Context, path, role string, users, groups, tokens []string, propagate bool) error {
	form := buildACLForm(path, role, users, groups, tokens, propagate)
	_, err := c.PutForm(ctx, "/access/acl", form)
	return err
}

// DeleteACL revokes a role on a path from a principal.
func (c *Client) DeleteACL(ctx context.Context, path, role string, users, groups, tokens []string) error {
	form := buildACLForm(path, role, users, groups, tokens, false)
	form.Set("delete", "1")
	_, err := c.PutForm(ctx, "/access/acl", form)
	return err
}

// GetPermissions returns the effective permission map for a user on a path
// (and any deeper paths covered by propagation). If userID is empty, PVE
// returns perms for the calling token.
func (c *Client) GetPermissions(ctx context.Context, userID, path string) (Permissions, error) {
	v := url.Values{}
	if userID != "" {
		v.Set("userid", userID)
	}
	if path != "" {
		v.Set("path", path)
	}
	endpoint := "/access/permissions"
	if len(v) > 0 {
		endpoint += "?" + v.Encode()
	}
	out := Permissions{}
	if err := c.GetJSON(ctx, endpoint, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func buildACLForm(path, role string, users, groups, tokens []string, propagate bool) url.Values {
	form := url.Values{}
	form.Set("path", path)
	form.Set("roles", role)
	if len(users) > 0 {
		form.Set("users", strings.Join(users, ","))
	}
	if len(groups) > 0 {
		form.Set("groups", strings.Join(groups, ","))
	}
	if len(tokens) > 0 {
		form.Set("tokens", strings.Join(tokens, ","))
	}
	if propagate {
		form.Set("propagate", "1")
	} else {
		form.Set("propagate", "0")
	}
	return form
}

func boolToOneZero(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func intsToCSV(xs []int) string {
	var b strings.Builder
	for i, x := range xs {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%d", x)
	}
	return b.String()
}
