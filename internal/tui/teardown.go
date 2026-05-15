package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rtx-monster/murmur/internal/config"
	"github.com/rtx-monster/murmur/internal/provision"
	"github.com/rtx-monster/murmur/internal/proxmox"
)

// teardownState tracks the view's coarse phase.
type teardownState int

const (
	teardownPicking teardownState = iota
	teardownConfirm
	teardownRunning
	teardownDone
	teardownFailed
)

// Bridges from the orchestrator goroutine into the Update loop.
type teardownProgressMsg provision.ProgressEvent
type teardownItemStartMsg struct{ idx int }
type teardownItemDoneMsg struct {
	idx int
	err error
}
type teardownAllDoneMsg struct{}

// itemResult records the outcome of one queue item.
type itemResult struct {
	req     provision.TeardownRequest
	done    bool
	err     error
}

// TeardownView lets the operator select one or more guests (via spacebar
// checkboxes) and destroy them as a batch. Three phases: picking, confirm,
// running. Templates render in their own section with a ⚠ marker so they
// aren't deleted by reflex.
type TeardownView struct {
	cfg    *config.Config
	client *proxmox.Client
	styles Styles

	// List state.
	rows     []proxmox.Resource // flat order: VMs → LXCs → templates
	cursor   int                // index into rows
	selected map[int]bool       // VMID → selected
	loading  bool
	loaded   bool
	err      error
	fetched  time.Time

	// Batch run state.
	state    teardownState
	queue    []provision.TeardownRequest
	results  []itemResult        // parallel with queue; done flag flips on completion
	queueIdx int                 // currently active item
	activeMsg string             // latest progress message for the active item
	activePct float64            // latest percent for the active item
	msgs     chan tea.Msg

	keys teardownKeyMap
}

type teardownKeyMap struct {
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

func NewTeardownView(cfg *config.Config, client *proxmox.Client) *TeardownView {
	return &TeardownView{
		cfg:      cfg,
		client:   client,
		styles:   NewStyles(DefaultTheme),
		selected: map[int]bool{},
		keys: teardownKeyMap{
			Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
			Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
			Toggle:   key.NewBinding(key.WithKeys(" ", "x"), key.WithHelp("space", "toggle")),
			SelAll:   key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "all")),
			ClearAll: key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "none")),
			Confirm:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "destroy")),
			Yes:      key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "destroy")),
			No:       key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "no")),
			Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
			NewAgain: key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "tear down more")),
		},
	}
}

func (v *TeardownView) Init() tea.Cmd {
	v.loading = true
	return v.fetch()
}

func (v *TeardownView) Title() string { return "teardown" }

func (v *TeardownView) CapturesKeys() bool { return false }

func (v *TeardownView) Help() []key.Binding {
	switch v.state {
	case teardownConfirm:
		return []key.Binding{v.keys.Yes, v.keys.No}
	case teardownDone, teardownFailed:
		return []key.Binding{v.keys.Back}
	default:
		return []key.Binding{v.keys.Up, v.keys.Down, v.keys.Toggle, v.keys.SelAll, v.keys.ClearAll, v.keys.Confirm}
	}
}

