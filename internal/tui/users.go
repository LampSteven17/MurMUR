package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rtx-monster/murmur/internal/config"
	"github.com/rtx-monster/murmur/internal/proxmox"
)

// usersState tracks the [U]sers tab's coarse phase.
type usersState int

const (
	usersListing usersState = iota
	usersActionMenu          // submenu popped from Enter on a user row
	usersAdding              // text-input form
	usersAddRunning          // ProxMox calls in flight
	usersSecret              // showing freshly-minted secret (modal)
	usersConfirmDelete       // typing username to confirm destroy
	usersConfirmRotate       // typing username to confirm token rotation
	usersBusy                // any other mutating call (delete/rotate/suspend)
)

// usersFetchedMsg returns from the ProxMox query that backs the list.
type usersFetchedMsg struct {
	users []proxmox.User
	pools []proxmox.Pool
	acl   []proxmox.ACLEntry
	err   error
}

// usersAddDoneMsg returns from the add bundle (user+pool+ACLs+token).
type usersAddDoneMsg struct {
	err      error
	created  config.User    // values to insert into cluster.yaml
	tokenVal string         // freshly-minted secret to show in modal
	// exportZip is the single hand-off archive (cluster.yaml + cluster.env +
	// README + murmur binary). Empty if the export step failed; exportErr
	// carries the reason.
	exportZip string
	exportErr error
}

// usersMutationDoneMsg returns from delete/rotate/suspend.
type usersMutationDoneMsg struct {
	err     error
	summary string // operator-visible result line
	// On delete, the cluster.yaml name that was removed (so the in-memory
	// config can be kept in sync). Empty for non-delete mutations.
	removedUser string
	// On rotate, surface the new secret in the modal.
	rotatedSecret string
	rotatedUser   config.User
}

// usersOwnedCheckMsg returns from the pre-delete ownership check. running
// counts guests in `running` state owned by the target user; stopped is
// guests in any non-running state. err set on API failures.
type usersOwnedCheckMsg struct {
	target  string
	running int
	stopped int
	err     error
}

// usersRow is one rendered row in the list — joins the PVE user record with
// derived "is this entry in cluster.yaml" status and the operator's pool name.
type usersRow struct {
	pveUser       proxmox.User
	inClusterYAML bool
	clusterEntry  config.User // valid when inClusterYAML
	hasPool       bool        // murmur-<name> exists
	tokens        int
}

// UsersView is the user-management tab. Admin-only — the parent app greys
// the tab key for non-admin roles. Operators who do reach this tab can:
//
//   - [a]dd a user (creates PVE user + pool + ACL bundle + token, appends
//     cluster.yaml entry, surfaces the freshly-minted secret in a modal)
//   - [d]elete a user (revokes ACL+token+pool, removes cluster.yaml entry)
//   - [r]otate a user's token (destroys + recreates, shows new secret)
//   - [s]uspend / [e]nable a user (PVE soft-disable)
//
// Orphan users — those that exist in PVE but not in cluster.yaml (root@pam,
// homepage-api, legacy accounts) — are shown but read-only. Manage those in
// the PVE web UI.
type UsersView struct {
	cfg        *config.Config
	client     *proxmox.Client
	active     *config.ActiveUser
	configPath string
	styles     Styles

	state    usersState
	loading  bool
	loaded   bool
	err      error
	fetched  time.Time

	rows   []usersRow
	cursor int

	// Add-form fields.
	addName    textinput.Model
	addComment textinput.Model
	addPubkey  textinput.Model
	addRoleIdx int
	addCursor  int // 0=name 1=role 2=comment 3=pubkey 4=submit
	addError   string

	// Confirm-typing fields (delete + rotate).
	confirmInput  textinput.Model
	confirmTarget string // expected username — must match input
	// confirmBlurb is the explanatory line shown above the type-to-confirm
	// input. For delete it's pre-populated with the orphan-guest count if
	// the safe-delete pre-check found stopped guests.
	confirmBlurb string

	// Action-submenu state. menuTarget is the row index in v.rows the
	// submenu was popped on; menuActions is the (label, callable) list for
	// that row (orphan rows get a single greyed-out "no actions" entry);
	// menuCursor is the focused entry within the submenu.
	menuTarget  int
	menuCursor  int
	menuActions []userAction

	// Secret-modal state.
	secretUser      config.User
	secretValue     string
	secretExportZip string // path to the hand-off .zip, if any
	secretExportErr error
	secretCopied    bool
	clipboardOK     bool   // true if [c] succeeded on the last attempt
	clipboardErr    string // tool name + reason on failure

	// Last operation status (rendered in footer until next action).
	statusMsg string
}

// userAction is one entry in the per-row submenu popped on Enter. Enabled
// reports whether the action is currently available; disabled entries
// render greyed and ignore Enter (handy for "delete" on self or orphans).
type userAction struct {
	label   string
	desc    string // one-line description shown under the menu
	enabled bool
	run     func(v *UsersView, row usersRow) tea.Cmd
}

// NewUsersView constructs the tab. configPath is the resolved cluster.yaml
// path — writeback edits this file.
func NewUsersView(cfg *config.Config, client *proxmox.Client, active *config.ActiveUser, configPath string) *UsersView {
	name := textinput.New()
	name.Placeholder = "operator-name (short, no @ — e.g. bob)"
	name.CharLimit = 32
	name.Width = 40

	comment := textinput.New()
	comment.Placeholder = "optional human description"
	comment.CharLimit = 80
	comment.Width = 50

	pubkey := textinput.New()
	pubkey.Placeholder = "ssh-ed25519 AAAA… (optional — blank uses cluster.ssh.identity)"
	pubkey.CharLimit = 0 // public keys can be long (RSA); don't truncate
	pubkey.Width = 50

	confirm := textinput.New()
	confirm.Placeholder = "type the username to confirm"
	confirm.CharLimit = 32
	confirm.Width = 40

	return &UsersView{
		cfg:        cfg,
		client:     client,
		active:     active,
		configPath: configPath,
		styles:     NewStyles(DefaultTheme),
		state:      usersListing,
		addName:    name,
		addComment: comment,
		addPubkey:  pubkey,
		confirmInput: confirm,
	}
}

