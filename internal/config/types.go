package config

type Config struct {
	Cluster         Cluster          `yaml:"cluster"`
	Flavors         []Flavor         `yaml:"flavors"`
	Images          []Image          `yaml:"images"`
	Apps            []App            `yaml:"apps,omitempty"`
	ReverseProxy    *ReverseProxy    `yaml:"reverse_proxy,omitempty"`
	Monitoring      *Monitoring      `yaml:"monitoring,omitempty"`
	StorageBackends []StorageBackend `yaml:"storage_backends"`
}

// App is a declarative VM-plus-configuration unit. Pick an app in the [a]apps
// tab and murmur provisions a guest matching Image/Flavor, then runs the
// post-deploy step (a Playbook path or a raw PostDeploy shell command).
//
// Exactly one of Playbook or PostDeploy may be set, both can be empty
// (provision-only, no configuration). Validation enforces this.
type App struct {
	Name       string `yaml:"name"`
	Image      string `yaml:"image"`
	Flavor     string `yaml:"flavor"`
	Playbook   string `yaml:"playbook,omitempty"`    // ansible playbook path (relative to config dir)
	PostDeploy string `yaml:"post_deploy,omitempty"` // raw shell command (alternative to Playbook)
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
