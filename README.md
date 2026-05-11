# murmur

A configurable TUI + CLI for managing a ProxMox cluster.

**Status:** pre-v0.1. Private until the shape settles. APIs and config format may break without notice.

## Why

ProxMox cluster management tools tend to be either web UIs (great for one cluster, painful for batch work) or hand-rolled bash + tofu + ansible (works once, drifts forever). Murmur is a third option: define your cluster in YAML, get a typed Go API, a CLI, and a TUI that doesn't fight your terminal.

## Design principles

- **Configurable, not hardcoded.** One `cluster.yaml` describes the API endpoint, nodes, storage IDs, network bridge, monitoring URLs. No cluster-specific names, IPs, or domains in the binary.
- **Event-driven TUI, no periodic refresh.** Redraws happen on user input or async-fetch-complete messages — never on a `tea.Tick`. Text selection works in the alt-screen because there's nothing to interrupt it.
- **Loud failures.** Validate at load time. When the config references a storage ID, verify it actually exists on the cluster. Surface errors with the field path.
- **Charm stack.** [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Lip Gloss](https://github.com/charmbracelet/lipgloss), [Bubbles](https://github.com/charmbracelet/bubbles).

## Quick start

```bash
go build -o murmur ./cmd/murmur

# Stage your config
mkdir -p ~/.config/murmur
cp configs/example.yaml ~/.config/murmur/cluster.yaml
cp configs/cluster.env.example ~/.config/murmur/cluster.env
# edit both with your cluster's values

./murmur validate    # syntactic check
./murmur status      # connect, dump version + resource tally + storage check
./murmur tui         # interactive
```

Resolution order for `cluster.yaml`: `$MURMUR_CONFIG`, then `./cluster.yaml`, then `~/.config/murmur/cluster.yaml`. The matching `cluster.env` (gitignored) lives next to it.

## Configuration

The cluster config is the contract. See [`configs/example.yaml`](configs/example.yaml) for the full schema with inline docs.

Required sections:

- `cluster.api` — endpoint, token ID, token secret, TLS verify flag
- `cluster.nodes` — name + address + roles for each node
- `cluster.storage` — ProxMox storage IDs used for VM disks, shared content, ISOs
- `cluster.network` — default bridge, optional default VLAN
- `cluster.ssh` — cloud-init default usernames per distro, identity path

Optional sections: `flavors`, `images`, `reverse_proxy`, `monitoring`, `storage_backends`.

Secrets use `${VAR}` references resolved from a sidecar `cluster.env` file or process env. Missing variables fail loudly with the field path that needed them.

## Commands

| Command | What it does |
|---|---|
| `murmur validate` | Load + syntactic-validate the cluster config. |
| `murmur status` | Connect to the cluster, print PVE version, resource tally by type, verify configured storage IDs exist. |
| `murmur tui` | Launch the interactive TUI. |

TUI keys: `r` refresh, `?` toggle help, `q` quit.

## v0.1 scope

Done:
- Cluster config schema (YAML + sidecar env)
- Config loader with env-var substitution against the YAML AST (comments untouched)
- Builtin flavor / image / SSH-user catalogs that user config extends or overrides
- ProxMox API client (read-only): `/version`, `/cluster/resources`, `/storage`, `/nodes`
- Typed `APIError` with status + body
- Loud-fail storage validation against the live cluster
- Event-driven TUI with one overview view

Not yet:
- Mutating API endpoints (create / destroy / migrate VMs and LXCs)
- Cluster mutation orchestration (the tofu + ansible pipeline)
- App / container updates with diff-then-apply
- Multiple TUI views (single overview view today)

## Repo layout

```
cmd/murmur/        CLI entry point
internal/config/   Cluster config types, loader, validator, builtin catalogs
internal/proxmox/  ProxMox API client (typed, context-aware, read-only for now)
internal/tui/      Bubble Tea app, views, styles
configs/           example.yaml + cluster.env.example
.claude/skills/    Development skill (murmur)
```

## Go version

1.25.5. Bump in lockstep across the repo.

## License

TBD — added before the repo goes public.
