package config

// Builtin flavor catalog. User config extends; same-name entries override.
var builtinFlavors = []Flavor{
	{Name: "1vcpu.512mb", CPU: 1, MemoryMB: 512, DiskGB: 4},
	{Name: "1vcpu.1gb", CPU: 1, MemoryMB: 1024, DiskGB: 8},
	{Name: "1vcpu.2gb", CPU: 1, MemoryMB: 2048, DiskGB: 16},
	{Name: "2vcpu.4gb", CPU: 2, MemoryMB: 4096, DiskGB: 32},
	{Name: "4vcpu.8gb", CPU: 4, MemoryMB: 8192, DiskGB: 64},
}

// Builtin image catalog. URLs intentionally point at "latest" channels;
// pin to a digest in your config if reproducibility matters.
var builtinImages = []Image{
	{Name: "debian-13", Distro: "debian", URL: "https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.qcow2"},
	{Name: "debian-12", Distro: "debian", URL: "https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-amd64.qcow2"},
	{Name: "ubuntu-26.04", Distro: "ubuntu", URL: "https://cloud-images.ubuntu.com/resolute/current/resolute-server-cloudimg-amd64.img"},
	{Name: "ubuntu-24.04", Distro: "ubuntu", URL: "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"},
	{Name: "ubuntu-22.04", Distro: "ubuntu", URL: "https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img"},
	{Name: "fedora-43", Distro: "fedora", URL: "https://download.fedoraproject.org/pub/fedora/linux/releases/43/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-43-1.6.x86_64.qcow2"},
	// Alpine has two parallel image families: `generic_*-cloudinit-*` is
	// historically misnamed and actually ships tiny-cloud (a minimal Alpine
	// bootstrap that reads metadata but does NOT execute cloud-config
	// directives — so our agent-install snippet is silently ignored). The
	// `nocloud_*-cloudinit-*` variant ships the real cloud-init from Pypi
	// and DOES process bootcmd/runcmd/packages/apk_repos. PVE's cidata drive
	// is exactly the NoCloud datasource, so this variant Just Works.
	//
	// Version chosen to match PVE's pveam appliance repo so LXC deploys hit
	// an exact-name match instead of needing the fuzzy-fallback path.
	{Name: "alpine-3.23", Distro: "alpine", URL: "https://dl-cdn.alpinelinux.org/alpine/v3.23/releases/cloud/nocloud_alpine-3.23.4-x86_64-bios-cloudinit-r0.qcow2"},
}

// Builtin distro → cloud-init default user.
var builtinSSHUsers = map[string]string{
	"debian": "debian",
	"ubuntu": "ubuntu",
	"fedora": "fedora",
	"alpine": "alpine",
	"rocky":  "rocky",
	"arch":   "arch",
}

func mergeFlavors(user []Flavor) []Flavor {
	return mergeNamed(builtinFlavors, user, func(f Flavor) string { return f.Name })
}

func mergeImages(user []Image) []Image {
	return mergeNamed(builtinImages, user, func(i Image) string { return i.Name })
}

func mergeSSHUsers(user map[string]string) map[string]string {
	out := make(map[string]string, len(builtinSSHUsers)+len(user))
	for k, v := range builtinSSHUsers {
		out[k] = v
	}
	for k, v := range user {
		out[k] = v
	}
	return out
}

// mergeNamed merges two slices keyed by name; user entries override builtins.
func mergeNamed[T any](builtin, user []T, key func(T) string) []T {
	idx := make(map[string]int, len(builtin))
	out := make([]T, len(builtin))
	for i, b := range builtin {
		out[i] = b
		idx[key(b)] = i
	}
	for _, u := range user {
		if i, ok := idx[key(u)]; ok {
			out[i] = u
		} else {
			idx[key(u)] = len(out)
			out = append(out, u)
		}
	}
	return out
}
