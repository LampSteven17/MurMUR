package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rtx-monster/murmur/internal/config"
	"github.com/rtx-monster/murmur/internal/provision"
	"github.com/rtx-monster/murmur/internal/proxmox"
)

// appsState mirrors teardownState — picker, confirm, secrets, batch run.
//
// The optional appsSecrets state only fires when at least one of the resolved
// deploy targets has Secrets declared in cluster.yaml. It collects per-target
// values from the operator before any provisioning starts.
type appsState int

const (
	appsPicking appsState = iota
	appsConfirm
	appsSecrets
	appsRunning
	appsDone
	appsFailed
)

// deployTarget is one provisioning job — a single guest on a specific node.
// match_all apps produce one target per node that doesn't already host a
// replica; single-instance apps produce one target with Node="" (best-fit).
// Secrets are filled in by the appsSecrets prompt; nil for apps that declare
// none.
type deployTarget struct {
	app     config.App
	node    string            // pinned node (match_all); "" = best-fit
	secrets map[string]string // env-var name → operator-supplied value
}

// label returns "app-name" or "app-name @ node" depending on whether the
// target is pinned. Used for picker/confirm/running rows.
func (t deployTarget) label() string {
	if t.node == "" {
		return t.app.Name
	}
	return t.app.Name + " @ " + t.node
}

// Bridges from the orchestrator goroutine into Update.
type appsProgressMsg provision.ProgressEvent
type appsItemStartMsg struct{ idx int }
type appsItemDoneMsg struct {
	idx int
	err error
}
type appsAllDoneMsg struct{}

// appResult records one queued target's outcome.
type appResult struct {
	target deployTarget
	done   bool
	err    error
}

// AppsView is the apps catalog picker — declarative deploys from cluster.yaml.
type AppsView struct {
	cfg        *config.Config
	client     *proxmox.Client
	active     *config.ActiveUser
	styles     Styles
	configDir  string // dir containing cluster.yaml; WorkDir for post-deploy

	// picker state
	cursor   int
	selected map[string]bool // app name → picked
	rows     []proxmox.Resource // current cluster guests (for collision detection)
	loading  bool
	loaded   bool
	err      error
	fetched  time.Time

	// batch run state
	state     appsState
	queue     []deployTarget
	results   []appResult
	queueIdx  int
	activeMsg string
	activePct float64
	msgs      chan tea.Msg

	// Secrets-prompt state. secretInputs is flat across all targets;
	// secretInputFor[i] = (targetIdx, secretName) tells us where to write
	// the collected value when the operator advances.
	secretInputs   []textinput.Model
	secretInputFor []secretInputBinding
	secretFocus    int

	keys appsKeyMap
}

// secretInputBinding ties a textinput.Model index back to (target, secret name).
type secretInputBinding struct {
	targetIdx  int
	secretName string
}

type appsKeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Toggle   key.Binding
	SelAll   key.Binding
	ClearAll key.Binding
	Confirm  key.Binding
	Yes      key.Binding
	No       key.Binding
	Back     key.Binding
	NewAgain key.Binding
}

// NewAppsView wires the apps tab. configPath is used as the working directory
// for playbook/post-deploy execution so relative paths resolve as the
// operator expects.
func NewAppsView(cfg *config.Config, client *proxmox.Client, active *config.ActiveUser, configPath string) *AppsView {
	return &AppsView{
		cfg:       cfg,
		client:    client,
		active:    active,
		styles:    NewStyles(DefaultTheme),
		configDir: filepath.Dir(configPath),
		selected:  map[string]bool{},
		keys: appsKeyMap{
			Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
			Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
			Toggle:   key.NewBinding(key.WithKeys(" ", "x"), key.WithHelp("space", "toggle")),
			SelAll:   key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "all")),
			ClearAll: key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "none")),
			Confirm:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "deploy")),
			Yes:      key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "deploy")),
			No:       key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "no")),
			Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
			NewAgain: key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "deploy more")),
		},
	}
}

func (v *AppsView) Init() tea.Cmd {
	v.loading = true
	return v.fetch()
}

func (v *AppsView) Title() string { return "apps" }

// CapturesKeys is true while the secrets-prompt textinputs are focused, so
// the parent app stops intercepting global hotkeys (otherwise typing a "q"
// in a token field would quit the program).
func (v *AppsView) CapturesKeys() bool { return v.state == appsSecrets }

