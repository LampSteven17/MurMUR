package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rtx-monster/murmur/internal/config"
	"github.com/rtx-monster/murmur/internal/provision"
	"github.com/rtx-monster/murmur/internal/proxmox"
)

// appsState mirrors teardownState — picker, confirm, batch run.
type appsState int

const (
	appsPicking appsState = iota
	appsConfirm
	appsRunning
	appsDone
	appsFailed
)

// Bridges from the orchestrator goroutine into Update.
type appsProgressMsg provision.ProgressEvent
type appsItemStartMsg struct{ idx int }
type appsItemDoneMsg struct {
	idx int
	err error
}
type appsAllDoneMsg struct{}

// appResult records one queued app's outcome.
type appResult struct {
	app     config.App
	done    bool
	err     error
}

// AppsView is the apps catalog picker — declarative deploys from cluster.yaml.
type AppsView struct {
	cfg        *config.Config
	client     *proxmox.Client
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
	queue     []config.App
	results   []appResult
	queueIdx  int
	activeMsg string
	activePct float64
	msgs      chan tea.Msg

	keys appsKeyMap
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
func NewAppsView(cfg *config.Config, client *proxmox.Client, configPath string) *AppsView {
	return &AppsView{
		cfg:       cfg,
		client:    client,
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

func (v *AppsView) CapturesKeys() bool { return false }

func (v *AppsView) Help() []key.Binding {
	switch v.state {
	case appsConfirm:
		return []key.Binding{v.keys.Yes, v.keys.No}
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
		v.rows = m.Resources
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

func (v *AppsView) updatePicking(m tea.KeyMsg) (View, tea.Cmd) {
	apps := v.cfg.Apps
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
		return v, v.startBatch()
	case key.Matches(m, v.keys.No), key.Matches(m, v.keys.Back):
		v.state = appsPicking
	}
	return v, nil
}

// ── batch run ──────────────────────────────────────────────────────────────

// startBatch builds the queue from picked apps, skipping any whose name
// already has a running guest (collision policy: skip silently in the run
// log; the confirm screen highlighted them upstream so the operator already
// saw which ones were left out).
func (v *AppsView) startBatch() tea.Cmd {
	running := v.runningNames()
	v.queue = nil
	for _, a := range v.cfg.Apps {
		if !v.selected[a.Name] {
			continue
		}
		if running[a.Name] {
			continue
		}
		v.queue = append(v.queue, a)
	}
	if len(v.queue) == 0 {
		// Everything we picked collided. Bounce back to confirm so the
		// operator can deselect or teardown.
		v.state = appsConfirm
		return nil
	}
	v.results = make([]appResult, len(v.queue))
	for i := range v.queue {
		v.results[i].app = v.queue[i]
	}
	v.queueIdx = 0
	v.activeMsg = ""
	v.activePct = 0
	v.state = appsRunning
	v.msgs = make(chan tea.Msg, 256)

	orch := provision.New(v.cfg, v.client)
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
		for i, app := range queue {
			msgs <- appsItemStartMsg{idx: i}
			req := provision.Request{
				Name:              app.Name,
				Type:              "vm",
				Image:             app.Image,
				Flavor:            app.Flavor,
				PostDeployCommand: buildPostDeployCommand(app, configDir),
				WorkDir:           configDir,
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
		return fmt.Sprintf(
			"ansible-playbook -i ${GUEST_IP}, -u ${GUEST_USER} "+
				"-e vm_name=${GUEST_NAME} -e vm_ip=${GUEST_IP} "+
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

// runningNames returns the set of guest names currently running on the
// cluster, used for collision detection in the confirm screen + skip logic
// in the batch dispatch.
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
	case len(v.cfg.Apps) == 0:
		return " " + v.styles.Subtle.Render(Glyph.Empty+
			" no apps in cluster.yaml — declare an `apps:` section to use this tab.")
	}

	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	ink := lipgloss.NewStyle().Foreground(DefaultTheme.Ink)
	gold := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	verm := lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Bold(true)
	running := v.runningNames()

	lines := []string{v.styles.Title.Render("APPS") + "  " + muted.Render(fmt.Sprintf("(%d)", len(v.cfg.Apps)))}
	for i, a := range v.cfg.Apps {
		cursor := "  "
		if v.cursor == i {
			cursor = v.styles.SelectionMark.Render("▶ ")
		}
		check := muted.Render("[ ]")
		if v.selected[a.Name] {
			check = verm.Render("[x]")
		}
		recipe := postDeploySummary(a)
		collision := ""
		if running[a.Name] {
			collision = "  " + verm.Render("(collision — already running)")
		}
		row := fmt.Sprintf("%s%s  %s  %s  %s  %s%s",
			cursor,
			check,
			gold.Render(padRight(a.Name, 18)),
			ink.Render(padRight(a.Image, 14)),
			ink.Render(padRight(a.Flavor, 12)),
			muted.Render(truncate(recipe, 50)),
			collision,
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
// after the guest is up: the playbook path, the raw command (truncated), or
// "(provision only)" for apps with no post-deploy.
func postDeploySummary(a config.App) string {
	switch {
	case a.Playbook != "":
		return "playbook: " + a.Playbook
	case a.PostDeploy != "":
		return "shell: " + a.PostDeploy
	}
	return "(provision only)"
}

func (v *AppsView) renderConfirm(width int) string {
	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	ink := lipgloss.NewStyle().Foreground(DefaultTheme.Ink).Bold(true)
	gold := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	verm := lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Bold(true)
	running := v.runningNames()

	var picked []config.App
	var skipped []config.App
	for _, a := range v.cfg.Apps {
		if !v.selected[a.Name] {
			continue
		}
		if running[a.Name] {
			skipped = append(skipped, a)
		} else {
			picked = append(picked, a)
		}
	}

	auth := "key"
	if v.cfg.Cluster.SSH.Password != "" {
		auth = "key+password"
		if v.cfg.Cluster.SSH.Identity == "" {
			auth = "password"
		}
	}

	lines := []string{
		" " + ink.Render(fmt.Sprintf("About to deploy %d app(s):", len(picked))),
		"",
	}
	for _, a := range picked {
		lines = append(lines, fmt.Sprintf("    %s  %s  %s  %s",
			gold.Render(padRight(a.Name, 18)),
			ink.Render(padRight(a.Image, 14)),
			ink.Render(padRight(a.Flavor, 12)),
			muted.Render(postDeploySummary(a)),
		))
	}
	if len(skipped) > 0 {
		lines = append(lines, "")
		lines = append(lines, " "+verm.Render(fmt.Sprintf("%d will be SKIPPED (collision — already running):", len(skipped))))
		for _, a := range skipped {
			lines = append(lines, "    "+verm.Render(a.Name)+
				muted.Render("  (teardown first to redeploy)"))
		}
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  %s %s   %s %s",
		muted.Render("auth:"), ink.Render(auth),
		muted.Render("placement:"), ink.Render("auto (best-fit per app)"),
	))
	lines = append(lines, "")
	yes := verm.Render("[y]")
	no := gold.Render("[n]")
	if len(picked) == 0 {
		lines = append(lines, "    "+muted.Render("nothing to deploy after collisions — esc to go back"))
	} else {
		lines = append(lines, "    "+yes+" "+ink.Render("deploy")+"      "+no+" "+ink.Render("cancel"))
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
	for i, app := range v.queue {
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
			gold.Render(padRight(app.Name, 18)),
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