func (v *UsersView) Init() tea.Cmd {
	v.loading = true
	return v.fetch()
}

func (v *UsersView) Title() string { return "users" }

func (v *UsersView) Help() []key.Binding {
	if v.active != nil && !v.active.Fallback && v.active.Role.Name != "admin" {
		return nil
	}
	switch v.state {
	case usersListing:
		// All actions reached via arrow keys + Enter so they don't collide
		// with the top tab bar's a/d/r/s hotkeys.
		return []key.Binding{
			key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "navigate")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open / add")),
		}
	case usersActionMenu:
		return []key.Binding{
			key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "pick action")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close menu")),
		}
	case usersAdding:
		return []key.Binding{
			key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}
	case usersSecret:
		return []key.Binding{
			key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy to clipboard")),
			key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "I've copied it")),
		}
	case usersConfirmDelete, usersConfirmRotate:
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}
	}
	return nil
}

// CapturesKeys: only return true for states where letter keys matter to the
// view (forms, confirm typing, the secret modal, and the open submenu). In
// the plain listing state we let letters bubble up so a/d/t/u/p/x still
// switch tabs — otherwise the operator gets stuck on this tab.
func (v *UsersView) CapturesKeys() bool {
	switch v.state {
	case usersActionMenu, usersAdding,
		usersConfirmDelete, usersConfirmRotate, usersSecret:
		return true
	}
	return false
}

func (v *UsersView) Update(msg tea.Msg) (View, tea.Cmd) {
	// Non-admin shouldn't be here. App-level tabAllowed prevents tab switch;
	// this is belt-and-braces in case someone constructs the view directly.
	if v.active != nil && !v.active.Fallback && v.active.Role.Name != "admin" {
		return v, nil
	}

	switch m := msg.(type) {
	case RefreshMsg:
		if v.loading {
			return v, nil
		}
		v.loading = true
		v.err = nil
		return v, v.fetch()
	case usersFetchedMsg:
		v.loading = false
		v.loaded = true
		v.fetched = time.Now()
		if m.err != nil {
			v.err = m.err
			return v, nil
		}
		v.rows = v.buildRows(m.users, m.pools)
		if v.cursor >= len(v.rows) {
			v.cursor = 0
		}
		return v, nil
	case usersAddDoneMsg:
		if m.err != nil {
			v.state = usersAdding
			v.addError = m.err.Error()
			return v, nil
		}
		// Keep the in-memory config in step with the cluster.yaml writeback that
		// just ran, so buildRows joins this user instead of treating it as an
		// orphan PVE account (which would hide it from the table).
		v.cfg.Users = append(v.cfg.Users, m.created)
		// Success: stash the secret for the modal, refresh list, persist yaml.
		v.secretUser = m.created
		v.secretValue = m.tokenVal
		v.secretExportZip = m.exportZip
		v.secretExportErr = m.exportErr
		v.secretCopied = false
		v.clipboardOK = false
		v.clipboardErr = ""
		v.state = usersSecret
		// Status line summarises the file-system side. PVE side already
		// mutated by the time we get here — writeback / export failures are
		// loud in the modal but the token still has to be recorded.
		switch {
		case m.exportErr != nil:
			v.statusMsg = fmt.Sprintf("added %s — PVE + cluster.yaml OK; starter kit export failed (see modal)", m.created.Name)
		case m.exportZip != "":
			v.statusMsg = fmt.Sprintf("added %s — wrote starter kit %s", m.created.Name, m.exportZip)
		default:
			v.statusMsg = fmt.Sprintf("added %s", m.created.Name)
		}
		return v, v.fetch() // refresh list under the modal
	case usersOwnedCheckMsg:
		if m.err != nil {
			v.state = usersListing
			v.statusMsg = "ownership check failed: " + m.err.Error()
			return v, nil
		}
		if m.running > 0 {
			v.state = usersListing
			v.statusMsg = fmt.Sprintf(
				"refuse to delete %s — owns %d running guest(s); stop or tear them down first",
				m.target, m.running)
			return v, nil
		}
		blurb := "This will revoke ACLs + token + pool, drop the PVE user and the cluster.yaml entry."
		if m.stopped > 0 {
			blurb += fmt.Sprintf(" %d stopped guest(s) owned by %s will be orphaned (their owner tag will reference a non-existent user).",
				m.stopped, m.target)
		}
		v.beginConfirmWithBlurb(m.target, usersConfirmDelete, blurb)
		v.statusMsg = ""
		return v, nil
	case usersMutationDoneMsg:
		if m.err != nil {
			v.state = usersListing
			v.statusMsg = "error: " + m.err.Error()
			return v, nil
		}
		v.statusMsg = m.summary
		// Mirror a delete into the in-memory config so the row drops out on the
		// next render (buildRows reads v.cfg.Users).
		if m.removedUser != "" {
			kept := v.cfg.Users[:0]
			for _, u := range v.cfg.Users {
				if u.Name != m.removedUser {
					kept = append(kept, u)
				}
			}
			v.cfg.Users = kept
		}
		if m.rotatedSecret != "" {
			v.secretUser = m.rotatedUser
			v.secretValue = m.rotatedSecret
			v.secretCopied = false
			v.clipboardOK = false
			v.clipboardErr = ""
			v.state = usersSecret
		} else {
			v.state = usersListing
		}
		return v, v.fetch()
	case tea.KeyMsg:
		return v.handleKey(m)
	}
	return v, nil
}