func (v *AppsView) Help() []key.Binding {
	switch v.state {
	case appsConfirm:
		return []key.Binding{v.keys.Yes, v.keys.No}
	case appsSecrets:
		return []key.Binding{v.keys.Up, v.keys.Down, v.keys.Confirm, v.keys.Back}
	case appsDone, appsFailed:
		return []key.Binding{v.keys.Back}
	default:
		return []key.Binding{v.keys.Up, v.keys.Down, v.keys.Toggle, v.keys.SelAll, v.keys.ClearAll, v.keys.Confirm}
	}
}


func (v *AppsView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch m := msg.(type) {
	case RefreshMsg:
		if v.state != appsPicking {
			return v, nil
		}
		v.loading = true
		v.err = nil
		return v, v.fetch()
	case ClusterDataMsg:
		v.loading = false
		v.loaded = true
		if m.Err != nil {
			v.err = m.Err
			return v, nil
		}
		v.err = nil
		v.fetched = time.Now()
		// Filter guests by owner tag for non-admin operators so coverage and
		// collision indicators reflect only what this operator sees. Admins
		// + the legacy fallback path keep the full list.
		v.rows = ownerFilter(v.active, m.Resources)
		return v, nil
	case appsProgressMsg:
		v.activePct = m.Percent
		v.activeMsg = m.Message
		return v, v.readNext()
	case appsItemStartMsg:
		v.queueIdx = m.idx
		v.activeMsg = ""
		v.activePct = 0
		return v, v.readNext()
	case appsItemDoneMsg:
		if m.idx >= 0 && m.idx < len(v.results) {
			v.results[m.idx].done = true
			v.results[m.idx].err = m.err
		}
		return v, v.readNext()
	case appsAllDoneMsg:
		failed := 0
		for _, r := range v.results {
			if r.err != nil {
				failed++
			}
		}
		if failed > 0 {
			v.state = appsFailed
		} else {
			v.state = appsDone
		}
		return v, nil
	case tea.KeyMsg:
		switch v.state {
		case appsPicking:
			return v.updatePicking(m)
		case appsConfirm:
			return v.updateConfirm(m)
		case appsSecrets:
			return v.updateSecrets(m)
		case appsDone, appsFailed:
			switch {
			case key.Matches(m, v.keys.NewAgain), key.Matches(m, v.keys.Back):
				v.resetToList()
				return v, v.fetch()
			}
		}
	}
	return v, nil
}

// ── picking ────────────────────────────────────────────────────────────────

// catalog returns the apps the active operator may deploy — filtered by the
// role's apps: list via appAllowed (fallback admin / "*" sees them all). The
// picker, render, and resolveTargets all run through this, so a scoped operator
// (e.g. a deployer limited to one app) never sees or deploys the rest.
func (v *AppsView) catalog() []config.App {
	out := make([]config.App, 0, len(v.cfg.Apps))
	for _, a := range v.cfg.Apps {
		if appAllowed(v.active, a.Name) {
			out = append(out, a)
		}
	}
	return out
}

func (v *AppsView) updatePicking(m tea.KeyMsg) (View, tea.Cmd) {
	apps := v.catalog()
	switch {
	case key.Matches(m, v.keys.Up):
		if v.cursor > 0 {
			v.cursor--
		}
	case key.Matches(m, v.keys.Down):
		if v.cursor < len(apps)-1 {
			v.cursor++
		}
	case key.Matches(m, v.keys.Toggle):
		if v.cursor >= 0 && v.cursor < len(apps) {
			name := apps[v.cursor].Name
			if v.selected[name] {
				delete(v.selected, name)
			} else {
				v.selected[name] = true
			}
		}
	case key.Matches(m, v.keys.SelAll):
		for _, a := range apps {
			v.selected[a.Name] = true
		}
	case key.Matches(m, v.keys.ClearAll):
		v.selected = map[string]bool{}
	case key.Matches(m, v.keys.Confirm):
		// Single-select fallback to cursor row.
		if len(v.selected) == 0 && len(apps) > 0 {
			v.selected[apps[v.cursor].Name] = true
		}
		if len(v.selected) > 0 {
			v.state = appsConfirm
		}
	}
	return v, nil
}

// ── confirm ────────────────────────────────────────────────────────────────

