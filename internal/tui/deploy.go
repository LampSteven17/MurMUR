package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rtx-monster/murmur/internal/config"
	"github.com/rtx-monster/murmur/internal/provision"
	"github.com/rtx-monster/murmur/internal/proxmox"
)

// deployState is the view's coarse phase: form → running → done/failed.
type deployState int

const (
	deployForm deployState = iota
	deployRunning
	deployDone
	deployFailed
)

// deployProgressMsg / deployDoneMsg bridge the orchestrator's callback and
// goroutine into bubbletea's Update loop.
type deployProgressMsg provision.ProgressEvent
type deployDoneMsg struct {
	result *provision.Result
	err    error
}

// DeployView is the bubbles-form for provisioning a guest. Submit kicks off
// provision.Orchestrator.Deploy in a goroutine; progress events stream back
// via a channel and render as a log pane.
type DeployView struct {
	cfg    *config.Config
	client *proxmox.Client
	styles Styles

	// Form fields.
	name       textinput.Model
	typeOpts   []string
	typeIdx    int
	imageOpts  []string
	imageIdx   int
	flavorOpts []string
	flavorIdx  int
	nodeOpts   []string // [0] = "(auto: best-fit)"
	nodeIdx    int

	// cursor indexes the focusable rows: 0=type 1=name 2=image 3=flavor 4=node 5=submit.
	cursor int

	state    deployState
	progress []provision.ProgressEvent
	msgs     chan tea.Msg
	result   *provision.Result
	err      error

	keys deployKeyMap
}

type deployKeyMap struct {
	Next     key.Binding
	Prev     key.Binding
	Left     key.Binding
	Right    key.Binding
	Submit   key.Binding
	Reset    key.Binding
	NewAgain key.Binding
}

const cursorSubmit = 5

// NewDeployView wires the form from cluster.yaml flavor/image catalogs + nodes.
func NewDeployView(cfg *config.Config, client *proxmox.Client) *DeployView {
	name := textinput.New()
	name.Placeholder = "guest-name"
	name.CharLimit = 63
	name.Width = 40

	imageOpts := make([]string, 0, len(cfg.Images))
	for _, i := range cfg.Images {
		imageOpts = append(imageOpts, i.Name)
	}
	flavorOpts := make([]string, 0, len(cfg.Flavors))
	for _, f := range cfg.Flavors {
		flavorOpts = append(flavorOpts, f.Name)
	}
	nodeOpts := []string{"(auto: best-fit)"}
	for _, n := range cfg.Cluster.Nodes {
		nodeOpts = append(nodeOpts, n.Name)
	}

	v := &DeployView{
		cfg:        cfg,
		client:     client,
		styles:     NewStyles(DefaultTheme),
		name:       name,
		typeOpts:   []string{"vm", "lxc"},
		imageOpts:  imageOpts,
		flavorOpts: flavorOpts,
		nodeOpts:   nodeOpts,
		keys: deployKeyMap{
			Next:     key.NewBinding(key.WithKeys("tab", "down"), key.WithHelp("tab", "next")),
			Prev:     key.NewBinding(key.WithKeys("shift+tab", "up"), key.WithHelp("shift+tab", "prev")),
			Left:     key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "prev value")),
			Right:    key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "next value")),
			Submit:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "submit")),
			Reset:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
			NewAgain: key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "deploy another")),
		},
	}
	// Focus the first field's text input lazily — cursor starts at type (choice).
	return v
}

func (v *DeployView) Init() tea.Cmd { return nil }

func (v *DeployView) Title() string { return "deploy" }

// CapturesKeys is true while the name textinput is focused — keeps app-level
// shortcuts (d/t/u/1-5/r/?/q) from swallowing typed characters. Other form
// rows are choice-cycle only, so they don't need raw-key capture.
func (v *DeployView) CapturesKeys() bool {
	return v.state == deployForm && v.cursor == 1
}