func (v *TeardownView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch m := msg.(type) {
	case RefreshMsg:
		if v.state != teardownPicking {
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
		v.applyResources(m.Resources)
		return v, nil
	case teardownProgressMsg:
		v.activePct = m.Percent
		v.activeMsg = m.Message
		return v, v.readNext()
	case teardownItemStartMsg:
		v.queueIdx = m.idx
		v.activeMsg = ""
		v.activePct = 0
		return v, v.readNext()
	case teardownItemDoneMsg:
		if m.idx >= 0 && m.idx < len(v.results) {
			v.results[m.idx].done = true
			v.results[m.idx].err = m.err
		}
		return v, v.readNext()
	case teardownAllDoneMsg:
		failed := 0
		for _, r := range v.results {
			if r.err != nil {
				failed++
			}
		}
		if failed > 0 {
			v.state = teardownFailed
		} else {
			v.state = teardownDone
		}
		return v, nil
	case tea.KeyMsg:
		switch v.state {
		case teardownPicking:
			return v.updatePicking(m)
		case teardownConfirm:
			return v.updateConfirm(m)
		case teardownDone, teardownFailed:
			switch {
			case key.Matches(m, v.keys.NewAgain), key.Matches(m, v.keys.Back):
				// Both go back to the picker. NewAgain (enter) and Back (esc)
				// have the same effect here — there's no "preserve selection"
				// case for teardown since the things selected were destroyed.
				v.resetToList()
				return v, v.fetch()
			}
		}
	}
	return v, nil
}

// ── picking phase ──────────────────────────────────────────────────────────

func (v *TeardownView) updatePicking(m tea.KeyMsg) (View, tea.Cmd) {
	switch {
	case key.Matches(m, v.keys.Up):
		if v.cursor > 0 {
			v.cursor--
		}
	case key.Matches(m, v.keys.Down):
		if v.cursor < len(v.rows)-1 {
			v.cursor++
		}
	case key.Matches(m, v.keys.Toggle):
		if v.cursor >= 0 && v.cursor < len(v.rows) {
			vmid := v.rows[v.cursor].VMID
			if v.selected[vmid] {
				delete(v.selected, vmid)
			} else {
				v.selected[vmid] = true
			}
		}
	case key.Matches(m, v.keys.SelAll):
		for _, r := range v.rows {
			v.selected[r.VMID] = true
		}
	case key.Matches(m, v.keys.ClearAll):
		v.selected = map[int]bool{}
	case key.Matches(m, v.keys.Confirm):
		// If nothing selected, fall back to the cursor row.
		if len(v.selected) == 0 && len(v.rows) > 0 {
			v.selected[v.rows[v.cursor].VMID] = true
		}
		if len(v.selected) > 0 {
			v.state = teardownConfirm
		}
	}
	return v, nil
}

// ── confirm phase ──────────────────────────────────────────────────────────

func (v *TeardownView) updateConfirm(m tea.KeyMsg) (View, tea.Cmd) {
	switch {
	case key.Matches(m, v.keys.Yes):
		return v, v.startBatch()
	case key.Matches(m, v.keys.No), key.Matches(m, v.keys.Back):
		v.state = teardownPicking
	}
	return v, nil
}

// ── batch run ──────────────────────────────────────────────────────────────

func (v *TeardownView) startBatch() tea.Cmd {
	// Build the queue in display order so user can predict the sequence.
	v.queue = nil
	for _, r := range v.rows {
		if !v.selected[r.VMID] {
			continue
		}
		v.queue = append(v.queue, provision.TeardownRequest{
			Type: pveTypeToShort(r.Type),
			Node: r.Node,
			VMID: r.VMID,
			Name: r.Name,
		})
	}
	if len(v.queue) == 0 {
		v.state = teardownPicking
		return nil
	}
	v.results = make([]itemResult, len(v.queue))
	for i := range v.queue {
		v.results[i].req = v.queue[i]
	}
	v.queueIdx = 0
	v.activeMsg = ""
	v.activePct = 0
	v.state = teardownRunning
	v.msgs = make(chan tea.Msg, 256)

	orch := provision.New(v.cfg, v.client)
	msgs := v.msgs
	orch.SetProgress(func(ev provision.ProgressEvent) {
		select {
		case msgs <- teardownProgressMsg(ev):
		default:
		}
	})

	queue := v.queue
	go func() {
		for i, req := range queue {
			msgs <- teardownItemStartMsg{idx: i}
			err := orch.Teardown(context.Background(), req)
			msgs <- teardownItemDoneMsg{idx: i, err: err}
		}
		msgs <- teardownAllDoneMsg{}
		close(msgs)
	}()
	return v.readNext()
}

func (v *TeardownView) readNext() tea.Cmd {
	msgs := v.msgs
	return func() tea.Msg {
		msg, ok := <-msgs
		if !ok {
			return nil
		}
		return msg
	}
}

func (v *TeardownView) resetToList() {
	v.state = teardownPicking
	v.queue = nil
	v.results = nil
	v.queueIdx = 0
	v.activeMsg = ""
	v.activePct = 0
	v.selected = map[int]bool{}
	v.loading = true
}

// pveTypeToShort maps proxmox resource Type strings to murmur's "vm"/"lxc".
func pveTypeToShort(t string) string {
	if t == "qemu" {
		return "vm"
	}
	return "lxc"
}

// applyResources flattens the cluster's qemu+lxc rows into a sorted list:
// VMs first, then LXCs, then templates last. Within each group, ordered by
// VMID. The cursor is preserved by VMID when possible.
func (v *TeardownView) applyResources(all []proxmox.Resource) {
	prevVMID := -1
	if v.cursor >= 0 && v.cursor < len(v.rows) {
		prevVMID = v.rows[v.cursor].VMID
	}

	var filtered []proxmox.Resource
	for _, r := range all {
		if r.Type == "qemu" || r.Type == "lxc" {
			filtered = append(filtered, r)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		ai := teardownBucket(filtered[i])
		bj := teardownBucket(filtered[j])
		if ai != bj {
			return ai < bj
		}
		return filtered[i].VMID < filtered[j].VMID
	})
	v.rows = filtered

	// Drop stale selections.
	present := map[int]bool{}
	for _, r := range filtered {
		present[r.VMID] = true
	}
	for vmid := range v.selected {
		if !present[vmid] {
			delete(v.selected, vmid)
		}
	}

	// Restore cursor by VMID.
	v.cursor = 0
	for i, r := range filtered {
		if r.VMID == prevVMID {
			v.cursor = i
			break
		}
	}
}

// teardownBucket: VMs=0, LXCs=1, Templates=2. Used for grouping in the sort.
func teardownBucket(r proxmox.Resource) int {
	if r.Template == 1 {
		return 2
	}
	if r.Type == "qemu" {
		return 0
	}
	return 1
}

// fetch returns a tea.Cmd that pulls /cluster/resources and emits ClusterDataMsg.
func (v *TeardownView) fetch() tea.Cmd {
	client := v.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		version, err := client.GetVersion(ctx)
		if err != nil {
			return ClusterDataMsg{Err: err}
		}
		resources, err := client.GetResources(ctx, "")
		if err != nil {
			return ClusterDataMsg{Err: err}
		}
		return ClusterDataMsg{Version: version, Resources: resources}
	}
}

