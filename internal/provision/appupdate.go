package provision

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/rtx-monster/murmur/internal/config"
	"github.com/rtx-monster/murmur/internal/proxmox"
)

// AppRuntimeInfo is the live-state snapshot the [p]atch tab renders per app.
// All fields are best-effort — partial data is fine, the UI shows "—" for
// anything that didn't resolve.
//
// For match_all apps, the single-instance fields below describe the FIRST
// found replica (so the table row still has a representative OS / first IP).
// Replicas holds the full set; len(Replicas) > 0 signals "this app has
// fanned out across multiple guests".
type AppRuntimeInfo struct {
	App      config.App
	VMID     int    // 0 if no guest with this name exists
	Node     string
	Type     string // "qemu" | "lxc"
	Status   string // running | stopped | (empty = not deployed)
	OS       string // pretty OS string ("Ubuntu 24.04.4 LTS", "debian (LXC)", "—")
	IP       string // IPv4; "—" if not reported
	Compose  []ComposeContainer
	Err      error // surfaces lookup failures we couldn't recover from

	// Update-availability snapshot. Populated by checkAppUpdates() during
	// InspectApp. UpdatesTotal is the number of probed components (compose
	// images, apt packages, etc.); UpdatesAvailable is the count of those
	// that have a newer version waiting. UpdateChecked = false means the
	// probe didn't run (guest down, no compose dir, no SSH, etc.).
	UpdatesAvailable int
	UpdatesTotal     int
	UpdateChecked    bool

	// Replicas is populated only when App.MatchAll is true. Contains the
	// per-instance summary for every matching guest (including the first,
	// to keep the iteration symmetric in callers).
	Replicas []ReplicaInfo
}

// ReplicaInfo is the per-instance summary for match_all apps. Each replica
// can drift independently (different OS minor, different IP, different
// patch level) so we fetch per-instance details rather than assume they
// match.
type ReplicaInfo struct {
	VMID   int
	Node   string
	Type   string // "qemu" | "lxc"
	Status string
	IP     string
	OS     string // per-instance — replicas can drift

	// Per-replica update snapshot. See AppRuntimeInfo for semantics.
	UpdatesAvailable int
	UpdatesTotal     int
	UpdateChecked    bool
}

// ComposeContainer is one row from `docker compose ps` on the guest. We
// keep it small — just enough to render a sane summary column.
type ComposeContainer struct {
	Service string
	Image   string // full image:tag or image@digest
	State   string // "running" | "exited" | ...
}

// InspectApp pulls a runtime snapshot for one app: locates the guest by
// name, fetches OS info from the agent (qemu) or config (lxc), gets the
// IP, then SSHes to read `docker compose ps` from /opt/<name>/. Returns
// an AppRuntimeInfo with best-effort fields; Err is set only for failures
// the UI should know about (e.g., resource listing failed).
//
// For match_all apps, the single-instance fields describe the first
// matching guest (representative OS/IP for the table); Replicas holds the
// full per-instance set.
// resourceCarriesOwner reports whether r's Tags contain murmur-owner-<owner>.
// PVE normalises tags to semicolons in /cluster/resources output; we tolerate
// commas/spaces too so callers can be sloppy about separators.
func resourceCarriesOwner(r *proxmox.Resource, owner string) bool {
	if r == nil || r.Tags == "" {
		return false
	}
	want := "murmur-owner-" + owner
	for _, t := range splitOwnerTags(r.Tags) {
		if t == want {
			return true
		}
	}
	return false
}