func (v *DeployView) Help() []key.Binding {
	return []key.Binding{v.keys.Next, v.keys.Prev, v.keys.Left, v.keys.Right, v.keys.Submit}
}

func (v *DeployView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch m := msg.(type) {
	case deployProgressMsg:
		v.progress = append(v.progress, provision.ProgressEvent(m))
		return v, v.readNext()
	case deployDoneMsg:
		v.result = m.result
		v.err = m.err
		if m.err != nil {
			v.state = deployFailed
		} else {
			v.state = deployDone
		}
		return v, nil
	case tea.KeyMsg:
		switch v.state {
		case deployForm:
			return v.updateForm(m)
		case deployDone, deployFailed:
			switch {
			case key.Matches(m, v.keys.NewAgain):
				v.resetToFreshForm()
			case key.Matches(m, v.keys.Reset):
				// esc preserves the prior field values — handy for tweaking
				// after a failure.
				v.state = deployForm
				v.progress = nil
				v.result = nil
				v.err = nil
			}
		}
	}
	return v, nil
}

// resetToFreshForm clears the form back to defaults (empty name, first option
// for each cycler, auto-node) and returns to picking — for the "deploy
// another" workflow where the operator just finished one and wants a clean
// slate immediately.
func (v *DeployView) resetToFreshForm() {
	v.state = deployForm
	v.progress = nil
	v.result = nil
	v.err = nil
	v.typeIdx = 0
	v.imageIdx = 0
	v.flavorIdx = 0
	v.nodeIdx = 0
	v.cursor = 0
	v.name.Reset()
	v.name.Blur()
}

func (v *DeployView) updateForm(m tea.KeyMsg) (View, tea.Cmd) {
	onText := v.cursor == 1
	switch {
	case key.Matches(m, v.keys.Next):
		v.moveCursor(1)
	case key.Matches(m, v.keys.Prev):
		v.moveCursor(-1)
	case key.Matches(m, v.keys.Submit):
		if v.cursor == cursorSubmit {
			return v, v.startDeploy()
		}
		v.moveCursor(1)
	case !onText && key.Matches(m, v.keys.Left):
		v.cycleField(-1)
	case !onText && key.Matches(m, v.keys.Right):
		v.cycleField(1)
	default:
		if onText {
			var cmd tea.Cmd
			v.name, cmd = v.name.Update(m)
			return v, cmd
		}
	}
	return v, nil
}

func (v *DeployView) moveCursor(d int) {
	v.cursor += d
	if v.cursor < 0 {
		v.cursor = 0
	}
	if v.cursor > cursorSubmit {
		v.cursor = cursorSubmit
	}
	if v.cursor == 1 {
		v.name.Focus()
	} else {
		v.name.Blur()
	}
}

func (v *DeployView) cycleField(d int) {
	switch v.cursor {
	case 0:
		v.typeIdx = wrap(v.typeIdx+d, len(v.typeOpts))
	case 2:
		v.imageIdx = wrap(v.imageIdx+d, len(v.imageOpts))
	case 3:
		v.flavorIdx = wrap(v.flavorIdx+d, len(v.flavorOpts))
	case 4:
		v.nodeIdx = wrap(v.nodeIdx+d, len(v.nodeOpts))
	}
}

func wrap(a, n int) int {
	if n == 0 {
		return 0
	}
	return ((a % n) + n) % n
}