func (v *UsersView) handleKey(m tea.KeyMsg) (View, tea.Cmd) {
	switch v.state {
	case usersListing:
		return v.handleListKey(m)
	case usersActionMenu:
		return v.handleActionMenuKey(m)
	case usersAdding:
		return v.handleAddKey(m)
	case usersConfirmDelete, usersConfirmRotate:
		return v.handleConfirmKey(m)
	case usersSecret:
		return v.handleSecretKey(m)
	}
	return v, nil
}

// handleListKey: arrow keys move over rows; Enter activates. Row 0 is the
// virtual "+ add new user" row that opens the add form; subsequent rows
// (cursor-1) index into v.rows. No letter hotkeys here — they'd collide
// with the top tab bar (a=apps, d=deploy, r=refresh, etc).
func (v *UsersView) handleListKey(m tea.KeyMsg) (View, tea.Cmd) {
	maxIdx := len(v.rows) // includes the +add row at index 0
	switch m.String() {
	case "up", "k":
		if v.cursor > 0 {
			v.cursor--
		}
	case "down", "j":
		if v.cursor < maxIdx {
			v.cursor++
		}
	case "enter":
		if v.cursor == 0 {
			v.beginAdd()
			return v, nil
		}
		row := v.rowAt(v.cursor)
		if row == nil {
			return v, nil
		}
		v.openActionMenu(*row)
	}
	return v, nil
}

// handleActionMenuKey: arrow keys move the action cursor, Enter executes
// the focused action (if enabled), Esc closes the submenu.
func (v *UsersView) handleActionMenuKey(m tea.KeyMsg) (View, tea.Cmd) {
	switch m.String() {
	case "esc":
		v.state = usersListing
		v.menuActions = nil
		return v, nil
	case "up", "k":
		if v.menuCursor > 0 {
			v.menuCursor--
		}
	case "down", "j":
		if v.menuCursor < len(v.menuActions)-1 {
			v.menuCursor++
		}
	case "enter":
		if v.menuCursor < 0 || v.menuCursor >= len(v.menuActions) {
			return v, nil
		}
		act := v.menuActions[v.menuCursor]
		if !act.enabled {
			v.statusMsg = "action unavailable: " + act.label
			return v, nil
		}
		row := v.rowAt(v.menuTarget)
		if row == nil {
			v.state = usersListing
			return v, nil
		}
		v.state = usersListing
		v.menuActions = nil
		if act.run == nil {
			return v, nil
		}
		return v, act.run(v, *row)
	}
	return v, nil
}

// openActionMenu builds the available-actions list for the given row and
// transitions to the submenu state. Self-delete and orphan-row actions are
// included but disabled (so the operator can SEE that the action exists,
// they're just locked out of it for a stated reason).
func (v *UsersView) openActionMenu(row usersRow) {
	v.menuTarget = v.cursor
	v.menuCursor = 0
	v.menuActions = v.buildActionMenu(row)
	v.state = usersActionMenu
}

func (v *UsersView) buildActionMenu(row usersRow) []userAction {
	managed := row.inClusterYAML
	isSelf := managed && v.active != nil && !v.active.Fallback &&
		row.clusterEntry.Name == v.active.Name

	if !managed {
		return []userAction{
			{
				label:   "(no actions)",
				desc:    "orphan PVE account — manage via the PVE web UI",
				enabled: false,
			},
			{label: "close menu", desc: "go back to the user list", enabled: true},
		}
	}

	rotateAction := userAction{
		label:   "rotate token",
		desc:    "revoke the current token and mint a fresh one (consumer cluster.env must be updated)",
		enabled: true,
		run: func(v *UsersView, row usersRow) tea.Cmd {
			v.beginConfirm(row.clusterEntry.Name, usersConfirmRotate)
			return nil
		},
	}
	suspendLabel := "suspend"
	suspendDesc := "soft-disable the PVE account (token stops working; reversible)"
	if row.pveUser.Enable == 0 {
		suspendLabel = "re-enable"
		suspendDesc = "re-enable the suspended PVE account"
	}
	suspendAction := userAction{
		label:   suspendLabel,
		desc:    suspendDesc,
		enabled: true,
		run: func(v *UsersView, row usersRow) tea.Cmd {
			v.state = usersBusy
			return v.toggleSuspend(row)
		},
	}
	deleteAction := userAction{
		label:   "delete",
		desc:    "revoke ACLs + token + pool, drop the PVE user, remove cluster.yaml entry",
		enabled: !isSelf,
		run: func(v *UsersView, row usersRow) tea.Cmd {
			// Reuse the existing async safe-delete pre-check path.
			v.state = usersBusy
			v.statusMsg = "checking what " + row.clusterEntry.Name + " owns…"
			return v.checkOwnedGuests(row.clusterEntry.Name)
		},
	}
	if isSelf {
		deleteAction.desc = "cannot delete yourself — ask another admin (or use the PVE web UI)"
	}

	return []userAction{
		rotateAction,
		suspendAction,
		deleteAction,
		{label: "close menu", desc: "go back to the user list", enabled: true},
	}
}

// rowAt resolves a list-cursor index (where 0 = the virtual +add row) to a
// real usersRow pointer, or nil if out of range.
func (v *UsersView) rowAt(idx int) *usersRow {
	if idx <= 0 || idx > len(v.rows) {
		return nil
	}
	return &v.rows[idx-1]
}