func (v *AppsView) updateConfirm(m tea.KeyMsg) (View, tea.Cmd) {
	switch {
	case key.Matches(m, v.keys.Yes):
		// Resolve picked apps → one or more deploy targets each (fan out
		// match_all apps), then either prompt for secrets or jump straight
		// to the deploy loop.
		v.queue = v.resolveTargets()
		if len(v.queue) == 0 {
			// Everything we picked collided. Stay in confirm so the
			// operator can see the skip list and back out.
			return v, nil
		}
		if v.targetsNeedSecrets() {
			v.beginSecretsPrompt()
			return v, textinput.Blink
		}
		return v, v.startBatch()
	case key.Matches(m, v.keys.No), key.Matches(m, v.keys.Back):
		v.state = appsPicking
	}
	return v, nil
}

// ── secrets prompt ─────────────────────────────────────────────────────────

// updateSecrets handles input while the operator is filling per-replica
// secret values. Tab/down/up navigates between inputs; enter dispatches the
// batch once every field is non-empty; esc returns to confirm.
func (v *AppsView) updateSecrets(m tea.KeyMsg) (View, tea.Cmd) {
	switch {
	case m.Type == tea.KeyEsc:
		v.state = appsConfirm
		v.secretInputs = nil
		v.secretInputFor = nil
		return v, nil
	case m.Type == tea.KeyTab, m.Type == tea.KeyDown:
		// Navigation keys are restricted to raw tab/arrows here — using
		// v.keys.Down would treat "j" as down, swallowing it from token
		// text input.
		v.focusSecretInput((v.secretFocus + 1) % len(v.secretInputs))
		return v, textinput.Blink
	case m.Type == tea.KeyShiftTab, m.Type == tea.KeyUp:
		idx := v.secretFocus - 1
		if idx < 0 {
			idx = len(v.secretInputs) - 1
		}
		v.focusSecretInput(idx)
		return v, textinput.Blink
	case m.Type == tea.KeyEnter:
		// Validate: every field must be non-empty. On miss, jump to it.
		for i, in := range v.secretInputs {
			if strings.TrimSpace(in.Value()) == "" {
				v.focusSecretInput(i)
				return v, textinput.Blink
			}
		}
		// Pour collected values back into the queue's per-target maps.
		for i, b := range v.secretInputFor {
			if v.queue[b.targetIdx].secrets == nil {
				v.queue[b.targetIdx].secrets = map[string]string{}
			}
			v.queue[b.targetIdx].secrets[b.secretName] = v.secretInputs[i].Value()
		}
		v.secretInputs = nil
		v.secretInputFor = nil
		return v, v.startBatch()
	}
	// Forward the keypress to the focused textinput.
	if v.secretFocus < len(v.secretInputs) {
		var cmd tea.Cmd
		v.secretInputs[v.secretFocus], cmd = v.secretInputs[v.secretFocus].Update(m)
		return v, cmd
	}
	return v, nil
}

// focusSecretInput moves focus to input idx and toggles the bubbles cursor.
func (v *AppsView) focusSecretInput(idx int) {
	if idx < 0 || idx >= len(v.secretInputs) {
		return
	}
	for i := range v.secretInputs {
		if i == idx {
			v.secretInputs[i].Focus()
		} else {
			v.secretInputs[i].Blur()
		}
	}
	v.secretFocus = idx
}

// beginSecretsPrompt builds one textinput per (target, secret) pair so the
// operator pastes each value in sequence. Inputs are flat — they render
// grouped by target in renderSecretsPrompt.
func (v *AppsView) beginSecretsPrompt() {
	v.secretInputs = v.secretInputs[:0]
	v.secretInputFor = v.secretInputFor[:0]
	for ti, t := range v.queue {
		for _, s := range t.app.Secrets {
			in := textinput.New()
			in.CharLimit = 4096
			in.Width = 60
			in.Placeholder = s.Prompt
			if in.Placeholder == "" {
				in.Placeholder = s.Name
			}
			v.secretInputs = append(v.secretInputs, in)
			v.secretInputFor = append(v.secretInputFor, secretInputBinding{
				targetIdx: ti, secretName: s.Name,
			})
		}
	}
	v.state = appsSecrets
	v.secretFocus = 0
	if len(v.secretInputs) > 0 {
		v.secretInputs[0].Focus()
	}
}

// targetsNeedSecrets reports whether any queued target has at least one
// secret declared in cluster.yaml.
func (v *AppsView) targetsNeedSecrets() bool {
	for _, t := range v.queue {
		if len(t.app.Secrets) > 0 {
			return true
		}
	}
	return false
}