// startDeploy validates, spawns the orchestrator goroutine, and returns the
// first readNext cmd. Validation errors short-circuit straight to failed.
func (v *DeployView) startDeploy() tea.Cmd {
	name := strings.TrimSpace(v.name.Value())
	if name == "" {
		v.err = fmt.Errorf("name is required")
		v.state = deployFailed
		return nil
	}
	if len(v.imageOpts) == 0 {
		v.err = fmt.Errorf("no images in cluster.yaml catalog")
		v.state = deployFailed
		return nil
	}
	if len(v.flavorOpts) == 0 {
		v.err = fmt.Errorf("no flavors in cluster.yaml catalog")
		v.state = deployFailed
		return nil
	}
	req := provision.Request{
		Name:   name,
		Type:   v.typeOpts[v.typeIdx],
		Image:  v.imageOpts[v.imageIdx],
		Flavor: v.flavorOpts[v.flavorIdx],
	}
	if v.nodeIdx > 0 && v.nodeIdx-1 < len(v.cfg.Cluster.Nodes) {
		req.TargetNode = v.cfg.Cluster.Nodes[v.nodeIdx-1].Name
	}

	v.state = deployRunning
	v.progress = nil
	v.result = nil
	v.err = nil
	v.msgs = make(chan tea.Msg, 64)

	orch := provision.New(v.cfg, v.client)
	msgs := v.msgs
	orch.SetProgress(func(ev provision.ProgressEvent) {
		select {
		case msgs <- deployProgressMsg(ev):
		default:
			// channel full; drop rather than block the orchestrator.
		}
	})
	go func() {
		result, err := orch.Deploy(context.Background(), req)
		msgs <- deployDoneMsg{result: result, err: err}
	}()
	return v.readNext()
}

// readNext blocks on the next message from the orchestrator side. Each
// progress event re-arms via readNext; doneMsg ends the chain.
func (v *DeployView) readNext() tea.Cmd {
	msgs := v.msgs
	return func() tea.Msg { return <-msgs }
}

// ── rendering ────────────────────────────────────────────────────────────────

func (v *DeployView) View(width, height int) string {
	var b strings.Builder

	header := v.styles.Title.Render("⚗  DEPLOY")
	right := v.styles.Subtle.Render(v.headerSubtitle())
	gap := width - lipgloss.Width(header) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	b.WriteString(header + lipgloss.NewStyle().Width(gap).Render("") + right)
	b.WriteString("\n\n")

	switch v.state {
	case deployForm:
		b.WriteString(v.renderForm())
	case deployRunning, deployDone, deployFailed:
		b.WriteString(v.renderProgress(width))
	}

	return padBlock(b.String(), width, height)
}

func (v *DeployView) renderForm() string {
	rows := []struct {
		idx   int
		label string
		value string
	}{
		{0, "type", v.typeOpts[v.typeIdx]},
		{1, "name", v.name.View()},
		{2, "image", optAt(v.imageOpts, v.imageIdx, "(none)")},
		{3, "flavor", optAt(v.flavorOpts, v.flavorIdx, "(none)")},
		{4, "node", optAt(v.nodeOpts, v.nodeIdx, "(none)")},
	}
	var lines []string
	for _, r := range rows {
		lines = append(lines, v.renderFormRow(r.idx, r.label, r.value, r.idx != 1))
	}
	submitFocused := v.cursor == cursorSubmit
	lines = append(lines, "")
	lines = append(lines, v.renderSubmit(submitFocused))
	if v.cursor == 0 || v.cursor == 2 || v.cursor == 3 || v.cursor == 4 {
		lines = append(lines, "")
		lines = append(lines, " "+v.styles.Subtle.Render("← / →  cycle  ·  tab next  ·  ⏎ advance"))
	} else if v.cursor == 1 {
		lines = append(lines, "")
		lines = append(lines, " "+v.styles.Subtle.Render("type the guest name  ·  tab next  ·  ⏎ advance"))
	} else {
		lines = append(lines, "")
		lines = append(lines, " "+v.styles.Subtle.Render("⏎ to deploy"))
	}
	return strings.Join(lines, "\n")
}

