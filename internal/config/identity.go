package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// ActiveUser is the operator murmur is running as in this process. It bundles
// the user record, the resolved role, and the API token credentials. The
// implicit-admin fallback (no users: section) returns an ActiveUser with an
// empty Name and the builtin admin role, so call sites can always treat the
// result uniformly.
type ActiveUser struct {
	Name        string // empty for the implicit-admin fallback
	Role        Role
	User        User   // zero-valued in the fallback path
	TokenID     string // full PVE token id ("user@realm!tokenname")
	TokenSecret string // resolved secret
	// Fallback reports whether this came from the cluster.api.token_* legacy
	// path rather than a users: entry. UI code can use it to label the
	// session ("logged in as cluster.api.token_id (implicit admin)") and to
	// disable the [U]sers tab when there is no users: section to extend.
	Fallback bool
}

// ResolveActive determines which user is "logged in" and returns their
// effective credentials + role.
//
// Resolution order:
//
//  1. asFlag (`--as <name>`)
//  2. $MURMUR_USER
//  3. exactly one entry in users:
//  4. exactly one operator whose token_secret is set in the environment —
//     each operator's cluster.env carries only their own token, so a single
//     match is an unambiguous identity ("who's logged in")
//  5. error (lists available names)
//
// If users: is empty, falls back to cluster.api.token_* as an implicit admin.
// The active user's token_secret is expanded against the current environment
// here; missing env vars fail loudly.
func (c *Config) ResolveActive(asFlag string) (*ActiveUser, error) {
	if len(c.Users) == 0 {
		// Legacy single-operator path. cluster.api.token_* already had its
		// ${VAR}s substituted by the loader; no further work needed.
		return &ActiveUser{
			Role: roleByNameOrBuiltin(c.Roles, "admin"),
			TokenID:     c.Cluster.API.TokenID,
			TokenSecret: c.Cluster.API.TokenSecret,
			Fallback:    true,
		}, nil
	}

	pick := strings.TrimSpace(asFlag)
	if pick == "" {
		pick = strings.TrimSpace(os.Getenv("MURMUR_USER"))
	}
	if pick == "" {
		switch resolvable := operatorsWithResolvableToken(c.Users); {
		case len(c.Users) == 1:
			// Single operator — unambiguous by definition.
			pick = c.Users[0].Name
		case len(resolvable) == 1:
			// Exactly one operator's token is present in the environment, so the
			// identity is unambiguous even though several operators are
			// configured: each operator's cluster.env carries only their own.
			pick = resolvable[0]
		case len(resolvable) > 1:
			return nil, fmt.Errorf(
				"ambiguous operator: tokens for multiple operators are set in the environment (%s). "+
					"pass --as <name> or set MURMUR_USER. defined users: %s",
				strings.Join(resolvable, ", "), availableUserNames(c.Users))
		default:
			return nil, fmt.Errorf(
				"no operator selected: set one operator's token in cluster.env, or pass --as <name> "+
					"/ set MURMUR_USER. defined users: %s",
				availableUserNames(c.Users))
		}
	}

	var matched *User
	for i := range c.Users {
		if c.Users[i].Name == pick {
			matched = &c.Users[i]
			break
		}
	}
	if matched == nil {
		return nil, fmt.Errorf(
			"unknown operator %q. defined users: %s",
			pick, availableUserNames(c.Users))
	}

	role := roleByNameOrBuiltin(c.Roles, matched.Role)
	if role.Name == "" {
		// Validate should have caught this, but belt-and-braces.
		return nil, fmt.Errorf(
			"operator %q references undefined role %q",
			matched.Name, matched.Role)
	}

	secret, err := expandEnvStrict(matched.TokenSecret)
	if err != nil {
		return nil, fmt.Errorf("operator %q: token_secret: %w", matched.Name, err)
	}
	if strings.TrimSpace(secret) == "" {
		return nil, fmt.Errorf(
			"operator %q: token_secret resolved to empty. define the "+
				"referenced env var in cluster.env (e.g. %s=...)",
			matched.Name, suggestEnvVarName(matched))
	}

	return &ActiveUser{
		Name:        matched.Name,
		Role:        role,
		User:        *matched,
		TokenID:     matched.FullTokenID(),
		TokenSecret: secret,
	}, nil
}

// operatorsWithResolvableToken returns the names of users whose token_secret
// resolves to a non-empty value against the current environment. Used to infer
// the active operator when none was named: each operator's cluster.env carries
// only their own token, so exactly one match is an unambiguous identity. Only
// users[].token_secret refs are consulted — app secrets and other env vars are
// irrelevant.
func operatorsWithResolvableToken(users []User) []string {
	var out []string
	for _, u := range users {
		s, err := expandEnvStrict(u.TokenSecret)
		if err != nil {
			continue // references an env var that isn't set
		}
		if strings.TrimSpace(s) == "" {
			continue
		}
		out = append(out, u.Name)
	}
	return out
}

func availableUserNames(users []User) string {
	names := make([]string, 0, len(users))
	for _, u := range users {
		names = append(names, u.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func roleByNameOrBuiltin(roles []Role, name string) Role {
	for _, r := range roles {
		if r.Name == name {
			return r
		}
	}
	for _, r := range builtinRoles {
		if r.Name == name {
			return r
		}
	}
	return Role{}
}

// expandEnvStrict walks a string and resolves ${VAR} against os.Getenv.
// Undefined vars produce a loud error listing the missing names. The
// `$${VAR}` escape from substituteScalars never reaches here because it's
// resolved at YAML-load time, before token_secret is restored from the
// stash. We still pass `${...}` through varRE for consistency.
func expandEnvStrict(s string) (string, error) {
	var missing []string
	out := varRE.ReplaceAllStringFunc(s, func(m string) string {
		if strings.HasPrefix(m, "$$") {
			return m[1:]
		}
		name := m[2 : len(m)-1]
		v, ok := os.LookupEnv(name)
		if !ok {
			missing = append(missing, name)
			return m
		}
		return v
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("undefined env vars: %s (set in cluster.env)",
			strings.Join(uniq(missing), ", "))
	}
	return out, nil
}

// suggestEnvVarName builds a likely env-var name from a User entry — used in
// error messages so the operator sees a copy-pasteable hint.
func suggestEnvVarName(u *User) string {
	// First preference: pluck the var name out of the operator's raw
	// token_secret if it's a simple ${VAR} reference.
	matches := varRE.FindStringSubmatch(u.TokenSecret)
	if len(matches) >= 2 {
		return matches[1]
	}
	// Fall back to a synthesised conventional name.
	upper := strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(u.Name))
	return upper + "_TOKEN"
}