// resolveTargets expands picked apps into provisioning targets:
//   - Single-instance app already running → skipped (collision)
//   - Single-instance app not running → one target, node="" (best-fit)
//   - match_all app → one target per node that doesn't already host a replica
func (v *AppsView) resolveTargets() []deployTarget {
	nodesPerApp := v.nodesRunningByApp()
	var out []deployTarget
	for _, a := range v.catalog() {
		if !v.selected[a.Name] {
			continue
		}
		if !a.MatchAll {
			if len(nodesPerApp[a.Name]) > 0 {
				continue // collision — already running
			}
			out = append(out, deployTarget{app: a})
			continue
		}
		// match_all: deploy to the NEXT missing node only — one at a time.
		// The picker shows "(N/M nodes deployed)" so the operator can see
		// progress and re-trigger to add another. Doing all missing nodes
		// in one batch is too aggressive: it stacks 6 token-paste fields
		// for replicated services, hides per-replica failures behind a
		// batch summary, and treats deploying to fresh hardware as a
		// one-shot mass-rollout when it's usually an incremental decision.
		have := map[string]bool{}
		for _, n := range nodesPerApp[a.Name] {
			have[n] = true
		}
		for _, n := range v.cfg.Cluster.Nodes {
			if have[n.Name] {
				continue
			}
			out = append(out, deployTarget{app: a, node: n.Name})
			break // one per invocation; rerun [a]apps to add another
		}
	}
	return out
}

// nodesRunningByApp returns guest-name → list of node names where a guest of
// that name currently exists (running OR stopped — both block a fresh deploy).
func (v *AppsView) nodesRunningByApp() map[string][]string {
	out := map[string][]string{}
	for _, r := range v.rows {
		if (r.Type != "qemu" && r.Type != "lxc") || r.Template == 1 {
			continue
		}
		if r.Status != "running" && r.Status != "stopped" {
			continue
		}
		out[r.Name] = append(out[r.Name], r.Node)
	}
	return out
}

// ── batch run ──────────────────────────────────────────────────────────────

// startBatch dispatches v.queue to the orchestrator sequentially. The queue
// must already be populated by resolveTargets() (called from confirm-yes);
// startBatch does not rebuild it.
func (v *AppsView) startBatch() tea.Cmd {
	v.results = make([]appResult, len(v.queue))
	for i := range v.queue {
		v.results[i].target = v.queue[i]
	}
	v.queueIdx = 0
	v.activeMsg = ""
	v.activePct = 0
	v.state = appsRunning
	v.msgs = make(chan tea.Msg, 256)

	orch := provision.New(v.cfg, v.client)
	orch.SetActiveUser(v.active)
	msgs := v.msgs
	orch.SetProgress(func(ev provision.ProgressEvent) {
		select {
		case msgs <- appsProgressMsg(ev):
		default:
		}
	})

	queue := v.queue
	configDir := v.configDir
	go func() {
		for i, t := range queue {
			msgs <- appsItemStartMsg{idx: i}
			gtype := t.app.Type
			if gtype == "" {
				gtype = "vm"
			}
			req := provision.Request{
				Name:              t.app.Name,
				Type:              gtype,
				Image:             t.app.Image,
				Flavor:            t.app.Flavor,
				AppName:           t.app.Name, // stamps `murmur-app-<name>` so patch/apps tabs can match by tag
				TargetNode:        t.node,
				PostDeployCommand: buildPostDeployCommand(t.app, configDir),
				PostDeployRemote:  t.app.PostDeploy != "", // raw shell → run on guest
				WorkDir:           configDir,
				SecretEnv:         t.secrets,
			}
			_, err := orch.Deploy(context.Background(), req)
			msgs <- appsItemDoneMsg{idx: i, err: err}
		}
		msgs <- appsAllDoneMsg{}
		close(msgs)
	}()
	return v.readNext()
}

