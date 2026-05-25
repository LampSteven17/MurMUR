package provision

import (
	"testing"

	"github.com/rtx-monster/murmur/internal/config"
)

func TestOwnerTagSet(t *testing.T) {
	cases := []struct {
		name    string
		active  *config.ActiveUser
		appName string
		want    string
	}{
		{"no active user", nil, "", ""},
		{"no active user with appName", nil, "webapp", ""},
		{"fallback admin (legacy single-operator)",
			&config.ActiveUser{Fallback: true, TokenID: "automation@pve!murmur"},
			"webapp", ""},
		{"named operator, no appName (raw deploy)",
			&config.ActiveUser{Name: "alice"}, "",
			"murmur-owner-alice"},
		{"named operator with appName",
			&config.ActiveUser{Name: "alice"}, "webapp",
			"murmur-owner-alice,murmur-app-webapp"},
		{"empty name is treated as no active user",
			&config.ActiveUser{Name: "", Role: config.Role{Name: "admin"}}, "webapp",
			""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := &Orchestrator{active: c.active}
			if got := o.ownerTagSet(c.appName); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
