package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rtx-monster/murmur/internal/config"
	"github.com/rtx-monster/murmur/internal/provision"
	"github.com/rtx-monster/murmur/internal/proxmox"
)

// updateState — picker → confirm → batch run, same as teardown/apps.
type updateState int

const (
	updatePicking updateState = iota
	updateConfirm
	updateRunning
	updateDone
	updateFailed
)

type updateStatusesMsg []provision.HostUpdateStatus
type updateProgressMsg provision.ProgressEvent
type updateItemStartMsg struct{ idx int }
type updateItemDoneMsg struct {
	idx int
	err error
}
type updateAllDoneMsg struct{}

type nodeResult struct {
	node string
	done bool
	err  error
}

// UpdateView lists PVE nodes with their apt-update status and runs
// `apt-get update && apt-get dist-upgrade` over SSH on selected nodes.
// Sequential to preserve cluster quorum during reboots.
type UpdateView struct {
	cfg    *config.Config
	client *proxmox.Client
	active *config.ActiveUser
	styles Styles

	cursor   int
	selected map[string]bool // node name → picked
	statuses []provision.HostUpdateStatus
	loading  bool
	loaded   bool
	err      error
	fetched  time.Time

	state     updateState
	queue     []string
	results   []nodeResult
	queueIdx  int
	activeMsg string
	activePct float64
	msgs      chan tea.Msg

	keys updateKeyMap
}

type updateKeyMap struct {
	Up        key.Binding
	Down      key.Binding
	Toggle    key.Binding
	SelStale  key.Binding
	ClearAll  key.Binding
	RefreshAll key.Binding
	Confirm   key.Binding
	Yes       key.Binding
	No        key.Binding
	Back      key.Binding
	NewAgain  key.Binding
}

func NewUpdateView(cfg *config.Config, client *proxmox.Client, active *config.ActiveUser) *UpdateView {
	return &UpdateView{
		cfg:      cfg,
		client:   client,
		active:   active,
		styles:   NewStyles(DefaultTheme),
		selected: map[string]bool{},
		keys: updateKeyMap{
			Up:         key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
			Down:       key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
			Toggle:     key.NewBinding(key.WithKeys(" ", "x"), key.WithHelp("space", "toggle")),
			SelStale:   key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "all w/ pending")),
			ClearAll:   key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "none")),
			RefreshAll: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
			Confirm:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "upgrade")),
			Yes:        key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "upgrade")),
			No:         key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "no")),
			Back:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
			NewAgain:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "back to list")),
		},
	}
}

func (v *UpdateView) Init() tea.Cmd {
	v.loading = true
	return v.fetch(false)
}

func (v *UpdateView) Title() string { return "update" }

func (v *UpdateView) CapturesKeys() bool { return false }

func (v *UpdateView) Help() []key.Binding {
	switch v.state {
	case updateConfirm:
		return []key.Binding{v.keys.Yes, v.keys.No}
	case updateDone, updateFailed:
		return []key.Binding{v.keys.Back}
	default:
		return []key.Binding{v.keys.Up, v.keys.Down, v.keys.Toggle, v.keys.SelStale, v.keys.ClearAll, v.keys.RefreshAll, v.keys.Confirm}
	}
}

func (v *UpdateView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch m := msg.(type) {
	case RefreshMsg:
		if v.state != updatePicking {
			return v, nil
		}
		v.loading = true
		v.err = nil
		return v, v.fetch(true)
	case updateStatusesMsg:
		v.loading = false
		v.loaded = true
		v.err = nil
		v.fetched = time.Now()
		v.statuses = []provision.HostUpdateStatus(m)
		return v, nil
	case updateProgressMsg:
		v.activePct = m.Percent
		v.activeMsg = m.Message
		return v, v.readNext()
	case updateItemStartMsg:
		v.queueIdx = m.idx
		v.activeMsg = ""
		v.activePct = 0
		return v, v.readNext()
	case updateItemDoneMsg:
		if m.idx >= 0 && m.idx < len(v.results) {
			v.results[m.idx].done = true
			v.results[m.idx].err = m.err
		}
		return v, v.readNext()
	case updateAllDoneMsg:
		failed := 0
		for _, r := range v.results {
			if r.err != nil {
				failed++
			}
		}
		if failed > 0 {
			v.state = updateFailed
		} else {
			v.state = updateDone
		}
		return v, nil
	case tea.KeyMsg:
		switch v.state {
		case updatePicking:
			return v.updatePicking(m)
		case updateConfirm:
			return v.updateConfirm(m)
		case updateDone, updateFailed:
			switch {
			case key.Matches(m, v.keys.NewAgain), key.Matches(m, v.keys.Back):
				v.resetToList()
				return v, v.fetch(false)
			}
		}
	}
	return v, nil
}

