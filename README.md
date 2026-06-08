<p align="center">
  <img src="docs/splash.gif" alt="murmur — animated splash" width="640">
</p>

<h1 align="center">murmur</h1>

<p align="center">A configurable TUI + CLI for managing a ProxMox cluster.</p>

---

**Status:** actively developed and usable. The cluster config schema can still
shift between releases — if you depend on it, pin a commit.

## Why

ProxMox cluster management tends to be either a web UI (great for one cluster,
painful for batch work) or hand-rolled bash + tofu + ansible (works once, drifts
forever). Murmur is a third option: describe your cluster in one YAML file and
get a typed Go API, a CLI, and a TUI that doesn't fight your terminal.

It talks to ProxMox over the native API and runs post-deploy steps over its own
SSH — no Terraform/OpenTofu, no Ansible required (though you can still call an
ansible playbook if you want one).

## Features

- **Declarative app catalog.** Define apps in `cluster.yaml`; deploy each as a
  VM or a lightweight LXC. One picker, confirm, go.
- **One-key lifecycle.** Separate TUI tabs for deploy, teardown, patch, and
  host upgrades — each backed by a single orchestrator entrypoint.
- **HA-aware teardown.** Removes a guest from HA before destroy, tolerates the
  pvestatd status cache lag, and polls through LXC cleanup.
- **Update detection.** An image-digest probe compares each guest's local
  RepoDigest against the registry and shows `UPDATES:n/total` in the patch tab.
- **Multi-user operators.** Named operators with role-scoped tabs/apps, backed
  by per-operator ProxMox tokens + pool/ACL bundles. Murmur is the ergonomic
  layer; ProxMox ACLs are the actual perimeter.
- **Turnkey operator kits.** Adding an operator exports a ready-to-run zip
  (binary + config + a standalone `cluster.env` + README) to hand off.
- **Secrets prompted at deploy.** Per-replica secrets are collected at deploy
  time, exported to post-deploy steps, and never logged.
- **Reverse-proxy hints.** Optionally stamp a route string on the guest's PVE
  description for an external sync (Traefik scraper, Caddy, …) to publish.
- **Event-driven TUI, no periodic refresh.** Redraws fire on user input or
  async-fetch-complete messages — never on a tick — so copy-paste just works.

## Quick start

```bash
go build -o murmur ./cmd/murmur

# Stage your config
mkdir -p ~/.config/murmur
cp configs/example.yaml ~/.config/murmur/cluster.yaml
cp configs/cluster.env.example ~/.config/murmur/cluster.env
# edit both with your cluster's values

./murmur validate    # load + validate the config (no network)
./murmur status      # connect, dump version + resource tally + storage check
./murmur tui         # interactive
```

Resolution order for `cluster.yaml`: `$MURMUR_CONFIG`, then `./cluster.yaml`,
then `~/.config/murmur/cluster.yaml`. The matching `cluster.env` (gitignored)
lives next to it.

## Configuration

The cluster config is the contract. See
[`configs/example.yaml`](configs/example.yaml) for the full schema with inline
docs.

Required sections:

- `cluster.api` — endpoint, token ID, token secret, TLS verify flag
- `cluster.nodes` — name + address + roles for each node
- `cluster.storage` — ProxMox storage IDs for VM disks, shared content, ISOs
- `cluster.network` — default bridge, optional default VLAN
- `cluster.ssh` — cloud-init default usernames per distro, identity path

Optional sections: `users`, `roles`, `flavors`, `images`, `apps`,
`reverse_proxy`, `monitoring`, `storage_backends`.

Secrets use `${VAR}` references resolved from a sidecar `cluster.env` file or
process env. Missing variables fail loudly with the field path that needed them.

### Two-file pattern

| File | Committable? | Contents |
|---|---|---|
| `cluster.yaml` | yes | the schema above, with `${VAR}` for anything secret |
| `cluster.env`  | no (gitignored) | `KEY=value` pairs, loaded before the YAML is parsed |

This keeps `cluster.yaml` identical across operators — only each operator's
`cluster.env` differs.

## Commands

| Command | What it does |
|---|---|
| `murmur validate` | Load + validate the cluster config. No network. |
| `murmur status` | Connect, print PVE version, resource tally by type, verify configured storage IDs exist. |
| `murmur whoami` | Print the resolved operator identity + role for this invocation. No network. |
| `murmur tui` | Launch the interactive TUI. |

Global flags: `--config PATH` (override config discovery), `--as NAME` (select
an operator from `users:`; or set `MURMUR_USER`).

### TUI

Tabs: **overview**, **apps**, **deploy**, **teardown**, **update**, **patch**,
and **users** (admin-only). Which tabs an operator sees is gated by their role.

Global keys: `r` refresh, `?` toggle help, `q` quit.

## Repo layout

```
cmd/murmur/         CLI entry point
internal/config/    Cluster config types, loader, validator, builtin catalogs
internal/proxmox/   ProxMox API client (typed, context-aware)
internal/provision/ App lifecycle orchestrator (deploy/teardown/upgrade/patch)
internal/tui/       Bubble Tea app, views, styles
configs/            example.yaml + cluster.env.example
```

Built on the [Charm](https://charm.sh) stack —
[Bubble Tea](https://github.com/charmbracelet/bubbletea),
[Lip Gloss](https://github.com/charmbracelet/lipgloss),
[Bubbles](https://github.com/charmbracelet/bubbles).

## Go version

1.25.5.

## License

[AGPL-3.0](LICENSE). The copyleft is intentional: improvements to murmur,
including those run as a network service, stay open.