func (v *UsersView) handleAddKey(m tea.KeyMsg) (View, tea.Cmd) {
	switch m.Type {
	case tea.KeyEsc:
		v.state = usersListing
		v.addError = ""
		return v, nil
	case tea.KeyTab, tea.KeyShiftTab:
		dir := 1
		if m.Type == tea.KeyShiftTab {
			dir = -1
		}
		v.addCursor = (v.addCursor + dir + 5) % 5
		v.refocusAddInputs()
		return v, nil
	case tea.KeyEnter:
		if v.addCursor == 4 {
			return v.submitAdd()
		}
		v.addCursor = (v.addCursor + 1) % 5
		v.refocusAddInputs()
		return v, nil
	}
	// Role picker is keyboard-driven on left/right.
	if v.addCursor == 1 {
		switch m.String() {
		case "left", "h":
			if v.addRoleIdx > 0 {
				v.addRoleIdx--
			}
			return v, nil
		case "right", "l":
			if v.addRoleIdx < len(v.addableRoles())-1 {
				v.addRoleIdx++
			}
			return v, nil
		}
	}
	// Route to the focused textinput.
	var cmd tea.Cmd
	switch v.addCursor {
	case 0:
		v.addName, cmd = v.addName.Update(m)
	case 2:
		v.addComment, cmd = v.addComment.Update(m)
	case 3:
		v.addPubkey, cmd = v.addPubkey.Update(m)
	}
	return v, cmd
}

func (v *UsersView) handleConfirmKey(m tea.KeyMsg) (View, tea.Cmd) {
	switch m.Type {
	case tea.KeyEsc:
		v.state = usersListing
		v.statusMsg = ""
		return v, nil
	case tea.KeyEnter:
		if strings.TrimSpace(v.confirmInput.Value()) != v.confirmTarget {
			v.statusMsg = "name mismatch — aborted"
			v.state = usersListing
			return v, nil
		}
		// Find the row.
		var row *usersRow
		for i := range v.rows {
			if v.rows[i].inClusterYAML && v.rows[i].clusterEntry.Name == v.confirmTarget {
				row = &v.rows[i]
				break
			}
		}
		if row == nil {
			v.statusMsg = "target user gone — list may be stale"
			v.state = usersListing
			return v, nil
		}
		switch v.state {
		case usersConfirmDelete:
			v.state = usersBusy
			return v, v.executeDelete(*row)
		case usersConfirmRotate:
			v.state = usersBusy
			return v, v.executeRotate(*row)
		}
	}
	var cmd tea.Cmd
	v.confirmInput, cmd = v.confirmInput.Update(m)
	return v, cmd
}

func (v *UsersView) handleSecretKey(m tea.KeyMsg) (View, tea.Cmd) {
	switch m.String() {
	case "c":
		v.clipboardOK = false
		v.clipboardErr = ""
		if tool, err := copyToClipboard(v.secretValue); err != nil {
			v.clipboardErr = err.Error()
		} else {
			v.clipboardOK = true
			v.clipboardErr = "copied via " + tool
		}
		return v, nil
	case "y", "enter", "esc":
		// Confirm or dismiss — either way, clear the secret from memory.
		v.secretValue = ""
		v.secretUser = config.User{}
		v.state = usersListing
		return v, nil
	}
	return v, nil
}

// beginAdd resets the add-form fields and switches state.
func (v *UsersView) beginAdd() {
	v.addName.SetValue("")
	v.addComment.SetValue("")
	v.addPubkey.SetValue("")
	v.addRoleIdx = 1 // default to deployer — safer than admin-by-default
	v.addCursor = 0
	v.addError = ""
	v.state = usersAdding
	v.refocusAddInputs()
}

func (v *UsersView) refocusAddInputs() {
	v.addName.Blur()
	v.addComment.Blur()
	v.addPubkey.Blur()
	switch v.addCursor {
	case 0:
		v.addName.Focus()
	case 2:
		v.addComment.Focus()
	case 3:
		v.addPubkey.Focus()
	}
}

func (v *UsersView) beginConfirm(name string, st usersState) {
	v.beginConfirmWithBlurb(name, st, "")
}

// beginConfirmWithBlurb sets the type-to-confirm state with an explanatory
// line shown above the input. Used by the delete flow to surface the
// safe-delete pre-check's orphan-guest count.
func (v *UsersView) beginConfirmWithBlurb(name string, st usersState, blurb string) {
	v.confirmInput.SetValue("")
	v.confirmInput.Focus()
	v.confirmTarget = name
	v.confirmBlurb = blurb
	v.state = st
}

// checkOwnedGuests counts guests tagged murmur-owner-<name>. Returns counts
// split by status so the caller can refuse on running and warn on stopped.
func (v *UsersView) checkOwnedGuests(name string) tea.Cmd {
	client := v.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		resources, err := client.GetResources(ctx, "vm")
		if err != nil {
			return usersOwnedCheckMsg{target: name, err: err}
		}
		want := "murmur-owner-" + name
		var running, stopped int
		for _, r := range resources {
			if r.Type != "qemu" && r.Type != "lxc" {
				continue
			}
			if r.Template == 1 {
				continue
			}
			if !tagsContain(r.Tags, want) {
				continue
			}
			if r.Status == "running" {
				running++
			} else {
				stopped++
			}
		}
		return usersOwnedCheckMsg{target: name, running: running, stopped: stopped}
	}
}

// tagsContain reports whether the comma/semicolon/space-separated tag string
// includes the exact token want. Matches resourceHasOwner's semantics so
// behavior here lines up with the list-view filter.
func tagsContain(tags, want string) bool {
	for _, t := range splitTags(tags) {
		if t == want {
			return true
		}
	}
	return false
}

func (v *UsersView) currentRow() *usersRow {
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		return nil
	}
	return &v.rows[v.cursor]
}

// addableRoles returns the role names the [a]dd flow can configure ACLs for.
// Limited to admin/deployer because each has a known ACL bundle; user-defined
// roles are valid for murmur-side gating but require manual ACL setup via the
// PVE web UI.
func (v *UsersView) addableRoles() []string {
	return []string{"admin", "deployer"}
}

