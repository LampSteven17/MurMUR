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

// patchInspectedMsg carries the parallel-fetched per-app runtime snapshots.
type patchInspectedMsg []provision.AppRuntimeInfo

// patchRow is one displayable+selectable row in the picker. For a normal
// single-instance app, rep is nil and the row's data comes from info. For
// a match_all app, one row per replica is emitted and rep points at the
// per-instance summary (OS/IP/etc. can differ between replicas).
type patchRow struct {
	app  config.App
	info provision.AppRuntimeInfo
	rep  *provision.ReplicaInfo // nil for single-instance rows
	key  string                 // unique selection key
}

// patchState — picker → confirm → batch run, same shape as apps/teardown.
type patchState int

const (
	patchPicking patchState = iota
	patchConfirm
	patchRunning
	patchDone
	patchFailed
)

type patchProgressMsg provision.ProgressEvent
type patchItemStartMsg struct{ idx int }
type patchItemDoneMsg struct {
	idx int
	err error
}
type patchAllDoneMsg struct{}

type patchResult struct {
	row  patchRow
	done bool
	err  error
}

// PatchView is the [p]atch tab: run each app's declared `update:` command
// against its running guest. Apps without `update:` are filtered out;
// apps with `update:` but no running guest are flagged and skipped.
type PatchView struct {
	cfg    *config.Config
	client *proxmox.Client
	styles Styles

	cursor   int
	selected map[string]bool
	apps     []config.App                       // filtered to those with Update set
	infos    map[string]provision.AppRuntimeInfo // per-app runtime snapshot
	loading  bool
	loaded   bool
	err      error
	fetched  time.Time

	state     patchState
	queue     []patchRow
	results   []patchResult
	queueIdx  int
	activeMsg string
	activePct float64
	msgs      chan tea.Msg

	keys patchKeyMap
}

type patchKeyMap struct {
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

func NewPatchView(cfg *config.Config, client *proxmox.Client) *PatchView {
	apps := make([]config.App, 0, len(cfg.Apps))
	for _, a := range cfg.Apps {
		if a.Update != "" {
			apps = append(apps, a)
		}
	}
	return &PatchView{
		cfg:      cfg,
		client:   client,
		styles:   NewStyles(DefaultTheme),
		selected: map[string]bool{},
		apps:     apps,
		keys: patchKeyMap{
			Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
			Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
			Toggle:   key.NewBinding(key.WithKeys(" ", "x"), key.WithHelp("space", "toggle")),
			SelAll:   key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "all running")),
			ClearAll: key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "none")),
			Confirm:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "patch")),
			Yes:      key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "patch")),
			No:       key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "no")),
			Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
			NewAgain: key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "patch more")),
		},
	}
}

func (v *PatchView) Init() tea.Cmd {
	v.loading = true
	return v.fetch()
}

func (v *PatchView) Title() string { return "patch" }

func (v *PatchView) CapturesKeys() bool { return false }

func (v *PatchView) Help() []key.Binding {
	switch v.state {
	case patchConfirm:
		return []key.Binding{v.keys.Yes, v.keys.No}
	case patchDone, patchFailed:
		return []key.Binding{v.keys.Back}
	default:
		return []key.Binding{v.keys.Up, v.keys.Down, v.keys.Toggle, v.keys.SelAll, v.keys.ClearAll, v.keys.Confirm}
	}
}