// buildPostDeployCommand returns the shell command murmur runs after the
// guest IP resolves. Playbook paths get wrapped with `ansible-playbook -i
// $GUEST_IP, -u $GUEST_USER ...`; raw shell commands pass through. Empty if
// neither field is set (provision-only).
func buildPostDeployCommand(app config.App, _ string) string {
	switch {
	case app.Playbook != "":
		// Strict host-key checking is disabled because the guest is brand
		// new — no entry in known_hosts yet. Operators who want stronger
		// guarantees can move the host-key flag to ~/.ansible.cfg and set
		// PostDeploy instead of Playbook.
		// Pass both `app_name` (lightlab's playbook convention) and `vm_name`
		// (murmur's earlier convention) so playbooks written either way work.
		return fmt.Sprintf(
			"ansible-playbook -i ${GUEST_IP}, -u ${GUEST_USER} "+
				"-e app_name=${GUEST_NAME} -e vm_name=${GUEST_NAME} -e vm_ip=${GUEST_IP} "+
				"--ssh-extra-args='-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null' "+
				"%s",
			shellQuote(app.Playbook))
	case app.PostDeploy != "":
		return app.PostDeploy
	}
	return ""
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes,
// so a playbook path containing spaces or shell metacharacters survives the
// /bin/sh -c invocation.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// runningNames returns the set of guest names currently present (running or
// stopped) on the cluster, used for collision detection in the picker view.
// Match_all apps are accepted as long as not every node already hosts one —
// the per-node check happens in resolveTargets.
func (v *AppsView) runningNames() map[string]bool {
	out := map[string]bool{}
	for _, r := range v.rows {
		if (r.Type != "qemu" && r.Type != "lxc") || r.Template == 1 {
			continue
		}
		if r.Status != "running" && r.Status != "stopped" {
			continue
		}
		out[r.Name] = true
	}
	return out
}

func (v *AppsView) readNext() tea.Cmd {
	msgs := v.msgs
	return func() tea.Msg {
		msg, ok := <-msgs
		if !ok {
			return nil
		}
		return msg
	}
}

func (v *AppsView) resetToList() {
	v.state = appsPicking
	v.queue = nil
	v.results = nil
	v.queueIdx = 0
	v.activeMsg = ""
	v.activePct = 0
	v.selected = map[string]bool{}
	v.secretInputs = nil
	v.secretInputFor = nil
	v.secretFocus = 0
	v.loading = true
}

// fetch pulls /cluster/resources just to know which names are taken.
func (v *AppsView) fetch() tea.Cmd {
	client := v.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		resources, err := client.GetResources(ctx, "")
		if err != nil {
			return ClusterDataMsg{Err: err}
		}
		return ClusterDataMsg{Resources: resources}
	}
}

// ── rendering ──────────────────────────────────────────────────────────────

func (v *AppsView) View(width, height int) string {
	header := v.styles.Title.Render("⚗  APPS")
	right := v.styles.Subtle.Render(v.subtitle())
	gap := width - lipgloss.Width(header) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	headLine := header + lipgloss.NewStyle().Width(gap).Render("") + right

	var body string
	switch v.state {
	case appsPicking:
		body = v.renderPicking(width)
	case appsConfirm:
		body = v.renderConfirm(width)
	case appsSecrets:
		body = v.renderSecretsPrompt(width)
	case appsRunning, appsDone, appsFailed:
		body = v.renderRunning(width)
	}
	return padBlock(headLine+"\n\n"+body, width, height)
}

func (v *AppsView) subtitle() string {
	switch v.state {
	case appsPicking:
		if n := len(v.selected); n > 0 {
			return fmt.Sprintf("%d selected · space toggle · ⏎ deploy", n)
		}
		return "deploy from catalog · space toggle · ⏎ deploy"
	case appsConfirm:
		return "confirm deployment"
	case appsSecrets:
		return fmt.Sprintf("paste per-replica secrets · %d field(s)", len(v.secretInputs))
	case appsRunning:
		return fmt.Sprintf("deploying %d of %d", v.completedCount(), len(v.queue))
	case appsDone:
		return "all deployed"
	case appsFailed:
		return fmt.Sprintf("%d failed", v.failedCount())
	}
	return ""
}