// fetch returns the cmd that loads users + pools + acls in one go.
func (v *UsersView) fetch() tea.Cmd {
	client := v.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		users, err := client.ListUsers(ctx, true)
		if err != nil {
			return usersFetchedMsg{err: err}
		}
		pools, err := client.ListPools(ctx)
		if err != nil {
			return usersFetchedMsg{err: err}
		}
		acl, err := client.ListACL(ctx)
		if err != nil {
			return usersFetchedMsg{err: err}
		}
		return usersFetchedMsg{users: users, pools: pools, acl: acl}
	}
}

// buildRows joins PVE user records with the cluster.yaml users: section and
// pool existence. Only cluster.yaml-managed users appear; orphan PVE
// accounts (root@pam, traefik-sync, homepage-api, etc.) are filtered out —
// murmur isn't trying to be a generic PVE user manager. Rows are sorted
// alphabetically by their murmur name.
func (v *UsersView) buildRows(pveUsers []proxmox.User, pools []proxmox.Pool) []usersRow {
	clusterByProxmoxUser := map[string]config.User{}
	for _, u := range v.cfg.Users {
		clusterByProxmoxUser[u.ProxmoxUser] = u
	}
	poolByID := map[string]bool{}
	for _, p := range pools {
		poolByID[p.PoolID] = true
	}
	rows := make([]usersRow, 0, len(pveUsers))
	for _, u := range pveUsers {
		ce, ok := clusterByProxmoxUser[u.UserID]
		if !ok {
			continue // orphan PVE account — not murmur's concern
		}
		rows = append(rows, usersRow{
			pveUser:       u,
			tokens:        len(u.Tokens),
			inClusterYAML: true,
			clusterEntry:  ce,
			hasPool:       poolByID["murmur-"+ce.Name],
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].clusterEntry.Name < rows[j].clusterEntry.Name
	})
	return rows
}

// submitAdd validates the add form, runs the ProxMox-side bundle in a tea.Cmd,
// and on success transitions to the secret modal.
func (v *UsersView) submitAdd() (View, tea.Cmd) {
	name := strings.TrimSpace(v.addName.Value())
	comment := strings.TrimSpace(v.addComment.Value())
	pubkey := strings.TrimSpace(v.addPubkey.Value())
	roleName := v.addableRoles()[v.addRoleIdx]

	if name == "" {
		v.addError = "name is required"
		return v, nil
	}
	if strings.ContainsAny(name, "@! ") {
		v.addError = "name must be a short identity (no @, !, or spaces) — the PVE realm is added automatically"
		return v, nil
	}
	for _, u := range v.cfg.Users {
		if u.Name == name {
			v.addError = "name already exists in cluster.yaml users:"
			return v, nil
		}
	}
	// Fail fast on a malformed pubkey, before any PVE-side mutation.
	if pubkey != "" && !config.LooksLikeSSHPubKey(pubkey) {
		v.addError = "ssh key doesn't look like an OpenSSH public key (expected 'ssh-ed25519 AAAA…')"
		return v, nil
	}

	v.state = usersAddRunning
	v.addError = ""
	client := v.client
	configPath := v.configPath
	tokenEnv := suggestEnvName(name)
	return v, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = configPath // appended to YAML after success in Update handler

		// 1. Create user.
		pveUserID := name + "@pve"
		if err := client.CreateUser(ctx, pveUserID, comment); err != nil {
			return usersAddDoneMsg{err: fmt.Errorf("create user %s: %w", pveUserID, err)}
		}
		// 2. Create the operator's ownership pool.
		poolID := "murmur-" + name
		if err := client.CreatePool(ctx, poolID, fmt.Sprintf("murmur ownership pool for operator %q", name)); err != nil {
			// Tolerate "already exists" — pool may have survived a partial
			// previous attempt. Loudly fail other errors.
			if !strings.Contains(err.Error(), "already exists") {
				return usersAddDoneMsg{err: fmt.Errorf("create pool %s: %w", poolID, err)}
			}
		}
		// Deployers clone from the shared templates pool; make sure it exists so
		// the PVETemplateUser grant below lands on a real path. (Templates are
		// added to it at build time — see Orchestrator.buildVMTemplate.)
		if roleName == "deployer" {
			if err := client.CreatePool(ctx, config.TemplatesPoolID, "murmur shared templates — operators clone from here"); err != nil {
				if !strings.Contains(err.Error(), "already exists") {
					return usersAddDoneMsg{err: fmt.Errorf("ensure templates pool %s: %w", config.TemplatesPoolID, err)}
				}
			}
		}
		// 3. Apply ACL bundle for the chosen role.
		for _, b := range aclBundleFor(roleName, poolID) {
			if err := client.SetACL(ctx, b.path, b.role,
				[]string{pveUserID}, nil, nil, true); err != nil {
				return usersAddDoneMsg{err: fmt.Errorf("acl %s → %s: %w", b.path, b.role, err)}
			}
		}
		// 4. Mint API token (privsep=0 so the token inherits the user's perms).
		tok, err := client.CreateToken(ctx, pveUserID, "murmur", "", false)
		if err != nil {
			return usersAddDoneMsg{err: fmt.Errorf("create token: %w", err)}
		}
		created := config.User{
			Name:         name,
			Role:         roleName,
			ProxmoxUser:  pveUserID,
			ProxmoxToken: "murmur",
			TokenSecret:  "${" + tokenEnv + "}",
			SSHPubKey:    pubkey,
			Comment:      comment,
		}
		// File-system side, in order: writeback to cluster.yaml first (so the
		// admin's repo reflects the new operator), then export a starter
		// folder for the new operator. PVE state is already mutated by this
		// point, so we surface either failure in the result rather than
		// rolling back.
		var (
			exportZip string
			exportErr error
		)
		if err := config.AppendUserToFile(configPath, created); err != nil {
			return usersAddDoneMsg{
				err: fmt.Errorf("cluster.yaml writeback (PVE side already mutated): %w", err),
				created:  created,
				tokenVal: tok.Value,
			}
		}
		// Bundle the murmur binary we're running so the operator just unzips
		// and launches. os.Executable failing is near-impossible on linux; if
		// it does, ship the kit without the binary and the README says so.
		selfBin, _ := os.Executable()
		res, err := config.ExportOperatorFolder(configPath, created, tok.Value, "", selfBin)
		if err != nil {
			exportErr = err
		} else {
			exportZip = res.ZipPath
		}
		return usersAddDoneMsg{
			created:   created,
			tokenVal:  tok.Value,
			exportZip: exportZip,
			exportErr: exportErr,
		}
	}
}