// ── picking ────────────────────────────────────────────────────────────────

func (v *UpdateView) updatePicking(m tea.KeyMsg) (View, tea.Cmd) {
	switch {
	case key.Matches(m, v.keys.Up):
		if v.cursor > 0 {
			v.cursor--
		}
	case key.Matches(m, v.keys.Down):
		if v.cursor < len(v.statuses)-1 {
			v.cursor++
		}
	case key.Matches(m, v.keys.Toggle):
		if v.cursor >= 0 && v.cursor < len(v.statuses) {
			name := v.statuses[v.cursor].Node
			if v.selected[name] {
				delete(v.selected, name)
			} else {
				v.selected[name] = true
			}
		}
	case key.Matches(m, v.keys.SelStale):
		for _, s := range v.statuses {
			if s.Pending > 0 || s.NeedsReboot {
				v.selected[s.Node] = true
			}
		}
	case key.Matches(m, v.keys.ClearAll):
		v.selected = map[string]bool{}
	case key.Matches(m, v.keys.RefreshAll):
		v.loading = true
		return v, v.fetch(true)
	case key.Matches(m, v.keys.Confirm):
		if len(v.selected) == 0 && len(v.statuses) > 0 {
			v.selected[v.statuses[v.cursor].Node] = true
		}
		if len(v.selected) > 0 {
			v.state = updateConfirm
		}
	}
	return v, nil
}

// ── confirm ────────────────────────────────────────────────────────────────

func (v *UpdateView) updateConfirm(m tea.KeyMsg) (View, tea.Cmd) {
	switch {
	case key.Matches(m, v.keys.Yes):
		return v, v.startBatch()
	case key.Matches(m, v.keys.No), key.Matches(m, v.keys.Back):
		v.state = updatePicking
	}
	return v, nil
}

// ── batch run ──────────────────────────────────────────────────────────────

func (v *UpdateView) startBatch() tea.Cmd {
	v.queue = nil
	for _, s := range v.statuses {
		if v.selected[s.Node] {
			v.queue = append(v.queue, s.Node)
		}
	}
	if len(v.queue) == 0 {
		v.state = updatePicking
		return nil
	}
	v.results = make([]nodeResult, len(v.queue))
	for i := range v.queue {
		v.results[i].node = v.queue[i]
	}
	v.queueIdx = 0
	v.activeMsg = ""
	v.activePct = 0
	v.state = updateRunning
	v.msgs = make(chan tea.Msg, 256)

	orch := provision.New(v.cfg, v.client)
	orch.SetActiveUser(v.active)
	msgs := v.msgs
	orch.SetProgress(func(ev provision.ProgressEvent) {
		select {
		case msgs <- updateProgressMsg(ev):
		default:
		}
	})

	queue := v.queue
	go func() {
		for i, node := range queue {
			msgs <- updateItemStartMsg{idx: i}
			err := orch.UpgradeHost(context.Background(), node)
			msgs <- updateItemDoneMsg{idx: i, err: err}
		}
		msgs <- updateAllDoneMsg{}
		close(msgs)
	}()
	return v.readNext()
}

func (v *UpdateView) readNext() tea.Cmd {
	msgs := v.msgs
	return func() tea.Msg {
		msg, ok := <-msgs
		if !ok {
			return nil
		}
		return msg
	}
}

func (v *UpdateView) resetToList() {
	v.state = updatePicking
	v.queue = nil
	v.results = nil
	v.queueIdx = 0
	v.activeMsg = ""
	v.activePct = 0
	v.selected = map[string]bool{}
	v.loading = true
}

// fetch lists online nodes from /cluster/resources and fans out
// CheckHostUpdates across them. The refresh param decides whether to also
// run `apt-get update` on each (the operator's `r` keystroke) — costlier
// but gives fresh data.
func (v *UpdateView) fetch(refreshFirst bool) tea.Cmd {
	cfg := v.cfg
	client := v.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		resources, err := client.GetResources(ctx, "")
		if err != nil {
			return updateStatusesMsg(nil)
		}
		var nodes []string
		for _, r := range resources {
			if r.Type == "node" && r.Status == "online" {
				nodes = append(nodes, r.Node)
			}
		}
		// Optional refresh pass — POST /apt/update on each node in parallel.
		// Returns UPIDs; we wait briefly but don't block on long ones.
		if refreshFirst {
			refreshIndexes(ctx, client, nodes)
		}
		orch := provision.New(cfg, client)
		out := make([]provision.HostUpdateStatus, len(nodes))
		const concurrency = 6
		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup
		for i, n := range nodes {
			wg.Add(1)
			go func(i int, n string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				out[i] = orch.CheckHostUpdates(ctx, n)
			}(i, n)
		}
		wg.Wait()
		return updateStatusesMsg(out)
	}
}