func (v *AppsView) renderPicking(width int) string {
	switch {
	case v.err != nil:
		return " " + v.styles.StatusError.Render(Glyph.Error) + " " +
			lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Render(v.err.Error())
	case v.loading && !v.loaded:
		return " " + v.styles.Subtle.Render(Glyph.InFlight+" consulting the conclave…")
	case len(v.catalog()) == 0:
		return " " + v.styles.Subtle.Render(Glyph.Empty+
			" no apps available — none declared in cluster.yaml, or your role grants none.")
	}

	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	ink := lipgloss.NewStyle().Foreground(DefaultTheme.Ink)
	gold := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	verm := lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Bold(true)
	verd := lipgloss.NewStyle().Foreground(DefaultTheme.Verdigris).Bold(true)
	nodesPerApp := v.nodesRunningByApp()
	nodeCount := len(v.cfg.Cluster.Nodes)

	apps := v.catalog()
	lines := []string{v.styles.Title.Render("APPS") + "  " + muted.Render(fmt.Sprintf("(%d)", len(apps)))}
	for i, a := range apps {
		cursor := "  "
		if v.cursor == i {
			cursor = v.styles.SelectionMark.Render("▶ ")
		}
		check := muted.Render("[ ]")
		if v.selected[a.Name] {
			check = verm.Render("[x]")
		}
		recipe := postDeploySummary(a)
		gtype := a.Type
		if gtype == "" {
			gtype = "vm"
		}
		// Trailing status column: match_all apps show coverage; single-
		// instance apps show a collision marker. Secret-bearing apps don't
		// get a picker badge — the operator sees the per-replica count on
		// the confirm screen, where there's room for context.
		status := ""
		switch {
		case a.MatchAll:
			have := len(nodesPerApp[a.Name])
			if have >= nodeCount {
				// Goal state for a match_all app — green, no warning vibe.
				status = verd.Render(fmt.Sprintf("✓ deployed on all %d node(s)", nodeCount))
			} else {
				status = muted.Render(fmt.Sprintf("(%d/%d nodes deployed)", have, nodeCount))
			}
		case len(nodesPerApp[a.Name]) > 0:
			status = verm.Render("(collision — already present)")
		}
		row := fmt.Sprintf("%s%s  %s  %s  %s  %s  %s  %s",
			cursor,
			check,
			gold.Render(padRight(a.Name, 20)),
			ink.Render(padRight(gtype, 4)),
			ink.Render(padRight(a.Image, 14)),
			ink.Render(padRight(a.Flavor, 12)),
			muted.Render(padRight(truncate(recipe, 50), 50)),
			status,
		)
		lines = append(lines, row)
	}
	if !v.fetched.IsZero() {
		hb := v.styles.Heartbeat.Render("◉")
		ts := muted.Render(fmt.Sprintf("fetched %s", v.fetched.Format("15:04:05")))
		lines = append(lines, "")
		lines = append(lines, " "+hb+" "+ts+"  "+muted.Render(
			"· space toggle · A all · N none · ⏎ deploy"))
	}
	return strings.Join(lines, "\n")
}

// postDeploySummary returns a one-line description of what murmur will run
// after the guest is up: the playbook path, a single-line preview of the
// raw command, or "(provision only)" for apps with no post-deploy. Multi-
// line shell blocks are collapsed to the first non-blank line plus an
// ellipsis so the confirm screen doesn't explode vertically.
func postDeploySummary(a config.App) string {
	switch {
	case a.Playbook != "":
		return "playbook: " + a.Playbook
	case a.PostDeploy != "":
		return "shell: " + firstShellLine(a.PostDeploy)
	}
	return "(provision only)"
}

// firstShellLine returns the first non-blank line of s, with an ellipsis
// suffix if there's more content after it. Used to summarize multi-line
// post_deploy blocks in single-row UI.
func firstShellLine(s string) string {
	var first string
	more := false
	for _, ln := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(ln)
		if first == "" {
			if trimmed == "" {
				continue
			}
			first = trimmed
			continue
		}
		if trimmed != "" {
			more = true
			break
		}
	}
	if more {
		return first + " …"
	}
	return first
}