func splitOwnerTags(s string) []string {
	out := make([]string, 0, 4)
	start := 0
	for i, c := range s {
		if c == ';' || c == ',' || c == ' ' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func (o *Orchestrator) InspectApp(ctx context.Context, app config.App) AppRuntimeInfo {
	info := AppRuntimeInfo{App: app}

	resources, err := o.client.GetResources(ctx, "vm")
	if err != nil {
		info.Err = fmt.Errorf("inspect: list resources: %w", err)
		return info
	}
	// If the active operator's role restricts visibility to their own
	// guests, drop matches whose tags don't carry murmur-owner-<name>. The
	// fallback admin path and `guests: all` roles see every replica.
	wantOwner := ""
	if o.active != nil && !o.active.Fallback && o.active.Role.Guests == "own" {
		wantOwner = o.active.Name
	}
	var matches []*proxmox.Resource
	for i := range resources {
		r := &resources[i]
		if r.Template == 1 {
			continue
		}
		if r.Name != app.Name {
			continue
		}
		if wantOwner != "" && !resourceCarriesOwner(r, wantOwner) {
			continue
		}
		matches = append(matches, r)
		if !app.MatchAll {
			break // single-match mode — first hit wins
		}
	}
	if len(matches) == 0 {
		// Not deployed; everything else stays empty.
		return info
	}
	// Single-instance fields describe the first match — the row's
	// "representative" guest. Replicas only populated for match_all.
	first := matches[0]
	info.VMID = first.VMID
	info.Node = first.Node
	info.Type = first.Type
	info.Status = first.Status
	if app.MatchAll {
		info.Replicas = make([]ReplicaInfo, 0, len(matches))
		for _, m := range matches {
			info.Replicas = append(info.Replicas, ReplicaInfo{
				VMID: m.VMID, Node: m.Node, Type: m.Type, Status: m.Status,
			})
		}
	}

	// OS detection. VMs use the qemu-guest-agent; LXCs use the per-VM
	// config's `ostype` (set by `pct create --ostype`).
	osCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	switch info.Type {
	case "qemu":
		if osi, err := o.client.GuestAgentOSInfo(osCtx, info.Node, info.VMID); err == nil {
			info.OS = osi.PrettyName
			if info.OS == "" {
				info.OS = osi.Name
			}
		}
	case "lxc":
		if cfg, err := o.client.GetLXCConfig(osCtx, info.Node, info.VMID); err == nil {
			if t := cfg["ostype"]; t != "" {
				info.OS = t + " (LXC)"
			}
		}
	}

	// IP. Only meaningful for running guests; stopped guests have no IP.
	if info.Status == "running" {
		info.IP = o.fetchGuestIP(ctx, info.Type, info.Node, info.VMID)
	}
	// For match_all apps, fetch per-replica IP+OS. The first replica's
	// fields mirror the row's "representative" values; the rest are filled
	// independently in case they drifted (different OS minor, different IP).
	if app.MatchAll {
		info.Replicas[0].IP = info.IP
		info.Replicas[0].OS = info.OS
		for i, r := range matches {
			if i == 0 {
				continue // already filled
			}
			if r.Status == "running" {
				info.Replicas[i].IP = o.fetchGuestIP(ctx, r.Type, r.Node, r.VMID)
			}
			info.Replicas[i].OS = o.fetchGuestOS(ctx, r.Type, r.Node, r.VMID)
		}
	}

	// Docker compose introspection. Skip if guest isn't running or no IP.
	// We SSH and ask compose for a one-line-per-container service|image|state
	// summary. Bounded ~6s so a slow guest doesn't stall the whole batch.
	if info.IP != "" {
		user := o.appSSHUser(app, info.Type, info.Node, info.VMID)
		cmd := fmt.Sprintf(
			"cd /opt/%s 2>/dev/null && "+
				"docker compose ps --format '{{.Service}}|{{.Image}}|{{.State}}' 2>/dev/null",
			shellQuoteForSSH(app.Name))
		composeCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		out, _ := exec.CommandContext(composeCtx, "ssh",
			"-o", "BatchMode=yes",
			"-o", "ConnectTimeout=4",
			"-o", "StrictHostKeyChecking=accept-new",
			user+"@"+info.IP,
			cmd,
		).Output()
		info.Compose = parseComposeSummary(string(out))

		// Update-availability probe (built-in or app-declared).
		avail, total, checked := o.checkAppUpdates(ctx, app, user, info.IP)
		info.UpdatesAvailable, info.UpdatesTotal, info.UpdateChecked = avail, total, checked
		if app.MatchAll && len(info.Replicas) > 0 {
			info.Replicas[0].UpdatesAvailable = avail
			info.Replicas[0].UpdatesTotal = total
			info.Replicas[0].UpdateChecked = checked
		}
	}

	// Per-replica update check for match_all apps. Replicas can drift
	// independently (one node pulled twingate/connector:1 yesterday, the
	// other last week), so each gets its own probe.
	if app.MatchAll {
		for i, r := range matches {
			if i == 0 {
				continue // already filled above
			}
			if r.Status != "running" || info.Replicas[i].IP == "" {
				continue
			}
			rUser := o.appSSHUser(app, r.Type, r.Node, r.VMID)
			avail, total, checked := o.checkAppUpdates(ctx, app, rUser, info.Replicas[i].IP)
			info.Replicas[i].UpdatesAvailable = avail
			info.Replicas[i].UpdatesTotal = total
			info.Replicas[i].UpdateChecked = checked
		}
	}
	return info
}

// checkAppUpdates probes a running guest for available updates and returns
// (available, total, checked). checked=false means the probe couldn't run
// (no compose dir, no SSH access, slow registry, etc.) — the UI shows "—"
// for unchecked rows. If app.UpdateCheck is set, that command runs verbatim;
// otherwise a built-in compose-image-digest check runs against /opt/<name>/.
//
// Probe contract: stdout must contain one line "UPDATES:n/total". The script
// always emits this on a clean run, even when nothing's installed (0/0).
func (o *Orchestrator) checkAppUpdates(ctx context.Context, app config.App, user, ip string) (available, total int, checked bool) {
	if ip == "" || user == "" {
		return 0, 0, false
	}
	var script string
	if app.UpdateCheck != "" {
		script = app.UpdateCheck
	} else {
		// Built-in image-digest check, probe order:
		//   1. /opt/<app>/docker-compose.yml — enumerate via `docker
		//      compose config --images` (catches compose stacks even when
		//      no container is currently running).
		//   2. fallback: `docker ps --format '{{.Image}}'` — catches plain
		//      `docker run` services (e.g. twingate/connector:1).
		// For each image, compare local RepoDigest against the registry's
		// current digest (buildx imagetools preferred; falls back to
		// `docker manifest inspect`). Counts mismatches as updates.
		script = fmt.Sprintf(`imgs=""
if [ -f /opt/%s/docker-compose.yml ] || [ -f /opt/%s/compose.yml ] || [ -f /opt/%s/docker-compose.yaml ] || [ -f /opt/%s/compose.yaml ]; then
  imgs=$(cd /opt/%s && docker compose config --images 2>/dev/null)
fi
if [ -z "$imgs" ]; then
  imgs=$(docker ps --format '{{.Image}}' 2>/dev/null | sort -u)
fi
if [ -z "$imgs" ]; then
  echo "UPDATES:0/0"
  exit 0
fi
upd=0; tot=0
while IFS= read -r img; do
  [ -z "$img" ] && continue
  tot=$((tot+1))
  local=$(docker image inspect "$img" --format '{{range .RepoDigests}}{{println .}}{{end}}' 2>/dev/null | head -1 | cut -d@ -f2)
  remote=$(docker buildx imagetools inspect "$img" 2>/dev/null | awk '/^Digest:/ {print $2; exit}')
  if [ -z "$remote" ]; then
    remote=$(docker manifest inspect "$img" 2>/dev/null | grep -oE 'sha256:[a-f0-9]{64}' | head -1)
  fi
  if [ -n "$local" ] && [ -n "$remote" ] && [ "$local" != "$remote" ]; then
    upd=$((upd+1))
  fi
done <<INNER_EOF
$imgs
INNER_EOF
echo "UPDATES:$upd/$tot"
`,
			shellQuoteForSSH(app.Name),
			shellQuoteForSSH(app.Name),
			shellQuoteForSSH(app.Name),
			shellQuoteForSSH(app.Name),
			shellQuoteForSSH(app.Name),
		)
	}
	chkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(chkCtx, "ssh",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=4",
		"-o", "StrictHostKeyChecking=accept-new",
		user+"@"+ip,
		script,
	).Output()
	if err != nil {
		return 0, 0, false
	}
	return parseUpdatesLine(string(out))
}

// parseUpdatesLine scans probe output for a "UPDATES:n/total" line and
// returns the parsed counts. Lenient on whitespace and stray lines.
func parseUpdatesLine(s string) (available, total int, ok bool) {
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		if !strings.HasPrefix(ln, "UPDATES:") {
			continue
		}
		rest := strings.TrimPrefix(ln, "UPDATES:")
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) != 2 {
			continue
		}
		a, errA := strconv.Atoi(strings.TrimSpace(parts[0]))
		t, errT := strconv.Atoi(strings.TrimSpace(parts[1]))
		if errA == nil && errT == nil {
			return a, t, true
		}
	}
	return 0, 0, false
}