// executeDelete revokes the user's token, ACL entries that reference them,
// the pool (if empty), and the user record itself; then drops the
// cluster.yaml entry.
func (v *UsersView) executeDelete(row usersRow) tea.Cmd {
	client := v.client
	configPath := v.configPath
	entry := row.clusterEntry
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// 1. Delete token (best effort — may already be gone).
		_ = client.DeleteToken(ctx, entry.ProxmoxUser, entry.ProxmoxToken)
		// 2. Remove ACLs we know we placed (we don't track them — query
		// current ACL and drop any matching this user across the standard
		// paths). PVE's DELETE on /access/acl is the same endpoint as set,
		// with delete=1.
		acls, err := client.ListACL(ctx)
		if err != nil {
			return usersMutationDoneMsg{err: fmt.Errorf("list acl for cleanup: %w", err)}
		}
		for _, a := range acls {
			if a.Type != "user" || a.UGID != entry.ProxmoxUser {
				continue
			}
			if err := client.DeleteACL(ctx, a.Path, a.RoleID, []string{entry.ProxmoxUser}, nil, nil); err != nil {
				return usersMutationDoneMsg{err: fmt.Errorf("remove acl %s → %s: %w", a.Path, a.RoleID, err)}
			}
		}
		// 3. Delete pool (only if empty — PVE will refuse otherwise).
		_ = client.DeletePool(ctx, "murmur-"+entry.Name)
		// 4. Delete user.
		if err := client.DeleteUser(ctx, entry.ProxmoxUser); err != nil {
			return usersMutationDoneMsg{err: fmt.Errorf("delete user: %w", err)}
		}
		// 5. Remove from cluster.yaml.
		if err := config.RemoveUserFromFile(configPath, entry.Name); err != nil {
			return usersMutationDoneMsg{err: fmt.Errorf("cluster.yaml writeback: %w", err)}
		}
		return usersMutationDoneMsg{
			summary:     fmt.Sprintf("deleted %s — PVE + cluster.yaml updated", entry.Name),
			removedUser: entry.Name,
		}
	}
}

// executeRotate destroys the current token and mints a fresh one with the
// same name. Surfaces the new secret in the modal so the operator can update
// the consumer's cluster.env.
func (v *UsersView) executeRotate(row usersRow) tea.Cmd {
	client := v.client
	entry := row.clusterEntry
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := client.DeleteToken(ctx, entry.ProxmoxUser, entry.ProxmoxToken); err != nil {
			return usersMutationDoneMsg{err: fmt.Errorf("revoke old token: %w", err)}
		}
		tok, err := client.CreateToken(ctx, entry.ProxmoxUser, entry.ProxmoxToken, "", false)
		if err != nil {
			return usersMutationDoneMsg{err: fmt.Errorf("mint new token: %w", err)}
		}
		return usersMutationDoneMsg{
			summary:       fmt.Sprintf("rotated %s — new secret shown once", entry.Name),
			rotatedSecret: tok.Value,
			rotatedUser:   entry,
		}
	}
}

// toggleSuspend flips the enable flag on the underlying PVE user. No
// confirmation prompt — it's a soft toggle, reversible.
func (v *UsersView) toggleSuspend(row usersRow) tea.Cmd {
	client := v.client
	pveUser := row.pveUser
	enabling := pveUser.Enable == 0
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.SetUserEnabled(ctx, pveUser.UserID, enabling); err != nil {
			return usersMutationDoneMsg{err: err}
		}
		verb := "suspended"
		if enabling {
			verb = "re-enabled"
		}
		return usersMutationDoneMsg{summary: fmt.Sprintf("%s %s", verb, pveUser.UserID)}
	}
}

// aclBundle is one (path, role) to grant a new operator at add-user time.
type aclBundle struct {
	path string
	role string
}

// aclBundleFor returns the PVE-side ACL set for a given murmur role. The
// poolID placeholder slot is substituted when the role uses pool scoping.
func aclBundleFor(role, poolID string) []aclBundle {
	switch role {
	case "admin":
		return []aclBundle{{path: "/", role: "Administrator"}}
	case "deployer":
		return []aclBundle{
			{path: "/pool/" + poolID, role: "PVEVMAdmin"},
			// PVEVMAdmin on the pool gives VM.* but, per PVE's "deeper level
			// replaces inherited" rule, it shadows the PVEAuditor on / for this
			// path — stripping Pool.Audit/Pool.Allocate exactly where the
			// operator needs them. Re-add the pool privileges here so they can
			// see and allocate guests into their own pool.
			{path: "/pool/" + poolID, role: "PVEPoolAdmin"},
			// VM.Clone on the shared templates pool — and only there — so the
			// operator can clone templates without gaining any access to other
			// operators' guests. Templates are dropped into this pool at build
			// time (see Orchestrator.buildVMTemplate).
			{path: "/pool/" + config.TemplatesPoolID, role: "PVETemplateUser"},
			{path: "/storage", role: "PVEDatastoreUser"},
			{path: "/sdn", role: "PVESDNUser"},
			{path: "/", role: "PVEAuditor"},
		}
	}
	return nil
}