// refreshIndexes fan-fires RefreshAptIndex per node and waits up to 60s
// each for completion. apt-get update is usually a few seconds; capping
// keeps the UI honest if a mirror is misbehaving on one node.
func refreshIndexes(ctx context.Context, client *proxmox.Client, nodes []string) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for _, n := range nodes {
		wg.Add(1)
		go func(node string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			upid, err := client.RefreshAptIndex(callCtx, node)
			if err != nil || upid == "" {
				return
			}
			_, _ = client.WaitForTask(callCtx, upid, 2*time.Second)
		}(n)
	}
	wg.Wait()
}

// ── rendering ──────────────────────────────────────────────────────────────

func (v *UpdateView) View(width, height int) string {
	header := v.styles.Title.Render("⚒  UPDATE")
	right := v.styles.Subtle.Render(v.subtitle())
	gap := width - lipgloss.Width(header) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	headLine := header + lipgloss.NewStyle().Width(gap).Render("") + right

	var body string
	switch v.state {
	case updatePicking:
		body = v.renderPicking(width)
	case updateConfirm:
		body = v.renderConfirm(width)
	case updateRunning, updateDone, updateFailed:
		body = v.renderRunning(width)
	}
	return padBlock(headLine+"\n\n"+body, width, height)
}

func (v *UpdateView) subtitle() string {
	switch v.state {
	case updatePicking:
		pending := 0
		for _, s := range v.statuses {
			if s.Pending > 0 || s.NeedsReboot {
				pending++
			}
		}
		if pending > 0 {
			return fmt.Sprintf("%d nodes have pending work", pending)
		}
		return "proxmox host upgrades"
	case updateConfirm:
		return "confirm upgrade"
	case updateRunning:
		return fmt.Sprintf("upgrading %d of %d", v.completedCount(), len(v.queue))
	case updateDone:
		return "all upgraded"
	case updateFailed:
		return fmt.Sprintf("%d failed", v.failedCount())
	}
	return ""
}

func (v *UpdateView) renderPicking(width int) string {
	switch {
	case v.err != nil:
		return " " + v.styles.StatusError.Render(Glyph.Error) + " " +
			lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Render(v.err.Error())
	case v.loading && !v.loaded:
		return " " + v.styles.Subtle.Render(Glyph.InFlight+" checking each node's apt state…")
	case len(v.statuses) == 0:
		return " " + v.styles.Subtle.Render(Glyph.Empty+" no online nodes.")
	}

	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	ink := lipgloss.NewStyle().Foreground(DefaultTheme.Ink)
	gold := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	verd := lipgloss.NewStyle().Foreground(DefaultTheme.Verdigris).Bold(true)
	verm := lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Bold(true)

	lines := []string{v.styles.Title.Render("NODES") + "  " + muted.Render(fmt.Sprintf("(%d)", len(v.statuses)))}
	for i, s := range v.statuses {
		cursor := "  "
		if v.cursor == i {
			cursor = v.styles.SelectionMark.Render("▶ ")
		}
		check := muted.Render("[ ]")
		if v.selected[s.Node] {
			check = verm.Render("[x]")
		}
		var statusStr string
		switch {
		case s.Err != nil:
			statusStr = verm.Render("ERROR")
		case s.Pending == 0 && !s.NeedsReboot:
			statusStr = verd.Render("UP-TO-DATE")
		case s.NeedsReboot && s.Pending == 0:
			statusStr = gold.Render("NEEDS REBOOT")
		default:
			statusStr = gold.Render(fmt.Sprintf("%d PENDING", s.Pending))
			if s.NeedsReboot {
				statusStr += "  " + gold.Render("(+reboot)")
			}
		}
		row := fmt.Sprintf("%s%s  %s  %s",
			cursor,
			check,
			ink.Render(padRight(s.Node, 16)),
			statusStr,
		)
		lines = append(lines, row)
		if s.Err != nil {
			lines = append(lines, "        "+muted.Render(truncate(s.Err.Error(), 90)))
		}
	}
	if !v.fetched.IsZero() {
		hb := v.styles.Heartbeat.Render("◉")
		ts := muted.Render(fmt.Sprintf("fetched %s", v.fetched.Format("15:04:05")))
		lines = append(lines, "")
		lines = append(lines, " "+hb+" "+ts+"  "+muted.Render(
			"· space toggle · A all w/ pending · N none · r refresh · ⏎ upgrade"))
	}
	return strings.Join(lines, "\n")
}