// ── rendering ────────────────────────────────────────────────────────────────

func (v *TeardownView) View(width, height int) string {
	header := v.styles.Title.Render("☠  TEARDOWN")
	right := v.styles.Subtle.Render(v.subtitle())
	gap := width - lipgloss.Width(header) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	headLine := header + lipgloss.NewStyle().Width(gap).Render("") + right

	var body string
	switch v.state {
	case teardownPicking:
		body = v.renderPicking(width)
	case teardownConfirm:
		body = v.renderConfirm(width)
	case teardownRunning, teardownDone, teardownFailed:
		body = v.renderRunning(width)
	}
	return padBlock(headLine+"\n\n"+body, width, height)
}

func (v *TeardownView) subtitle() string {
	switch v.state {
	case teardownPicking:
		if n := len(v.selected); n > 0 {
			return fmt.Sprintf("%d selected · space toggle · ⏎ destroy", n)
		}
		return "pick guests with space · ⏎ destroy"
	case teardownConfirm:
		return "confirm destruction"
	case teardownRunning:
		return fmt.Sprintf("destroying %d of %d", v.completedCount(), len(v.queue))
	case teardownDone:
		return "all destroyed"
	case teardownFailed:
		return fmt.Sprintf("%d failed", v.failedCount())
	}
	return ""
}

func (v *TeardownView) renderPicking(width int) string {
	switch {
	case v.err != nil:
		return " " + v.styles.StatusError.Render(Glyph.Error) + " " +
			lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Render(v.err.Error())
	case v.loading && !v.loaded:
		return " " + v.styles.Subtle.Render(Glyph.InFlight+" consulting the conclave…")
	case v.loaded && len(v.rows) == 0:
		return " " + v.styles.Subtle.Render(Glyph.Empty+" no guests present in the cluster.")
	}

	// Group row indices by bucket.
	var vmIdx, lxcIdx, tplIdx []int
	for i, r := range v.rows {
		switch teardownBucket(r) {
		case 0:
			vmIdx = append(vmIdx, i)
		case 1:
			lxcIdx = append(lxcIdx, i)
		case 2:
			tplIdx = append(tplIdx, i)
		}
	}

	var parts []string
	if len(vmIdx) > 0 {
		parts = append(parts, v.renderSection("VMs", vmIdx, false))
	}
	if len(lxcIdx) > 0 {
		parts = append(parts, v.renderSection("LXCs", lxcIdx, false))
	}
	if len(tplIdx) > 0 {
		parts = append(parts, v.renderSection("Templates", tplIdx, true))
	}

	out := strings.Join(parts, "\n\n")
	if !v.fetched.IsZero() {
		ts := v.styles.Subtle.Render(fmt.Sprintf("fetched %s", v.fetched.Format("15:04:05")))
		hb := v.styles.Heartbeat.Render("◉")
		out += "\n\n " + hb + " " + ts + "  " + v.styles.Subtle.Render(
			"· space toggle · A all · N none · ⏎ destroy")
	}
	return out
}

