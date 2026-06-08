package config

type Config struct {
	Cluster         Cluster          `yaml:"cluster"`
	Flavors         []Flavor         `yaml:"flavors"`
	Images          []Image          `yaml:"images"`
	Apps            []App            `yaml:"apps,omitempty"`
	ReverseProxy    *ReverseProxy    `yaml:"reverse_proxy,omitempty"`
	Monitoring      *Monitoring      `yaml:"monitoring,omitempty"`
	StorageBackends []StorageBackend `yaml:"storage_backends"`

	// Users + Roles are optional. When both are empty murmur runs in single-
	// operator mode (uses cluster.api.token_* directly as an implicit admin),
	// preserving v0.1 behavior. When either is present, identity resolution
	// kicks in: --as <name>, $MURMUR_USER, or unambiguous single-user pick.
	Users []User `yaml:"users,omitempty"`
	Roles []Role `yaml:"roles,omitempty"`
}

// User is one operator identity backed by a ProxMox user + token. TokenSecret
// is resolved lazily from cluster.env: only the active user's secret needs to
// be defined in the operator's environment, so each operator can ship their
// own cluster.env without seeing other operators' tokens.
type User struct {
	Name         string `yaml:"name"`          // short identity used by --as / MURMUR_USER
	Role         string `yaml:"role"`          // must match one of Config.Roles (after builtin merge)
	ProxmoxUser  string `yaml:"proxmox_user"`  // "alice@pve" — backing PVE user
	ProxmoxToken string `yaml:"proxmox_token"` // token name (bit after `!`)
	TokenSecret  string `yaml:"token_secret"`  // secret value; ${VAR} resolved at identity selection
	// SSHPubKey is the operator's OpenSSH public key. When set, guests this
	// operator deploys trust it (baked into cloud-init / LXC authorized keys)
	// instead of the on-disk cluster.ssh.identity counterpart. Optional: blank
	// falls back to cluster.ssh.identity. The matching private key stays on the
	// operator's own machine and is used for patch/update SSH.
	SSHPubKey string `yaml:"ssh_pubkey,omitempty"`
	Comment   string `yaml:"comment,omitempty"`
}

// FullTokenID returns the ProxMox API token id ("user@realm!tokenname").
func (u User) FullTokenID() string {
	return u.ProxmoxUser + "!" + u.ProxmoxToken
}

// Role bundles the permissions one or more Users are granted murmur-side.
// PVE ACLs on the underlying token enforce the real perimeter; Role controls
// TUI affordances + ownership filtering.
//
// `*` is a wildcard accepted by Tabs, Actions, and Apps.
type Role struct {
	Name    string   `yaml:"name"`
	Tabs    []string `yaml:"tabs"`    // tab keys visible in the top bar; `[*]` = all
	Actions []string `yaml:"actions"` // deploy | teardown | patch | host-update | manage-users; `[*]` = all
	Apps    []string `yaml:"apps"`    // app names from Apps catalog this role can deploy; `[*]` = all
	Guests  string   `yaml:"guests"`  // "own" (filter list views by murmur-owner tag) | "all"
}

// Allows reports whether the role grants the given wildcard-allowable item.
// Empty slice = no access; ["*"] = all; otherwise exact-match.
func (r Role) Allows(field []string, item string) bool {
	for _, f := range field {
		if f == "*" || f == item {
			return true
		}
	}
	return false
}