func (v *UpdateView) renderConfirm(width int) string {
	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	ink := lipgloss.NewStyle().Foreground(DefaultTheme.Ink).Bold(true)
	gold := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	verm := lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Bold(true)

	var picked []provision.HostUpdateStatus
	for _, s := range v.statuses {
		if v.selected[s.Node] {
			picked = append(picked, s)
		}
	}

	lines := []string{
		" " + ink.Render(fmt.Sprintf("About to upgrade %d PVE node(s):", len(picked))),
		"",
	}
	totalPending := 0
	rebootCount := 0
	for _, s := range picked {
		totalPending += s.Pending
		rebootSuffix := ""
		if s.NeedsReboot {
			rebootSuffix = "  " + gold.Render("(reboot pending)")
			rebootCount++
		}
		lines = append(lines, fmt.Sprintf("    %s  %s%s",
			gold.Render(padRight(s.Node, 16)),
			ink.Render(fmt.Sprintf("%d packages to upgrade", s.Pending)),
			rebootSuffix,
		))
	}
	lines = append(lines, "")
	lines = append(lines, "    "+muted.Render(fmt.Sprintf(
		"%d packages total · runs `apt-get -y dist-upgrade` over SSH · sequential",
		totalPending)))
	if rebootCount > 0 {
		lines = append(lines, "    "+muted.Render(fmt.Sprintf(
			"%d node(s) will auto-reboot after upgrade if /var/run/reboot-required appears",
			rebootCount)))
	}
	lines = append(lines, "")
	yes := verm.Render("[y]")
	no := gold.Render("[n]")
	lines = append(lines, "    "+yes+" "+ink.Render("upgrade")+"      "+no+" "+ink.Render("cancel"))
	return strings.Join(lines, "\n")
}

func (v *UpdateView) renderRunning(width int) string {
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
		if v.state == updateRunning && v.queueIdx >= 0 && v.queueIdx < len(v.results) && !v.results[v.queueIdx].done {
			active = v.activePct / 100.0
		}
		pct = (float64(completed) + active) / float64(total) * 100
	}

	var lines []string
	lines = append(lines, " "+muted.Render(fmt.Sprintf(
		"%d / %d upgraded · %d failed · %d remaining",
		completed-failed, total, failed, total-completed,
	)))
	lines = append(lines, v.renderBar(pct))
	lines = append(lines, "")
	for i, node := range v.queue {
		res := v.results[i]
		var glyph, status string
		switch {
		case res.done && res.err == nil:
			glyph = v.styles.StatusSealed.Render(Glyph.Sealed)
			status = verd.Render("upgraded")
		case res.done && res.err != nil:
			glyph = v.styles.StatusError.Render(Glyph.Error)
			status = verm.Render("failed")
		case i == v.queueIdx && v.state == updateRunning:
			glyph = v.styles.Heartbeat.Render(Glyph.InFlight)
			status = gold.Render(fmt.Sprintf("%d%% %s", int(v.activePct), truncate(v.activeMsg, 90)))
		default:
			glyph = muted.Render("·")
			status = muted.Render("pending")
		}
		row := fmt.Sprintf(" %s  %s  %s",
			glyph,
			gold.Render(padRight(node, 16)),
			status,
		)
		lines = append(lines, row)
		if res.done && res.err != nil {
			lines = append(lines, v.renderWrappedErr(res.err.Error(), width, 4))
		}
	}

	lines = append(lines, "")
	switch v.state {
	case updateDone:
		lines = append(lines, " "+v.styles.StatusSealed.Render(Glyph.Sealed)+" "+verd.Render("batch complete"))
		lines = append(lines, " "+v.styles.Subtle.Render("⏎ back to list  ·  esc back to list"))
	case updateFailed:
		lines = append(lines, " "+v.styles.StatusError.Render(Glyph.Error)+" "+
			verm.Render(fmt.Sprintf("batch finished with %d failure(s)", failed)))
		lines = append(lines, " "+v.styles.Subtle.Render("⏎ back to list  ·  esc back to list"))
	}
	return strings.Join(lines, "\n")
}

func (v *UpdateView) completedCount() int {
	n := 0
	for _, r := range v.results {
		if r.done {
			n++
		}
	}
	return n
}

func (v *UpdateView) failedCount() int {
	n := 0
	for _, r := range v.results {
		if r.done && r.err != nil {
			n++
		}
	}
	return n
}

func (v *UpdateView) renderBar(pct float64) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(pct / 100 * float64(deployBarCells))
	fill := DefaultTheme.Gold
	switch {
	case v.state == updateFailed:
		fill = DefaultTheme.Vermilion
	case pct >= 100:
		fill = DefaultTheme.Verdigris
	}
	bar := lipgloss.NewStyle().Foreground(fill).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(DefaultTheme.Muted).Render(strings.Repeat("░", deployBarCells-filled))
	pctStr := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true).Render(fmt.Sprintf("%3d%%", int(pct)))
	return " " + bar + "  " + pctStr
}

func (v *UpdateView) renderWrappedErr(text string, width, indent int) string {
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