func (v *AppsView) renderConfirm(width int) string {
	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	ink := lipgloss.NewStyle().Foreground(DefaultTheme.Ink).Bold(true)
	gold := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	verm := lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Bold(true)

	// Preview-only: regenerate targets so the screen shows exactly what
	// will be deployed. The actual queue is built fresh from this same
	// helper when the operator presses [y].
	targets := v.resolveTargets()
	skipped := v.skippedFromPicked()

	auth := "key"
	if v.cfg.Cluster.SSH.Password != "" {
		auth = "key+password"
		if v.cfg.Cluster.SSH.Identity == "" {
			auth = "password"
		}
	}

	totalSecrets := 0
	for _, t := range targets {
		totalSecrets += len(t.app.Secrets)
	}

	lines := []string{
		" " + ink.Render(fmt.Sprintf("About to deploy %d target(s):", len(targets))),
		"",
	}
	for _, t := range targets {
		gtype := t.app.Type
		if gtype == "" {
			gtype = "vm"
		}
		secretTag := ""
		if n := len(t.app.Secrets); n > 0 {
			secretTag = "  " + verm.Render(fmt.Sprintf("· %d secret(s) to paste", n))
		}
		lines = append(lines, fmt.Sprintf("    %s  %s  %s  %s%s",
			gold.Render(padRight(t.label(), 28)),
			ink.Render(padRight(gtype, 4)),
			ink.Render(padRight(t.app.Image, 14)),
			muted.Render(postDeploySummary(t.app)),
			secretTag,
		))
	}
	// Split skipped into two buckets so the operator sees the right
	// remediation hint: match_all apps that are already deployed on every
	// declared node are at the desired state (add more nodes to scale up),
	// not a "collision" to be torn down. Single-instance apps with a guest
	// of that name already running ARE collisions.
	var fullMatchAll, collisions []config.App
	for _, a := range skipped {
		if a.MatchAll {
			fullMatchAll = append(fullMatchAll, a)
		} else {
			collisions = append(collisions, a)
		}
	}
	verd := lipgloss.NewStyle().Foreground(DefaultTheme.Verdigris).Bold(true)
	if len(fullMatchAll) > 0 {
		lines = append(lines, "")
		lines = append(lines, " "+verd.Render(fmt.Sprintf(
			"✓ %d already deployed on every node:", len(fullMatchAll))))
		for _, a := range fullMatchAll {
			lines = append(lines, "    "+gold.Render(a.Name)+
				muted.Render("  (add cluster.nodes entries to scale out)"))
		}
	}
	if len(collisions) > 0 {
		lines = append(lines, "")
		lines = append(lines, " "+verm.Render(fmt.Sprintf(
			"%d will be SKIPPED (collision — already running):", len(collisions))))
		for _, a := range collisions {
			lines = append(lines, "    "+verm.Render(a.Name)+
				muted.Render("  (teardown first to redeploy)"))
		}
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  %s %s   %s %s",
		muted.Render("auth:"), ink.Render(auth),
		muted.Render("placement:"), ink.Render("auto (best-fit per target unless pinned)"),
	))
	if totalSecrets > 0 {
		lines = append(lines, "  "+muted.Render(fmt.Sprintf(
			"secrets:")) + " " + ink.Render(fmt.Sprintf(
			"%d total — you'll be prompted next", totalSecrets)))
	}
	lines = append(lines, "")
	yes := verm.Render("[y]")
	no := gold.Render("[n]")
	if len(targets) == 0 {
		lines = append(lines, "    "+muted.Render("nothing to deploy after skips — esc to go back"))
	} else {
		next := "deploy"
		if totalSecrets > 0 {
			next = "next: enter secrets"
		}
		lines = append(lines, "    "+yes+" "+ink.Render(next)+"      "+no+" "+ink.Render("cancel"))
	}
	return strings.Join(lines, "\n")
}

// skippedFromPicked returns picked apps that are fully covered cluster-wide:
// a single-instance app already has any guest of that name; a match_all app
// already has one per cluster node. Used only by the confirm preview.
func (v *AppsView) skippedFromPicked() []config.App {
	nodesPerApp := v.nodesRunningByApp()
	var skipped []config.App
	for _, a := range v.catalog() {
		if !v.selected[a.Name] {
			continue
		}
		if !a.MatchAll {
			if len(nodesPerApp[a.Name]) > 0 {
				skipped = append(skipped, a)
			}
			continue
		}
		if len(nodesPerApp[a.Name]) >= len(v.cfg.Cluster.Nodes) {
			skipped = append(skipped, a)
		}
	}
	return skipped
}

// renderSecretsPrompt draws one block per target with at least one secret,
// each block listing the textinputs for that target's declared secrets.
func (v *AppsView) renderSecretsPrompt(width int) string {
	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	ink := lipgloss.NewStyle().Foreground(DefaultTheme.Ink).Bold(true)
	gold := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	verm := lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Bold(true)

	lines := []string{
		" " + ink.Render("Enter per-replica secrets") + "  " + muted.Render(
			"(tab/↓ next · shift+tab/↑ prev · ⏎ deploy · esc back)"),
		"",
	}

	// Walk inputs in order, grouping by target so headers print exactly
	// once per target. Each input's index in v.secretInputs matches its
	// binding in v.secretInputFor.
	lastTarget := -1
	for i, b := range v.secretInputFor {
		if b.targetIdx != lastTarget {
			t := v.queue[b.targetIdx]
			lines = append(lines, "  "+gold.Render(t.label()))
			lastTarget = b.targetIdx
		}
		marker := "  "
		if i == v.secretFocus {
			marker = v.styles.SelectionMark.Render("▶ ")
		}
		field := v.secretInputs[i].View()
		label := muted.Render(padRight(b.secretName+":", 24))
		lines = append(lines, "    "+marker+label+"  "+field)
	}
	if len(v.secretInputs) == 0 {
		lines = append(lines, "    "+verm.Render("(no inputs built — bug; press esc)"))
	}
	return strings.Join(lines, "\n")
}