func (v *PatchView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch m := msg.(type) {
	case RefreshMsg:
		if v.state != patchPicking {
			return v, nil
		}
		v.loading = true
		v.err = nil
		return v, v.fetch()
	case patchInspectedMsg:
		v.loading = false
		v.loaded = true
		v.err = nil
		v.fetched = time.Now()
		v.infos = map[string]provision.AppRuntimeInfo{}
		for _, info := range []provision.AppRuntimeInfo(m) {
			v.infos[info.App.Name] = info
		}
		return v, nil
	case patchProgressMsg:
		v.activePct = m.Percent
		v.activeMsg = m.Message
		return v, v.readNext()
	case patchItemStartMsg:
		v.queueIdx = m.idx
		v.activeMsg = ""
		v.activePct = 0
		return v, v.readNext()
	case patchItemDoneMsg:
		if m.idx >= 0 && m.idx < len(v.results) {
			v.results[m.idx].done = true
			v.results[m.idx].err = m.err
		}
		return v, v.readNext()
	case patchAllDoneMsg:
		failed := 0
		for _, r := range v.results {
			if r.err != nil {
				failed++
			}
		}
		if failed > 0 {
			v.state = patchFailed
		} else {
			v.state = patchDone
		}
		return v, nil
	case tea.KeyMsg:
		switch v.state {
		case patchPicking:
			return v.updatePicking(m)
		case patchConfirm:
			return v.updateConfirm(m)
		case patchDone, patchFailed:
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

func (v *PatchView) updatePicking(m tea.KeyMsg) (View, tea.Cmd) {
	rows := v.buildRows()
	switch {
	case key.Matches(m, v.keys.Up):
		if v.cursor > 0 {
			v.cursor--
		}
	case key.Matches(m, v.keys.Down):
		if v.cursor < len(rows)-1 {
			v.cursor++
		}
	case key.Matches(m, v.keys.Toggle):
		if v.cursor >= 0 && v.cursor < len(rows) {
			k := rows[v.cursor].key
			if v.selected[k] {
				delete(v.selected, k)
			} else {
				v.selected[k] = true
			}
		}
	case key.Matches(m, v.keys.SelAll):
		for _, r := range rows {
			if rowRunning(r) {
				v.selected[r.key] = true
			}
		}
	case key.Matches(m, v.keys.ClearAll):
		v.selected = map[string]bool{}
	case key.Matches(m, v.keys.Confirm):
		if len(v.selected) == 0 && len(rows) > 0 && v.cursor < len(rows) {
			v.selected[rows[v.cursor].key] = true
		}
		if len(v.selected) > 0 {
			v.state = patchConfirm
		}
	}
	return v, nil
}

// ── confirm ────────────────────────────────────────────────────────────────

func (v *PatchView) updateConfirm(m tea.KeyMsg) (View, tea.Cmd) {
	switch {
	case key.Matches(m, v.keys.Yes):
		return v, v.startBatch()
	case key.Matches(m, v.keys.No), key.Matches(m, v.keys.Back):
		v.state = patchPicking
	}
	return v, nil
}

// ── batch run ──────────────────────────────────────────────────────────────

// startBatch queues selected rows that point at running guests. Rows whose
// guest isn't running (or doesn't exist) are filtered with a notice in the
// confirm screen so the operator already saw which were skipped.
func (v *PatchView) startBatch() tea.Cmd {
	v.queue = nil
	for _, r := range v.buildRows() {
		if !v.selected[r.key] || !rowRunning(r) {
			continue
		}
		v.queue = append(v.queue, r)
	}
	if len(v.queue) == 0 {
		v.state = patchConfirm
		return nil
	}
	v.results = make([]patchResult, len(v.queue))
	for i := range v.queue {
		v.results[i].row = v.queue[i]
	}
	v.queueIdx = 0
	v.activeMsg = ""
	v.activePct = 0
	v.state = patchRunning
	v.msgs = make(chan tea.Msg, 256)

	orch := provision.New(v.cfg, v.client)
	msgs := v.msgs
	orch.SetProgress(func(ev provision.ProgressEvent) {
		select {
		case msgs <- patchProgressMsg(ev):
		default:
		}
	})

	queue := v.queue
	go func() {
		for i, row := range queue {
			msgs <- patchItemStartMsg{idx: i}
			var err error
			if row.rep != nil {
				err = orch.PatchAppInstance(context.Background(), row.app, row.rep.VMID, row.rep.Node)
			} else {
				err = orch.PatchApp(context.Background(), row.app)
			}
			msgs <- patchItemDoneMsg{idx: i, err: err}
		}
		msgs <- patchAllDoneMsg{}
		close(msgs)
	}()
	return v.readNext()
}

func (v *PatchView) readNext() tea.Cmd {
	msgs := v.msgs
	return func() tea.Msg {
		msg, ok := <-msgs
		if !ok {
			return nil
		}
		return msg
	}
}

func (v *PatchView) resetToList() {
	v.state = patchPicking
	v.queue = nil
	v.results = nil
	v.queueIdx = 0
	v.activeMsg = ""
	v.activePct = 0
	v.selected = map[string]bool{}
	v.loading = true
}

// renderUpdateStatus turns the (available, total, checked, running) state
// of a row into the "updates" column. Examples:
//
//	"↑ 2/3 outdated"          — checked, updates waiting (vermilion)
//	"✓ up-to-date"            — checked, all images current (verdigris)
//	"— (guest down)"          — not running / not deployed
//	"— (probe failed)"        — running but check didn't return UPDATES:n/m
func renderUpdateStatus(available, total int, checked bool, running bool) string {
	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	verd := lipgloss.NewStyle().Foreground(DefaultTheme.Verdigris).Bold(true)
	verm := lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Bold(true)
	if !running {
		return muted.Render("— (guest down)")
	}
	if !checked {
		return muted.Render("— (probe failed)")
	}
	if total == 0 {
		return muted.Render("— (no containers)")
	}
	if available == 0 {
		return verd.Render("✓ up-to-date")
	}
	return verm.Render(fmt.Sprintf("↑ %d/%d outdated", available, total))
}

// buildRows flattens apps into displayable rows. For match_all apps, one
// row per replica is emitted; otherwise one row per app. Stable order:
// catalog order, then replica order within each match_all app.
func (v *PatchView) buildRows() []patchRow {
	out := make([]patchRow, 0, len(v.apps))
	for _, a := range v.apps {
		info := v.infos[a.Name]
		if !a.MatchAll {
			out = append(out, patchRow{app: a, info: info, key: a.Name})
			continue
		}
		if len(info.Replicas) == 0 {
			// match_all app but no replicas live yet — still emit one row
			// so the operator can see the catalog entry exists.
			out = append(out, patchRow{app: a, info: info, key: a.Name + "#"})
			continue
		}
		for i := range info.Replicas {
			rep := &info.Replicas[i]
			out = append(out, patchRow{
				app:  a,
				info: info,
				rep:  rep,
				key:  fmt.Sprintf("%s#%d", a.Name, rep.VMID),
			})
		}
	}
	return out
}

// running reports whether a row's underlying guest is currently running.
// Drives the "selectable" check and the colored status badge.
func rowRunning(r patchRow) bool {
	if r.rep != nil {
		return r.rep.Status == "running"
	}
	return r.info.Status == "running"
}

// fetch fans out InspectApp across all apps in the catalog. Each inspect
// runs ≤4 API calls + ≤1 SSH (docker compose ps). Bounded to 5 in flight.
// Total wall-clock at this scale is ~5-10s.
func (v *PatchView) fetch() tea.Cmd {
	cfg := v.cfg
	client := v.client
	apps := v.apps
	return func() tea.Msg {
		orch := provision.New(cfg, client)
		out := make([]provision.AppRuntimeInfo, len(apps))
		const concurrency = 5
		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup
		for i, a := range apps {
			wg.Add(1)
			go func(i int, a config.App) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				out[i] = orch.InspectApp(ctx, a)
			}(i, a)
		}
		wg.Wait()
		return patchInspectedMsg(out)
	}
}

// ── rendering ──────────────────────────────────────────────────────────────

func (v *PatchView) View(width, height int) string {
	header := v.styles.Title.Render("⚒  PATCH")
	right := v.styles.Subtle.Render(v.subtitle())
	gap := width - lipgloss.Width(header) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	headLine := header + lipgloss.NewStyle().Width(gap).Render("") + right

	var body string
	switch v.state {
	case patchPicking:
		body = v.renderPicking(width)
	case patchConfirm:
		body = v.renderConfirm(width)
	case patchRunning, patchDone, patchFailed:
		body = v.renderRunning(width)
	}
	return padBlock(headLine+"\n\n"+body, width, height)
}

func (v *PatchView) subtitle() string {
	switch v.state {
	case patchPicking:
		runnable := 0
		for _, r := range v.buildRows() {
			if rowRunning(r) {
				runnable++
			}
		}
		if n := len(v.selected); n > 0 {
			return fmt.Sprintf("%d selected · %d patchable", n, runnable)
		}
		return "patch app update commands"
	case patchConfirm:
		return "confirm patch"
	case patchRunning:
		return fmt.Sprintf("patching %d of %d", v.completedCount(), len(v.queue))
	case patchDone:
		return "all patched"
	case patchFailed:
		return fmt.Sprintf("%d failed", v.failedCount())
	}
	return ""
}

func (v *PatchView) renderPicking(width int) string {
	switch {
	case v.err != nil:
		return " " + v.styles.StatusError.Render(Glyph.Error) + " " +
			lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Render(v.err.Error())
	case v.loading && !v.loaded:
		return " " + v.styles.Subtle.Render(Glyph.InFlight+" inspecting each app (OS, IP, containers)…")
	case len(v.apps) == 0:
		return " " + v.styles.Subtle.Render(Glyph.Empty+
			" no apps with `update:` set in cluster.yaml — declare one to patch from here.")
	}

	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	ink := lipgloss.NewStyle().Foreground(DefaultTheme.Ink)
	gold := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	verd := lipgloss.NewStyle().Foreground(DefaultTheme.Verdigris).Bold(true)
	verm := lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Bold(true)

	rows := v.buildRows()
	// Column widths. Tuned for typical terminals (~120-160 cols). Values
	// longer than their column width get truncated via truncate() with an
	// "…" suffix so the next column stays aligned.
	const (
		colName = 18
		colNode = 12
		colOS   = 26
		colGst  = 10
		colIP   = 15
	)
	// 7-col leading indent matches the data row's "▶ [ ]  " prefix
	// (cursor=2 + check=3 + sep=2).
	hdr := fmt.Sprintf("       %s  %s  %s  %s  %s  %s",
		muted.Render(padRight("name", colName)),
		muted.Render(padRight("node", colNode)),
		muted.Render(padRight("os", colOS)),
		muted.Render(padRight("guest", colGst)),
		muted.Render(padRight("ip", colIP)),
		muted.Render("updates"))
	lines := []string{
		v.styles.Title.Render("APPS") + "  " +
			muted.Render(fmt.Sprintf("(%d row · %d catalog)", len(rows), len(v.apps))),
		hdr,
	}
	for i, r := range rows {
		cursor := "  "
		if v.cursor == i {
			cursor = v.styles.SelectionMark.Render("▶ ")
		}
		check := muted.Render("[ ]")
		if v.selected[r.key] {
			check = verm.Render("[x]")
		}

		// Per-row data: from rep for match_all instances, else from info.
		var vmid int
		var node, gType, gStatus, gOS, gIP string
		if r.rep != nil {
			vmid, node, gType, gStatus, gOS, gIP = r.rep.VMID, r.rep.Node, r.rep.Type, r.rep.Status, r.rep.OS, r.rep.IP
		} else {
			vmid, node, gType, gStatus, gOS, gIP = r.info.VMID, r.info.Node, r.info.Type, r.info.Status, r.info.OS, r.info.IP
		}

		osStr := gOS
		if osStr == "" {
			osStr = "—"
		}
		guestStr := "—"
		if vmid > 0 {
			guestStr = fmt.Sprintf("%s/%d", gType, vmid)
		}
		ipStr := gIP
		if ipStr == "" {
			ipStr = "—"
		}
		nodeStr := node
		if nodeStr == "" {
			nodeStr = "—"
		}

		// Update-availability column. For match_all replicas, the per-rep
		// counts are authoritative; for single-instance rows, the top-level
		// info fields are.
		var upAvail, upTotal int
		var upChecked bool
		if r.rep != nil {
			upAvail, upTotal, upChecked = r.rep.UpdatesAvailable, r.rep.UpdatesTotal, r.rep.UpdateChecked
		} else {
			upAvail, upTotal, upChecked = r.info.UpdatesAvailable, r.info.UpdatesTotal, r.info.UpdateChecked
		}
		updatesStr := renderUpdateStatus(upAvail, upTotal, upChecked, gStatus == "running")

		// Color the guest column by status.
		guestStyled := ink.Render(guestStr)
		switch gStatus {
		case "running":
			guestStyled = verd.Render(guestStr)
		case "stopped":
			guestStyled = gold.Render(guestStr)
		case "":
			guestStyled = verm.Render(guestStr)
		}

		row := fmt.Sprintf("%s%s  %s  %s  %s  %s  %s  %s",
			cursor,
			check,
			gold.Render(padRight(truncate(r.app.Name, colName), colName)),
			muted.Render(padRight(truncate(nodeStr, colNode), colNode)),
			ink.Render(padRight(truncate(osStr, colOS), colOS)),
			padRight(guestStyled, colGst),
			muted.Render(padRight(truncate(ipStr, colIP), colIP)),
			updatesStr,
		)
		lines = append(lines, row)
	}
	if !v.fetched.IsZero() {
		hb := v.styles.Heartbeat.Render("◉")
		ts := muted.Render(fmt.Sprintf("fetched %s", v.fetched.Format("15:04:05")))
		lines = append(lines, "")
		lines = append(lines, " "+hb+" "+ts+"  "+muted.Render(
			"· space toggle · A all running · N none · ⏎ patch"))
	}
	return strings.Join(lines, "\n")
}

func (v *PatchView) renderConfirm(width int) string {
	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	ink := lipgloss.NewStyle().Foreground(DefaultTheme.Ink).Bold(true)
	gold := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	verm := lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Bold(true)

	var runnable, skipped []patchRow
	for _, r := range v.buildRows() {
		if !v.selected[r.key] {
			continue
		}
		if rowRunning(r) {
			runnable = append(runnable, r)
		} else {
			skipped = append(skipped, r)
		}
	}

	rowLabel := func(r patchRow) string {
		// Show the node for replicated rows so the operator sees which
		// instance is being patched (twingate-connector on pve1).
		if r.rep != nil {
			return fmt.Sprintf("%s @ %s", r.app.Name, r.rep.Node)
		}
		return r.app.Name
	}

	lines := []string{
		" " + ink.Render(fmt.Sprintf("About to patch %d guest(s):", len(runnable))),
		"",
	}
	for _, r := range runnable {
		lines = append(lines, fmt.Sprintf("    %s  %s",
			gold.Render(padRight(rowLabel(r), 28)),
			muted.Render(truncate(r.app.Update, 80)),
		))
	}
	if len(skipped) > 0 {
		lines = append(lines, "")
		lines = append(lines, " "+gold.Render(fmt.Sprintf("%d will be SKIPPED (not running):", len(skipped))))
		for _, r := range skipped {
			lines = append(lines, "    "+gold.Render(rowLabel(r))+
				muted.Render("  ([a]apps to redeploy if it should be running)"))
		}
	}
	lines = append(lines, "")
	lines = append(lines, "    "+muted.Render(
		"runs each app's update command over SSH against the running guest · sequential"))
	lines = append(lines, "")
	yes := verm.Render("[y]")
	no := gold.Render("[n]")
	if len(runnable) == 0 {
		lines = append(lines, "    "+muted.Render("nothing to patch after skips — esc to go back"))
	} else {
		lines = append(lines, "    "+yes+" "+ink.Render("patch")+"      "+no+" "+ink.Render("cancel"))
	}
	return strings.Join(lines, "\n")
}

func (v *PatchView) renderRunning(width int) string {
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
		if v.state == patchRunning && v.queueIdx >= 0 && v.queueIdx < len(v.results) && !v.results[v.queueIdx].done {
			active = v.activePct / 100.0
		}
		pct = (float64(completed) + active) / float64(total) * 100
	}

	var lines []string
	lines = append(lines, " "+muted.Render(fmt.Sprintf(
		"%d / %d patched · %d failed · %d remaining",
		completed-failed, total, failed, total-completed,
	)))
	lines = append(lines, v.renderBar(pct))
	lines = append(lines, "")
	for i, row := range v.queue {
		res := v.results[i]
		var glyph, status string
		switch {
		case res.done && res.err == nil:
			glyph = v.styles.StatusSealed.Render(Glyph.Sealed)
			status = verd.Render("patched")
		case res.done && res.err != nil:
			glyph = v.styles.StatusError.Render(Glyph.Error)
			status = verm.Render("failed")
		case i == v.queueIdx && v.state == patchRunning:
			glyph = v.styles.Heartbeat.Render(Glyph.InFlight)
			status = gold.Render(fmt.Sprintf("%d%% %s", int(v.activePct), truncate(v.activeMsg, 90)))
		default:
			glyph = muted.Render("·")
			status = muted.Render("pending")
		}
		// Show node alongside name for replicated rows so the operator can
		// tell which twingate-connector instance is which.
		label := row.app.Name
		if row.rep != nil {
			label = fmt.Sprintf("%s @ %s", row.app.Name, row.rep.Node)
		}
		out := fmt.Sprintf(" %s  %s  %s",
			glyph,
			gold.Render(padRight(label, 28)),
			status,
		)
		lines = append(lines, out)
		if res.done && res.err != nil {
			lines = append(lines, v.renderWrappedErr(res.err.Error(), width, 4))
		}
	}

	lines = append(lines, "")
	switch v.state {
	case patchDone:
		lines = append(lines, " "+v.styles.StatusSealed.Render(Glyph.Sealed)+" "+verd.Render("batch complete"))
		lines = append(lines, " "+v.styles.Subtle.Render("⏎ patch more  ·  esc back to list"))
	case patchFailed:
		lines = append(lines, " "+v.styles.StatusError.Render(Glyph.Error)+" "+
			verm.Render(fmt.Sprintf("batch finished with %d failure(s)", failed)))
		lines = append(lines, " "+v.styles.Subtle.Render("⏎ patch more  ·  esc back to list"))
	}
	return strings.Join(lines, "\n")
}

func (v *PatchView) completedCount() int {
	n := 0
	for _, r := range v.results {
		if r.done {
			n++
		}
	}
	return n
}

func (v *PatchView) failedCount() int {
	n := 0
	for _, r := range v.results {
		if r.done && r.err != nil {
			n++
		}
	}
	return n
}

func (v *PatchView) renderBar(pct float64) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(pct / 100 * float64(deployBarCells))
	fill := DefaultTheme.Gold
	switch {
	case v.state == patchFailed:
		fill = DefaultTheme.Vermilion
	case pct >= 100:
		fill = DefaultTheme.Verdigris
	}
	bar := lipgloss.NewStyle().Foreground(fill).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(DefaultTheme.Muted).Render(strings.Repeat("░", deployBarCells-filled))
	pctStr := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true).Render(fmt.Sprintf("%3d%%", int(pct)))
	return " " + bar + "  " + pctStr
}

func (v *PatchView) renderWrappedErr(text string, width, indent int) string {
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
