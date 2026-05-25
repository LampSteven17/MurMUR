package tui

import (
	"testing"

	"github.com/rtx-monster/murmur/internal/config"
)

// The deployer bundle must grant pool privileges on the operator's own pool
// (PVE's "deeper level replaces inherited" rule otherwise strips Pool.* there)
// and clone access scoped to the shared templates pool.
func TestACLBundleFor_Deployer(t *testing.T) {
	want := map[string]bool{
		"/pool/murmur-bob:PVEVMAdmin":                          false,
		"/pool/murmur-bob:PVEPoolAdmin":                        false,
		"/pool/" + config.TemplatesPoolID + ":PVETemplateUser": false,
		"/:PVEAuditor":                                         false,
	}
	for _, e := range aclBundleFor("deployer", "murmur-bob") {
		if _, ok := want[e.path+":"+e.role]; ok {
			want[e.path+":"+e.role] = true
		}
	}
	for k, got := range want {
		if !got {
			t.Errorf("deployer ACL bundle missing %q", k)
		}
	}
}

// appAllowed gates the apps/patch catalogs (it was dead until the deployer
// scoping work). Fallback/nil sees all; "*" sees all; a listed app is allowed;
// an unlisted app and an empty list are denied.
func TestAppAllowed(t *testing.T) {
	star := &config.ActiveUser{Role: config.Role{Apps: []string{"*"}}}
	scoped := &config.ActiveUser{Role: config.Role{Apps: []string{"example-vm"}}}
	none := &config.ActiveUser{Role: config.Role{Apps: []string{}}}

	cases := []struct {
		name  string
		user  *config.ActiveUser
		app   string
		allow bool
	}{
		{"nil → all", nil, "immich", true},
		{"fallback → all", &config.ActiveUser{Fallback: true}, "immich", true},
		{"wildcard → all", star, "immich", true},
		{"scoped allows listed", scoped, "example-vm", true},
		{"scoped denies unlisted", scoped, "immich", false},
		{"empty denies all", none, "example-vm", false},
	}
	for _, c := range cases {
		if got := appAllowed(c.user, c.app); got != c.allow {
			t.Errorf("%s: appAllowed(%q) = %v, want %v", c.name, c.app, got, c.allow)
		}
	}
}

// observer is gone — only admin and deployer remain addable, and neither
// resolves to an observer bundle.
func TestRolesNoObserver(t *testing.T) {
	v := &UsersView{}
	roles := v.addableRoles()
	if len(roles) != 2 || roles[0] != "admin" || roles[1] != "deployer" {
		t.Errorf("addableRoles = %v, want [admin deployer]", roles)
	}
	if aclBundleFor("observer", "murmur-x") != nil {
		t.Errorf("observer should have no ACL bundle")
	}
}