// fetchGuestOS returns the per-instance OS string (same shape used in
// AppRuntimeInfo.OS): full pretty-name for VMs via guest-agent, "<distro>
// (LXC)" for LXCs via config. Empty if the lookup fails.
func (o *Orchestrator) fetchGuestOS(ctx context.Context, guestType, node string, vmid int) string {
	osCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	switch guestType {
	case "qemu":
		if osi, err := o.client.GuestAgentOSInfo(osCtx, node, vmid); err == nil {
			if osi.PrettyName != "" {
				return osi.PrettyName
			}
			return osi.Name
		}
	case "lxc":
		if cfg, err := o.client.GetLXCConfig(osCtx, node, vmid); err == nil {
			if t := cfg["ostype"]; t != "" {
				return t + " (LXC)"
			}
		}
	}
	return ""
}

// fetchGuestIP returns the first IPv4 a running guest reports — qemu-agent
// for VMs, container interfaces table for LXCs. Empty string on failure.
func (o *Orchestrator) fetchGuestIP(ctx context.Context, guestType, node string, vmid int) string {
	ipCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	switch guestType {
	case "qemu":
		if ifs, err := o.client.GuestAgentNetInterfaces(ipCtx, node, vmid); err == nil {
			return proxmox.FirstIPv4(ifs)
		}
	case "lxc":
		if ifs, err := o.client.LXCInterfaces(ipCtx, node, vmid); err == nil {
			return proxmox.FirstLXCIPv4(ifs)
		}
	}
	return ""
}