func (v *AppsView) renderRunning(width int) string {
	completed := v.completedCount()
	failed := v.failedCount()
	total := len(v.queue)

	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	gold := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	verd := lipgloss.NewStyle().Foreground(DefaultTheme.Verdigris).Bold(true)
	verm := lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Bold(true)

	pct := 0.0
	if total > 0 {
		active := 0.0
		if v.state == appsRunning && v.queueIdx >= 0 && v.queueIdx < len(v.results) && !v.results[v.queueIdx].done {
			active = v.activePct / 100.0
		}
		pct = (float64(completed) + active) / float64(total) * 100
	}

	var lines []string
	lines = append(lines, " "+muted.Render(fmt.Sprintf(
		"%d / %d deployed · %d failed · %d remaining",
		completed-failed, total, failed, total-completed,
	)))
	lines = append(lines, v.renderBar(pct))
	lines = append(lines, "")
	for i, t := range v.queue {
		res := v.results[i]
		var glyph, status string
		switch {
		case res.done && res.err == nil:
			glyph = v.styles.StatusSealed.Render(Glyph.Sealed)
			status = verd.Render("deployed")
		case res.done && res.err != nil:
			glyph = v.styles.StatusError.Render(Glyph.Error)
			status = verm.Render("failed")
		case i == v.queueIdx && v.state == appsRunning:
			glyph = v.styles.Heartbeat.Render(Glyph.InFlight)
			status = gold.Render(fmt.Sprintf("%d%% %s", int(v.activePct), truncate(v.activeMsg, 70)))
		default:
			glyph = muted.Render("·")
			status = muted.Render("pending")
		}
		row := fmt.Sprintf(" %s  %s  %s",
			glyph,
			gold.Render(padRight(t.label(), 28)),
			status,
		)
		lines = append(lines, row)
		if res.done && res.err != nil {
			lines = append(lines, v.renderWrappedErr(res.err.Error(), width, 4))
		}
	}

	lines = append(lines, "")
	switch v.state {
	case appsDone:
		lines = append(lines, " "+v.styles.StatusSealed.Render(Glyph.Sealed)+" "+verd.Render("batch complete"))
		lines = append(lines, " "+v.styles.Subtle.Render("⏎ deploy more  ·  esc back to list"))
	case appsFailed:
		lines = append(lines, " "+v.styles.StatusError.Render(Glyph.Error)+" "+
			verm.Render(fmt.Sprintf("batch finished with %d failure(s)", failed)))
		lines = append(lines, " "+v.styles.Subtle.Render("⏎ deploy more  ·  esc back to list"))
	}
	return strings.Join(lines, "\n")
}

func (v *AppsView) completedCount() int {
	n := 0
	for _, r := range v.results {
		if r.done {
			n++
		}
	}
	return n
}

func (v *AppsView) failedCount() int {
	n := 0
	for _, r := range v.results {
		if r.done && r.err != nil {
			n++
		}
	}
	return n
}

func (v *AppsView) renderBar(pct float64) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(pct / 100 * float64(deployBarCells))
	fill := DefaultTheme.Verdigris
	switch {
	case v.state == appsFailed:
		fill = DefaultTheme.Vermilion
	case pct >= 100:
		fill = DefaultTheme.Gold
	}
	bar := lipgloss.NewStyle().Foreground(fill).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(DefaultTheme.Muted).Render(strings.Repeat("░", deployBarCells-filled))
	pctStr := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true).Render(fmt.Sprintf("%3d%%", int(pct)))
	return " " + bar + "  " + pctStr
}

func (v *AppsView) renderWrappedErr(text string, width, indent int) string {
	errStyle := lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion)
	pad := strings.Repeat(" ", indent)
	wrapW := width - indent - 2
	if wrapW < 20 {
		wrapW = 20
	}
	wrapped := lipgloss.NewStyle().Width(wrapW).Render(text)
	wlines := strings.Split(wrapped, "\n")
	out := make([]string, 0, len(wlines))
	for _, ln := range wlines {
		out = append(out, pad+errStyle.Render(strings.TrimRight(ln, " ")))
	}
	return strings.Join(out, "\n")
}