// renderSection renders a labeled group of guest rows.
func (v *TeardownView) renderSection(label string, idxs []int, isTemplate bool) string {
	sel := 0
	for _, i := range idxs {
		if v.selected[v.rows[i].VMID] {
			sel++
		}
	}
	count := fmt.Sprintf("(%d)", len(idxs))
	if sel > 0 {
		count = fmt.Sprintf("(%d of %d selected)", sel, len(idxs))
	}
	header := v.styles.Title.Render(strings.ToUpper(label)) + "  " +
		v.styles.Subtle.Render(count)

	lines := []string{header}
	for _, i := range idxs {
		lines = append(lines, v.renderRow(i, isTemplate))
	}
	return strings.Join(lines, "\n")
}

// renderRow renders one guest row with cursor marker, checkbox, and columns.
func (v *TeardownView) renderRow(i int, isTemplate bool) string {
	r := v.rows[i]
	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	ink := lipgloss.NewStyle().Foreground(DefaultTheme.Ink)
	gold := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	verd := lipgloss.NewStyle().Foreground(DefaultTheme.Verdigris)

	cursor := "  "
	if v.cursor == i {
		cursor = v.styles.SelectionMark.Render("▶ ")
	}
	check := muted.Render("[ ]")
	if v.selected[r.VMID] {
		check = lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Bold(true).Render("[x]")
	}

	statusStyle := muted
	if r.Status == "running" {
		statusStyle = verd
	}

	row := fmt.Sprintf("%s%s  %s %s  %s  %s  %s",
		cursor,
		check,
		gold.Render(padRight(pveTypeToShort(r.Type), 3)),
		gold.Render(padRight(fmt.Sprintf("%d", r.VMID), 5)),
		ink.Render(padRight(truncate(r.Name, 24), 24)),
		muted.Render(padRight(truncate(r.Node, 14), 14)),
		statusStyle.Render(padRight(r.Status, 8)),
	)
	// Warning glyph means "unusual state worth flagging". Templates are
	// always stopped — that's the normal/expected state, no flag. A regular
	// VM/LXC that's stopped is noteworthy.
	if !isTemplate && r.Status == "stopped" {
		row += "  " + lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Render("⚠")
	}
	return row
}

// ── confirm phase ──────────────────────────────────────────────────────────

func (v *TeardownView) renderConfirm(width int) string {
	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	ink := lipgloss.NewStyle().Foreground(DefaultTheme.Ink).Bold(true)
	vermilion := lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Bold(true)

	var picked []proxmox.Resource
	for _, r := range v.rows {
		if v.selected[r.VMID] {
			picked = append(picked, r)
		}
	}

	hdr := vermilion.Render(Glyph.Error + " about to PERMANENTLY destroy " + fmt.Sprintf("%d guest", len(picked)))
	if len(picked) != 1 {
		hdr += vermilion.Render("s")
	}
	hdr += vermilion.Render(":")

	lines := []string{" " + hdr, ""}
	for _, r := range picked {
		tmpl := ""
		if r.Template == 1 {
			tmpl = "  " + lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Render("⚠ template")
		}
		lines = append(lines, fmt.Sprintf("    %s  %s  %s  %s  %s%s",
			ink.Render(padRight(pveTypeToShort(r.Type), 3)),
			ink.Render(padRight(fmt.Sprintf("%d", r.VMID), 5)),
			ink.Render(padRight(truncate(r.Name, 24), 24)),
			muted.Render(padRight(truncate(r.Node, 14), 14)),
			muted.Render(padRight(r.Status, 8)),
			tmpl,
		))
	}
	lines = append(lines, "")
	lines = append(lines, "    "+muted.Render("disks removed · backup/replication entries purged · running guests hard-stopped"))
	lines = append(lines, "")
	yes := lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Bold(true).Render("[y]")
	no := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true).Render("[n]")
	lines = append(lines, "    "+yes+" "+ink.Render("destroy all")+"      "+no+" "+ink.Render("cancel"))
	return strings.Join(lines, "\n")
}