// appSSHUser determines the SSH login user for an app's guest. LXCs always
// log in as root. For VMs, the catalog's image.Distro is unreliable in
// practice (operators redeploy with different images, drift over time, etc.)
// so we query the guest agent's osinfo for the actual distro and derive the
// user from that. Falls back to the catalog hint, then "root".
func (o *Orchestrator) appSSHUser(app config.App, guestType, node string, vmid int) string {
	if guestType == "lxc" {
		return "root"
	}
	// Live osinfo query — authoritative.
	osCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if osi, err := o.client.GuestAgentOSInfo(osCtx, node, vmid); err == nil && osi.ID != "" {
		if u, ok := o.cfg.Cluster.SSH.Users[osi.ID]; ok && u != "" {
			return u
		}
	}
	// Catalog fallback.
	if image, err := o.resolveImage(app.Image); err == nil {
		return o.defaultUser(image)
	}
	return "root"
}

// parseComposeSummary turns `docker compose ps --format '{{.Service}}|...'`
// output into a slice of ComposeContainer. Each non-empty line is one row.
func parseComposeSummary(raw string) []ComposeContainer {
	var out []ComposeContainer
	for _, ln := range strings.Split(strings.TrimSpace(raw), "\n") {
		if ln == "" {
			continue
		}
		fields := strings.SplitN(ln, "|", 3)
		if len(fields) < 2 {
			continue
		}
		c := ComposeContainer{Service: fields[0], Image: fields[1]}
		if len(fields) >= 3 {
			c.State = fields[2]
		}
		out = append(out, c)
	}
	return out
}

