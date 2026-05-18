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

// AgentOSInfo is what /qemu/{vmid}/agent/get-osinfo reports. Requires the
// qemu-guest-agent inside the guest. Useful for showing the *actual* OS
// (vs whatever the deploy catalog claimed).
type AgentOSInfo struct {
	ID         string `json:"id"`             // e.g. "ubuntu", "debian"
	Name       string `json:"name"`           // e.g. "Ubuntu"
	PrettyName string `json:"pretty-name"`    // e.g. "Ubuntu 24.04.4 LTS"
	VersionID  string `json:"version-id"`     // e.g. "24.04"
	Kernel     string `json:"kernel-release,omitempty"`
	Machine    string `json:"machine,omitempty"`
}

// agentOSInfoResponse mirrors the inner-result wrapping PVE puts around guest
// agent responses (alongside the outer `data` envelope our `do()` already
// strips).
type agentOSInfoResponse struct {
	Result AgentOSInfo `json:"result"`
}

// GuestAgentOSInfo returns the OS info the guest agent reports. Errors out
// (rather than returning a partial struct) when the agent is unreachable.
func (c *Client) GuestAgentOSInfo(ctx context.Context, node string, vmid int) (AgentOSInfo, error) {
	var r agentOSInfoResponse
	path := fmt.Sprintf("/nodes/%s/qemu/%d/agent/get-osinfo", node, vmid)
	if err := c.GetJSON(ctx, path, &r); err != nil {
		return AgentOSInfo{}, err
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