// ── running phase ──────────────────────────────────────────────────────────

func (v *TeardownView) renderRunning(width int) string {
	completed := v.completedCount()
	failed := v.failedCount()
	total := len(v.queue)

	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	ink := lipgloss.NewStyle().Foreground(DefaultTheme.Ink)
	gold := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	verd := lipgloss.NewStyle().Foreground(DefaultTheme.Verdigris).Bold(true)
	vermilion := lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Bold(true)

	// Smooth bar: each completed item is one full unit; the active item
	// contributes a fractional unit equal to its current percent. Without
	// this, the bar sits at 0% through the first 70% of work because nothing
	// has *completed* yet.
	pct := 0.0
	if total > 0 {
		active := 0.0
		if v.state == teardownRunning && v.queueIdx >= 0 && v.queueIdx < len(v.results) && !v.results[v.queueIdx].done {
			active = v.activePct / 100.0
		}
		pct = (float64(completed) + active) / float64(total) * 100
	}

	var lines []string
	lines = append(lines, " "+muted.Render(fmt.Sprintf(
		"%d / %d destroyed · %d failed · %d remaining",
		completed-failed, total, failed, total-completed,
	)))
	lines = append(lines, v.renderBar(pct))
	lines = append(lines, "")
	for i, req := range v.queue {
		res := v.results[i]
		var glyph, status string
		switch {
		case res.done && res.err == nil:
			glyph = v.styles.StatusSealed.Render(Glyph.Sealed)
			status = verd.Render("destroyed")
		case res.done && res.err != nil:
			glyph = v.styles.StatusError.Render(Glyph.Error)
			status = vermilion.Render("failed")
		case i == v.queueIdx && v.state == teardownRunning:
			glyph = v.styles.Heartbeat.Render(Glyph.InFlight)
			status = gold.Render(fmt.Sprintf("%d%% %s", int(v.activePct), v.activeMsg))
		default:
			glyph = muted.Render("·")
			status = muted.Render("pending")
		}
		row := fmt.Sprintf(" %s  %s %s  %s  %s",
			glyph,
			gold.Render(padRight(req.Type, 3)),
			gold.Render(padRight(fmt.Sprintf("%d", req.VMID), 5)),
			ink.Render(padRight(truncate(req.Name, 24), 24)),
			status,
		)
		lines = append(lines, row)
		// Render the per-item error block under the row.
		if res.done && res.err != nil {
			lines = append(lines, v.renderWrappedErr(res.err.Error(), width, 4))
		}
	}

	lines = append(lines, "")
	switch v.state {
	case teardownDone:
		lines = append(lines, " "+v.styles.StatusSealed.Render(Glyph.Sealed)+" "+verd.Render("batch complete"))
		lines = append(lines, " "+v.styles.Subtle.Render("⏎ tear down more  ·  esc back to list"))
	case teardownFailed:
		lines = append(lines, " "+v.styles.StatusError.Render(Glyph.Error)+" "+
			vermilion.Render(fmt.Sprintf("batch finished with %d failure(s)", failed)))
		lines = append(lines, " "+v.styles.Subtle.Render("⏎ tear down more  ·  esc back to list"))
	}
	return strings.Join(lines, "\n")
}

func (v *TeardownView) completedCount() int {
	n := 0
	for _, r := range v.results {
		if r.done {
			n++
		}
	}
	return n
}

func (v *TeardownView) failedCount() int {
	n := 0
	for _, r := range v.results {
		if r.done && r.err != nil {
			n++
		}
	}
	return n
}

func (v *TeardownView) renderBar(pct float64) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(pct / 100 * float64(deployBarCells))
	fill := DefaultTheme.Vermilion
	if v.state == teardownDone {
		fill = DefaultTheme.Verdigris
	}
	bar := lipgloss.NewStyle().Foreground(fill).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(DefaultTheme.Muted).Render(strings.Repeat("░", deployBarCells-filled))
	pctStr := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true).Render(fmt.Sprintf("%3d%%", int(pct)))
	return " " + bar + "  " + pctStr
}

// renderWrappedErr wraps an error string, indenting continuation by `indent`
// columns. Returns a multi-line block.
func (v *TeardownView) renderWrappedErr(text string, width, indent int) string {
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