// shellQuoteForSSH single-quotes a value so it survives the remote shell's
// own parsing of the SSH command argument. Local quoting at the exec.Command
// level handles arg passing already; this guard is for the case where the
// value contains characters meaningful to the REMOTE shell.
func shellQuoteForSSH(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}

// PatchApp runs the app's update command against the (single) guest named
// app.Name. For match_all apps the caller should use PatchAppInstance per
// replica instead — the TUI emits one row per replica and dispatches them
// independently so each can succeed/fail/log on its own.
func (o *Orchestrator) PatchApp(ctx context.Context, app config.App) error {
	if app.Update == "" {
		return fmt.Errorf("patch: app %q has no `update:` command in cluster.yaml", app.Name)
	}
	o.emit(StepResolve, fmt.Sprintf("locating guest %q", app.Name), 5)
	resources, err := o.client.GetResources(ctx, "vm")
	if err != nil {
		return fmt.Errorf("patch: list cluster resources: %w", err)
	}
	for i := range resources {
		r := &resources[i]
		if r.Template == 1 {
			continue
		}
		if r.Name == app.Name {
			return o.patchOneGuest(ctx, app, r)
		}
	}
	return fmt.Errorf("patch: no guest named %q on the cluster (deploy via [a]apps first)", app.Name)
}

// PatchAppInstance runs the app's update command against a specific guest
// (by VMID+Node). Used for match_all apps where the TUI emits one row per
// replica — each instance gets its own patch task, its own success/failure,
// its own SSH session.
func (o *Orchestrator) PatchAppInstance(ctx context.Context, app config.App, vmid int, node string) error {
	if app.Update == "" {
		return fmt.Errorf("patch: app %q has no `update:` command in cluster.yaml", app.Name)
	}
	o.emit(StepResolve, fmt.Sprintf("locating %s/%d on %s", app.Name, vmid, node), 5)
	resources, err := o.client.GetResources(ctx, "vm")
	if err != nil {
		return fmt.Errorf("patch: list cluster resources: %w", err)
	}
	for i := range resources {
		r := &resources[i]
		if r.VMID == vmid && r.Node == node {
			return o.patchOneGuest(ctx, app, r)
		}
	}
	return fmt.Errorf("patch: VMID %d on %s not found (destroyed since last refresh?)", vmid, node)
}

// patchOneGuest runs `app.Update` against a single resolved guest. Used by
// both PatchApp and PatchAppInstance.
func (o *Orchestrator) patchOneGuest(ctx context.Context, app config.App, found *proxmox.Resource) error {
	if found.Status != "running" {
		return fmt.Errorf("%s/%d on %s is %s — start it before patching",
			found.Type, found.VMID, found.Node, found.Status)
	}
	o.emit(StepResolve, fmt.Sprintf("resolving IP of %s/%d on %s",
		found.Type, found.VMID, found.Node), 12)
	ip := o.fetchGuestIP(ctx, found.Type, found.Node, found.VMID)
	if ip == "" {
		return fmt.Errorf("no IP reported for %s/%d (running but networking not up?)",
			found.Type, found.VMID)
	}
	user := o.appSSHUser(app, found.Type, found.Node, found.VMID)
	target := user + "@" + ip
	o.emit(StepConfigure, fmt.Sprintf("ssh %s — running update command", target), 20)
	if err := o.sshStream(ctx, target, app.Update, 15*time.Minute); err != nil {
		return err
	}
	o.emit(StepDone, fmt.Sprintf("patched %s/%d on %s (@ %s)",
		found.Type, found.VMID, found.Node, ip), 100)
	return nil
}