// suggestEnvName builds the conventional ${VAR} name for an operator's token
// (e.g. "bob" → "BOB_TOKEN").
func suggestEnvName(name string) string {
	upper := strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(name))
	return upper + "_TOKEN"
}

// copyToClipboard tries xclip, then wl-copy, then pbcopy. Returns the tool
// name on success. All three accept stdin → clipboard.
func copyToClipboard(s string) (string, error) {
	candidates := [][]string{
		{"xclip", "-selection", "clipboard"},
		{"wl-copy"},
		{"pbcopy"},
	}
	var lastErr error
	for _, args := range candidates {
		if _, err := exec.LookPath(args[0]); err != nil {
			lastErr = err
			continue
		}
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdin = strings.NewReader(s)
		if err := cmd.Run(); err != nil {
			lastErr = err
			continue
		}
		return args[0], nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no clipboard tool found (install xclip / wl-copy / pbcopy)")
	}
	return "", lastErr
}

// --- rendering ---

func (v *UsersView) View(width, height int) string {
	var b strings.Builder

	header := v.styles.Title.Render("☉  USERS")
	right := ""
	if v.loaded {
		right = v.styles.Subtle.Render(fmt.Sprintf("%d users · %d in cluster.yaml",
			len(v.rows), v.clusterEntryCount()))
	}
	gap := width - lipgloss.Width(header) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	b.WriteString(header + lipgloss.NewStyle().Width(gap).Render("") + right)
	b.WriteString("\n")
	if v.statusMsg != "" {
		b.WriteString(" " + v.styles.Subtle.Render("· "+v.statusMsg) + "\n")
	}
	b.WriteString("\n")

	switch v.state {
	case usersAdding:
		b.WriteString(v.renderAddForm())
	case usersAddRunning, usersBusy:
		b.WriteString(v.styles.Subtle.Render(" " + Glyph.InFlight + " applying changes…"))
	case usersConfirmDelete:
		blurb := v.confirmBlurb
		if blurb == "" {
			blurb = "This will revoke ACLs + token + pool, then drop the PVE user and the cluster.yaml entry."
		}
		b.WriteString(v.renderConfirm("DELETE", DefaultTheme.Vermilion, blurb))
	case usersConfirmRotate:
		b.WriteString(v.renderConfirm("ROTATE TOKEN", DefaultTheme.Gold,
			"The current token will be revoked and replaced. Consumer cluster.env must be updated."))
	case usersSecret:
		b.WriteString(v.renderSecretModal(width))
	case usersActionMenu:
		// Render the list with the cursor frozen, then the submenu beneath.
		b.WriteString(v.renderList(width))
		b.WriteString("\n\n")
		b.WriteString(v.renderActionMenu())
	default:
		// usersListing
		switch {
		case v.err != nil:
			b.WriteString(v.styles.StatusError.Render(Glyph.Error+" ") +
				lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Render(v.err.Error()))
		case !v.loaded:
			b.WriteString(v.styles.Subtle.Render(" " + Glyph.InFlight + " loading users…"))
		default:
			b.WriteString(v.renderList(width))
		}
	}

	return padBlock(b.String(), width, height)
}

func (v *UsersView) clusterEntryCount() int {
	n := 0
	for _, r := range v.rows {
		if r.inClusterYAML {
			n++
		}
	}
	return n
}