// App is a declarative VM-plus-configuration unit. Pick an app in the [a]apps
// tab and murmur provisions a guest matching Image/Flavor, then runs the
// post-deploy step (a Playbook path or a raw PostDeploy shell command).
//
// Exactly one of Playbook or PostDeploy may be set, both can be empty
// (provision-only, no configuration). Validation enforces this.
type App struct {
	Name       string `yaml:"name"`
	Type       string `yaml:"type,omitempty"` // "vm" | "lxc" (default "vm" when empty)
	Image      string `yaml:"image"`
	Flavor     string `yaml:"flavor"`
	Playbook   string `yaml:"playbook,omitempty"`    // ansible playbook path (relative to config dir)
	PostDeploy string `yaml:"post_deploy,omitempty"` // raw shell command (alternative to Playbook)

	// Update is a shell command run via SSH against the running guest when
	// the operator triggers a patch in the [p]atch tab. Typical content:
	// `cd /opt/X && docker compose pull && docker compose up -d`, or an
	// ansible-playbook invocation for richer apps. Apps without an Update
	// command are skipped in the patch picker.
	Update string `yaml:"update,omitempty"`

	// UpdateCheck is an optional shell command run via SSH that prints
	// `UPDATES:<n>/<total>` (e.g. `UPDATES:2/3`) to stdout, indicating how
	// many of the app's components have updates waiting. Used to override
	// the built-in compose-image-digest check for apps that aren't
	// compose-based (apt packages, native binaries, etc.). If empty, murmur
	// runs a generic docker compose digest check against /opt/<name>/.
	UpdateCheck string `yaml:"update_check,omitempty"`

	// MatchAll: when true, patch/inspect fans out across every guest whose
	// name matches App.Name (rather than expecting exactly one). Used for
	// replicated services like twingate-connector that run one instance per
	// node. The Update command is run sequentially against each instance.
	MatchAll bool `yaml:"match_all,omitempty"`

	// Secrets declares per-replica secret values the operator must supply
	// at deploy time (Twingate access/refresh tokens, API keys, etc.).
	// During deploy the [a]apps tab prompts for each secret per target
	// guest; values are exported as env vars to post_deploy. Never logged.
	Secrets []AppSecret `yaml:"secrets,omitempty"`

	// Route is a reverse-proxy hint stamped verbatim into the guest's PVE
	// description at deploy time. An external sync tool (Traefik via a
	// description-scraping cron, Caddy, etc.) reads the description and
	// publishes the route — murmur only stamps the string, it never runs the
	// proxy or interprets the format. Common forms: `traefik-port:8080`
	// (subdomain derived from the guest name by the sync tool) or
	// `traefik:grafana:3000` (explicit subdomain). Empty ⇒ nothing stamped,
	// unless ReverseProxy.DescriptionTemplate is set as a cluster-wide
	// fallback. Applies to deploy only.
	Route string `yaml:"route,omitempty"`
}

// AppSecret is one per-replica secret prompted at deploy time. Name is the
// env-var that gets exported to post_deploy; Prompt is the operator-facing
// label (defaults to Name if empty).
type AppSecret struct {
	Name   string `yaml:"name"`
	Prompt string `yaml:"prompt,omitempty"`
}

type Cluster struct {
	Name    string  `yaml:"name"`
	Domain  string  `yaml:"domain"`
	API     API     `yaml:"api"`
	Nodes   []Node  `yaml:"nodes"`
	Storage Storage `yaml:"storage"`
	Network Network `yaml:"network"`
	SSH     SSH     `yaml:"ssh"`
}

type API struct {
	Endpoint      string `yaml:"endpoint"`
	TokenID       string `yaml:"token_id"`
	TokenSecret   string `yaml:"token_secret"`
	TLSSkipVerify bool   `yaml:"tls_skip_verify"`
}

type Node struct {
	Name    string   `yaml:"name"`
	Address string   `yaml:"address"`
	Roles   []string `yaml:"roles"`
}

type Storage struct {
	VMDisk string `yaml:"vm_disk"`
	Shared string `yaml:"shared"`
	ISO    string `yaml:"iso"`
}

type Network struct {
	DefaultBridge string `yaml:"default_bridge"`
	DefaultVLAN   *int   `yaml:"default_vlan,omitempty"`
}

type SSH struct {
	Identity string            `yaml:"identity"`
	Users    map[string]string `yaml:"users"`

	// Password is an optional cloud-init guest password. Either Identity (key
	// auth) or Password (password auth) — or both — must be set for VM
	// deploys. Reference cluster.env via ${VAR} for the actual secret so it
	// stays out of the committed cluster.yaml.
	Password string `yaml:"password,omitempty"`
}

type Flavor struct {
	Name     string `yaml:"name"`
	CPU      int    `yaml:"cpu"`
	MemoryMB int    `yaml:"memory_mb"`
	DiskGB   int    `yaml:"disk_gb"`
}

type Image struct {
	Name   string `yaml:"name"`
	Distro string `yaml:"distro"`
	URL    string `yaml:"url"`
}

// ReverseProxy is the cluster-wide fallback for the guest description, applied
// only to deploys whose App declares no explicit Route. DescriptionTemplate is
// stamped on each such guest with `{name}` (guest hostname) and `{app}` (apps:
// catalog name, empty for raw deploys) substituted. There is no `{port}`
// placeholder: murmur does not know the guest's listening port — it lives in
// the app's compose file on the guest, not in murmur's view — so express ports
// with a per-app `route:` instead. Omit the section to disable the fallback.
type ReverseProxy struct {
	DescriptionTemplate string `yaml:"description_template"`
}

type Monitoring struct {
	Grafana    string `yaml:"grafana"`
	Loki       string `yaml:"loki"`
	Prometheus string `yaml:"prometheus"`
}

type StorageBackend struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"` // smb | nfs
	Host     string `yaml:"host"`
	Share    string `yaml:"share"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
}
