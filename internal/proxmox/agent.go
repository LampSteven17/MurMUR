package proxmox

import (
	"context"
	"fmt"
	"strings"
)

// AgentIPAddress is one IP entry from the guest agent's network-get-interfaces.
type AgentIPAddress struct {
	IPAddress     string `json:"ip-address"`
	IPAddressType string `json:"ip-address-type"` // ipv4 | ipv6
	Prefix        int    `json:"prefix"`
}

// AgentNetInterface is one interface entry.
type AgentNetInterface struct {
	Name            string           `json:"name"`
	HardwareAddress string           `json:"hardware-address"`
	IPAddresses     []AgentIPAddress `json:"ip-addresses"`
}

// agentResponse is the inner envelope ProxMox wraps guest-agent results in
// (sits inside the outer `data` field).
type agentNetResponse struct {
	Result []AgentNetInterface `json:"result"`
}

// GuestAgentNetInterfaces returns the interfaces reported by the guest agent.
// Requires qemu-guest-agent installed and running in the guest, and
// `agent=1` set on the VM config.
func (c *Client) GuestAgentNetInterfaces(ctx context.Context, node string, vmid int) ([]AgentNetInterface, error) {
	var r agentNetResponse
	path := fmt.Sprintf("/nodes/%s/qemu/%d/agent/network-get-interfaces", node, vmid)
	if err := c.GetJSON(ctx, path, &r); err != nil {
		return nil, err
	}
	return r.Result, nil
}

// FirstIPv4 returns the first non-loopback IPv4 address reported by the guest
// agent. Empty string if none are found yet (boot still in progress, agent
// not running, etc.).
func FirstIPv4(ifaces []AgentNetInterface) string {
	for _, i := range ifaces {
		if strings.HasPrefix(i.Name, "lo") {
			continue
		}
		for _, a := range i.IPAddresses {
			if a.IPAddressType != "ipv4" {
				continue
			}
			if a.IPAddress == "" || strings.HasPrefix(a.IPAddress, "127.") {
				continue
			}
			return a.IPAddress
		}
	}
	return ""
}
