package proxmox

import "context"

// HAResource is one entry from GET /cluster/ha/resources. SID is the HA
// identifier ("vm:103" / "ct:110"); State is the desired HA state
// (started | stopped | ignored | disabled).
type HAResource struct {
	SID   string `json:"sid"`
	Type  string `json:"type"` // "vm" | "ct"
	State string `json:"state"`
}

// ListHAResources returns every guest registered with the HA manager. Empty
// when HA isn't configured. Requires Sys.Audit (read).
func (c *Client) ListHAResources(ctx context.Context) ([]HAResource, error) {
	var out []HAResource
	if err := c.GetJSON(ctx, "/cluster/ha/resources", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteHAResource removes a guest from HA management so it can be stopped and
// destroyed without the HA stack restarting it. sid is the HA identifier, e.g.
// "vm:103" or "ct:110".
func (c *Client) DeleteHAResource(ctx context.Context, sid string) error {
	_, err := c.Delete(ctx, "/cluster/ha/resources/"+sid, nil)
	return err
}