func (v *UsersView) renderList(width int) string {
	gold := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	verd := lipgloss.NewStyle().Foreground(DefaultTheme.Verdigris)
	verm := lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion)

	var lines []string

	// === Section 1 — Add new user ===
	lines = append(lines, v.styles.Title.Render(" ADD"))
	addCursor := "  "
	if v.cursor == 0 {
		addCursor = v.styles.SelectionMark.Render("▶ ")
	}
	addLine := fmt.Sprintf("  %s%s %s",
		addCursor,
		gold.Render("+"),
		muted.Render(" add new user                · open the add form (admin / deployer)"))
	if v.cursor == 0 {
		addLine = gold.Render(addLine)
	}
	lines = append(lines, addLine, "", "")

	// === Section 2 — Active users (cluster.yaml-managed only) ===
	if len(v.rows) == 0 {
		lines = append(lines,
			v.styles.Title.Render(" ACTIVE USERS"),
			"",
			"  "+v.styles.Subtle.Render(Glyph.Empty+" none yet — select [+] above and press enter"),
		)
		return strings.Join(lines, "\n")
	}
	hdr := fmt.Sprintf("   %-3s %-18s %-12s %-22s %-7s %s",
		"", "murmur name", "role", "pve user", "tokens", "status")
	lines = append(lines,
		v.styles.Title.Render(" ACTIVE USERS"),
		muted.Render(hdr),
	)
	for i, r := range v.rows {
		rowIdx := i + 1 // shift past the +add row at cursor index 0
		cursor := "  "
		if v.cursor == rowIdx {
			cursor = v.styles.SelectionMark.Render("▶ ")
		}
		var pveStatus string
		if r.pveUser.Enable == 0 {
			pveStatus = verm.Render("suspended")
		} else if !r.hasPool {
			pveStatus = verm.Render("missing pool")
		} else {
			pveStatus = verd.Render("ok")
		}
		line := fmt.Sprintf("  %s%s %-18s %-12s %-22s %-7d %s",
			cursor, verd.Render("⬢"),
			truncate(r.clusterEntry.Name, 18),
			truncate(r.clusterEntry.Role, 12),
			truncate(r.pveUser.UserID, 22),
			r.tokens,
			pveStatus,
		)
		if v.cursor == rowIdx {
			line = gold.Render(line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// renderActionMenu draws the per-row submenu popped on Enter. Disabled
// entries are rendered greyed and a one-line description of the focused
// entry sits below the menu.
func (v *UsersView) renderActionMenu() string {
	gold := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	verm := lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Bold(true)

	target := v.rowAt(v.menuTarget)
	heading := "  ACTIONS"
	if target != nil {
		if target.inClusterYAML {
			heading = "  ACTIONS · " + target.clusterEntry.Name
		} else {
			heading = "  ACTIONS · " + target.pveUser.UserID + " (orphan)"
		}
	}
	lines := []string{v.styles.Title.Render(heading)}
	for i, act := range v.menuActions {
		cursor := "    "
		if v.menuCursor == i {
			cursor = "  " + v.styles.SelectionMark.Render("▶ ")
		}
		label := act.label
		switch {
		case !act.enabled:
			label = muted.Render(label)
		case v.menuCursor == i:
			label = gold.Render(label)
		default:
			label = v.styles.Subtle.Render(label)
		}
		// Visually emphasise destructive actions when focused.
		if v.menuCursor == i && act.enabled && (act.label == "delete") {
			label = verm.Render(act.label)
		}
		lines = append(lines, cursor+label)
	}
	// Description of the focused entry.
	if v.menuCursor >= 0 && v.menuCursor < len(v.menuActions) {
		desc := v.menuActions[v.menuCursor].desc
		if desc != "" {
			lines = append(lines, "", "    "+muted.Render("· "+desc))
		}
	}
	return strings.Join(lines, "\n")
}

func (v *UsersView) renderAddForm() string {
	gold := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	verm := lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion)

	roles := v.addableRoles()
	roleLine := ""
	for i, r := range roles {
		s := r
		if i == v.addRoleIdx {
			s = gold.Render("[ " + r + " ]")
		} else {
			s = muted.Render("  " + r + "  ")
		}
		roleLine += s
	}

	rows := []string{
		v.styles.Title.Render(" ADD OPERATOR"),
		"",
		fieldLabel("name      ", v.addCursor == 0) + v.addName.View(),
		fieldLabel("role      ", v.addCursor == 1) + roleLine + muted.Render("  ← / →"),
		fieldLabel("comment   ", v.addCursor == 2) + v.addComment.View(),
		fieldLabel("ssh key   ", v.addCursor == 3) + v.addPubkey.View(),
		muted.Render("              optional — paste the operator's public key; blank = use cluster.ssh.identity"),
		"",
		fieldLabel("[ submit ]", v.addCursor == 4) + muted.Render(" press enter on submit"),
	}
	if v.addError != "" {
		rows = append(rows, "", verm.Render("  "+Glyph.Error+" "+v.addError))
	}
	rows = append(rows, "", muted.Render("  tab/shift-tab moves, esc cancels"))
	return strings.Join(rows, "\n")
}

func fieldLabel(label string, focused bool) string {
	gold := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	if focused {
		return "  " + gold.Render(label) + "  "
	}
	return "  " + muted.Render(label) + "  "
}

func (v *UsersView) renderConfirm(action string, color lipgloss.Color, blurb string) string {
	style := lipgloss.NewStyle().Foreground(color).Bold(true)
	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	rows := []string{
		style.Render(" " + Glyph.Error + "  " + action),
		"",
		"  " + blurb,
		"",
		"  " + muted.Render("type the operator name to confirm: ") + style.Render(v.confirmTarget),
		"",
		"  " + v.confirmInput.View(),
		"",
		muted.Render("  enter confirms · esc cancels"),
	}
	return strings.Join(rows, "\n")
}

func (v *UsersView) renderSecretModal(width int) string {
	gold := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	verm := lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Bold(true)
	verd := lipgloss.NewStyle().Foreground(DefaultTheme.Verdigris)
	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)

	envHint := suggestEnvName(v.secretUser.Name)
	rows := []string{
		verm.Render(" " + Glyph.Sealed + "  NEW TOKEN — shown once, never retrievable"),
		"",
		"  " + muted.Render("operator:    ") + gold.Render(v.secretUser.Name),
		"  " + muted.Render("token id:    ") + gold.Render(v.secretUser.FullTokenID()),
		"  " + muted.Render("secret:      ") + gold.Render(v.secretValue),
		"",
	}
	// Starter-kit export status. Successful path shown prominently so admin
	// knows what to hand the new operator. Failure surfaces the reason and the
	// env-var line they'd need to paste manually.
	switch {
	case v.secretExportZip != "":
		rows = append(rows,
			"  "+muted.Render("Operator starter kit (zip):"),
			"  "+verd.Render("    "+v.secretExportZip),
			"  "+muted.Render("    bundles cluster.yaml, cluster.env (token inside), README.md, murmur binary"),
			"  "+muted.Render("    hand it to "+v.secretUser.Name+" — they unzip and run `./murmur tui`"),
			"",
		)
	case v.secretExportErr != nil:
		rows = append(rows,
			"  "+verm.Render("Starter folder export failed: "+v.secretExportErr.Error()),
			"  "+muted.Render("Add to "+v.secretUser.Name+"'s cluster.env manually:"),
			"  "+verd.Render("    "+envHint+"="+v.secretValue),
			"",
		)
	default:
		rows = append(rows,
			"  "+muted.Render("Add to "+v.secretUser.Name+"'s cluster.env:"),
			"  "+verd.Render("    "+envHint+"="+v.secretValue),
			"",
		)
	}
	switch {
	case v.clipboardOK:
		rows = append(rows, verd.Render("  ✓ "+v.clipboardErr))
	case v.clipboardErr != "":
		rows = append(rows, verm.Render("  ✗ clipboard: "+v.clipboardErr))
	}
	rows = append(rows,
		"",
		muted.Render("  [c] copy secret to clipboard   [y] I've recorded it (closes modal)"),
	)
	_ = width
	return strings.Join(rows, "\n")
}