// renderFormRow renders one labeled row with a focus mark and value style.
// brackets=true wraps the value in `‹ ... ›` to suggest a cycler.
func (v *DeployView) renderFormRow(idx int, label, value string, brackets bool) string {
	focused := v.cursor == idx
	mark := "  "
	labelStyle := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	valueStyle := lipgloss.NewStyle().Foreground(DefaultTheme.Ink).Bold(true)
	if focused {
		mark = v.styles.SelectionMark.Render("▶ ")
		labelStyle = lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Bold(true)
		valueStyle = lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	}
	val := value
	if brackets {
		left := "‹ "
		right := " ›"
		val = lipgloss.NewStyle().Foreground(DefaultTheme.Muted).Render(left) +
			valueStyle.Render(value) +
			lipgloss.NewStyle().Foreground(DefaultTheme.Muted).Render(right)
	} else {
		val = valueStyle.Render(value)
	}
	return fmt.Sprintf(" %s%s  %s",
		mark,
		padRight(labelStyle.Render(label), 12),
		val,
	)
}

func (v *DeployView) renderSubmit(focused bool) string {
	style := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	mark := "  "
	label := " deploy "
	if focused {
		mark = v.styles.SelectionMark.Render("▶ ")
		style = lipgloss.NewStyle().Foreground(DefaultTheme.Parchment).Background(DefaultTheme.Gold).Bold(true)
	}
	return " " + mark + style.Render("[ "+label+"]")
}

func (v *DeployView) renderProgress(width int) string {
	var lines []string
	lines = append(lines, v.styles.Subtle.Render(" deploying…"))
	lines = append(lines, v.renderProgressBar(v.currentPct()))
	lines = append(lines, "")
	for _, ev := range v.progress {
		lines = append(lines, v.renderProgressLine(ev, width))
	}
	switch v.state {
	case deployRunning:
		lines = append(lines, "")
		lines = append(lines, " "+v.styles.Heartbeat.Render(Glyph.InFlight)+" "+v.styles.Subtle.Render("running…"))
	case deployDone:
		r := v.result
		lines = append(lines, "")
		lines = append(lines, " "+v.styles.StatusSealed.Render(Glyph.Sealed)+" "+
			lipgloss.NewStyle().Foreground(DefaultTheme.Verdigris).Bold(true).Render("ready"))
		lines = append(lines,
			fmt.Sprintf("   %s  %s",
				lipgloss.NewStyle().Foreground(DefaultTheme.Muted).Render("VMID"),
				lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true).Render(fmt.Sprintf("%d", r.VMID))),
		)
		lines = append(lines,
			fmt.Sprintf("   %s  %s",
				lipgloss.NewStyle().Foreground(DefaultTheme.Muted).Render("node"),
				lipgloss.NewStyle().Foreground(DefaultTheme.Ink).Render(r.Node)),
		)
		lines = append(lines,
			fmt.Sprintf("   %s  %s",
				lipgloss.NewStyle().Foreground(DefaultTheme.Muted).Render("ipv4"),
				lipgloss.NewStyle().Foreground(DefaultTheme.Mercury).Render(r.IPv4)),
		)
		lines = append(lines,
			fmt.Sprintf("   %s  %s@%s",
				lipgloss.NewStyle().Foreground(DefaultTheme.Muted).Render("ssh"),
				lipgloss.NewStyle().Foreground(DefaultTheme.Ink).Render(r.User),
				lipgloss.NewStyle().Foreground(DefaultTheme.Mercury).Render(r.IPv4)),
		)
		lines = append(lines, "")
		lines = append(lines, " "+v.styles.Subtle.Render("⏎ deploy another  ·  esc edit this one"))
	case deployFailed:
		lines = append(lines, "")
		lines = append(lines, v.renderWrappedError(v.err.Error(), width))
		lines = append(lines, "")
		lines = append(lines, " "+v.styles.Subtle.Render("⏎ deploy another  ·  esc edit this one"))
	}
	return strings.Join(lines, "\n")
}

// renderProgressLine renders a "  N%  step  message" row, wrapping the
// message at the terminal width with subsequent lines indented under the
// message column.
func (v *DeployView) renderProgressLine(ev provision.ProgressEvent, width int) string {
	pct := lipgloss.NewStyle().Foreground(DefaultTheme.Muted).Render(fmt.Sprintf("%3d%%", int(ev.Percent)))
	step := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true).Render(padRight(ev.Step.String(), 10))
	prefix := fmt.Sprintf(" %s  %s  ", pct, step)
	prefixCols := lipgloss.Width(prefix)
	indent := strings.Repeat(" ", prefixCols)
	msgStyle := lipgloss.NewStyle().Foreground(DefaultTheme.Ink)

	wrapW := width - prefixCols - 1
	if wrapW < 20 {
		wrapW = 20
	}
	wrapped := lipgloss.NewStyle().Width(wrapW).Render(ev.Message)
	wlines := strings.Split(wrapped, "\n")
	out := make([]string, 0, len(wlines))
	for i, ln := range wlines {
		ln = strings.TrimRight(ln, " ")
		if i == 0 {
			out = append(out, prefix+msgStyle.Render(ln))
		} else {
			out = append(out, indent+msgStyle.Render(ln))
		}
	}
	return strings.Join(out, "\n")
}

// renderWrappedError wraps a multi-line error under the ✠ glyph, indenting
// continuation lines so the column structure stays readable.
func (v *DeployView) renderWrappedError(text string, width int) string {
	errStyle := lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Bold(true)
	wrapW := width - 4
	if wrapW < 20 {
		wrapW = 20
	}
	wrapped := lipgloss.NewStyle().Width(wrapW).Render(text)
	wlines := strings.Split(wrapped, "\n")
	out := make([]string, 0, len(wlines))
	for i, ln := range wlines {
		ln = strings.TrimRight(ln, " ")
		if i == 0 {
			out = append(out, " "+v.styles.StatusError.Render(Glyph.Error)+" "+errStyle.Render(ln))
		} else {
			out = append(out, "   "+errStyle.Render(ln))
		}
	}
	return strings.Join(out, "\n")
}

// headerSubtitle renders the right-side text on the deploy panel header,
// tracking the current state so the operator gets a stable status indicator
// at the top even when the log scrolls.
func (v *DeployView) headerSubtitle() string {
	switch v.state {
	case deployForm:
		return "clone template · configure · boot · resolve IP"
	case deployRunning:
		if len(v.progress) > 0 {
			latest := v.progress[len(v.progress)-1]
			return fmt.Sprintf("deploying… · %s", latest.Step.String())
		}
		return "deploying…"
	case deployDone:
		return "deployed"
	case deployFailed:
		return "failed"
	}
	return ""
}

// currentPct returns the latest percent for the bar — last event's value, or
// 100 when done.
func (v *DeployView) currentPct() float64 {
	if v.state == deployDone {
		return 100
	}
	if len(v.progress) == 0 {
		return 0
	}
	return v.progress[len(v.progress)-1].Percent
}

// renderProgressBar draws a wide horizontal bar with the % to its right.
// Color thresholds match the overview quota bars: verdigris → gold → vermilion.
const deployBarCells = 56

func (v *DeployView) renderProgressBar(pct float64) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(pct / 100 * float64(deployBarCells))
	fill := DefaultTheme.Verdigris
	switch {
	case v.state == deployFailed:
		fill = DefaultTheme.Vermilion
	case pct >= 100:
		fill = DefaultTheme.Gold
	}
	bar := lipgloss.NewStyle().Foreground(fill).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(DefaultTheme.Muted).Render(strings.Repeat("░", deployBarCells-filled))
	pctStr := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true).Render(fmt.Sprintf("%3d%%", int(pct)))
	return " " + bar + "  " + pctStr
}

// optAt returns the option at idx, or fallback if idx is out of range / list empty.
func optAt(opts []string, idx int, fallback string) string {
	if idx < 0 || idx >= len(opts) {
		return fallback
	}
	return opts[idx]
}
