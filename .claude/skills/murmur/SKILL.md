---
name: murmur
description: Develop the murmur CLI/TUI — extend cluster config schema, ProxMox client, or TUI views. Inputs `cmd/murmur/`, `internal/{config,proxmox,tui}/`, `configs/example.yaml`, `go.mod`. Outputs new packages/fields/views in-repo. Does NOT cover *using* murmur to manage a live cluster (that's the consumer-repo's job, e.g. `/lightlab`). Pre-v0.1 — schema and architecture still moving.
---

# The Murmur Skill

Develop the murmur codebase: extend the cluster config schema, the ProxMox API client, or the Bubble Tea TUI. Murmur is a configurable, generic ProxMox cluster manager — no cluster-specific facts (node names, IPs, domains) belong in code or fixtures.

## I/O contract

| | |
|---|---|
| Inputs | `cmd/murmur/`, `internal/config/`, `internal/proxmox/`, `internal/tui/`, `configs/example.yaml`, `go.mod` |
| Outputs | new packages under `internal/`, new fields in the cluster config Go types + matching `example.yaml` entry, new views under `internal/tui/views/` |
| Manifest | `configs/example.yaml` is the canonical reference for the config surface. Every schema field must appear there with a comment. |
| Upstream | None — murmur is the root of its own dependency graph. Reference TheLightLab/internal/* as a "we know this shape works" example, but never copy verbatim. |
| Downstream | Consumer repos (e.g. TheLightLab) provide a `cluster.yaml` and run the binary. |
| Narrow exceptions | None yet. |

## Determining current state

This skill stores no state. To read it:

1. **Built binary works:** `cd ~/murmur && go build ./... && ./murmur --help`
2. **Config surface:** `cat configs/example.yaml`
3. **Go module health:** `go vet ./... && go test ./...`
4. **Open tasks:** check `TaskList` in the parent project where the work was scoped.

## Build + copy workflow (mandatory after every code change)

After **every** Go change under `~/murmur/`, run:

```bash
cd ~/murmur && go build -o murmur ./cmd/murmur && cp murmur ~/TheLightLab/murmur
```

User runs murmur from `~/TheLightLab/` as `./murmur tui` against the real cluster. Skipping the copy means the user runs stale code. The copy is part of "done", not a follow-up step.

If `cp` fails with `Text file busy`, the user has the TUI running — ask them to quit (`q`) before retrying. Don't kill the process for them.

Pure docs/yaml/memory edits don't need the build+copy.

## Architectural principles (load-bearing)

- **Configurable, not hardcoded.** No `prxy-*`, `192.168.x.x`, `example.net`, or any other cluster-specific token in non-fixture code. If a value differs between clusters, it goes in `cluster.yaml`.
- **Event-driven TUI, no periodic refresh.** Redraws fire on user input or async-fetch-complete messages — never on a `tea.Tick`. Copy-paste must work in a normal terminal.
- **Alt-screen is opt-in, off by default.** Inline output is the default for TUI views; alt-screen is a flag for sustained interactive sessions only.
- **Explicit updates.** No "auto-detected, mysteriously failed to apply." Show diffs, show apply output, fail loudly with the underlying error.

## Procedure: add a field to cluster config

1. Add to the Go type in `internal/config/` with a `yaml:"..."` tag.
2. Add a validation case in the config Validate path (required fields, ranges, env-var resolution).
3. Add the field to `configs/example.yaml` with a one-line comment explaining when it applies.
4. If the field has a non-trivial default, document it in the comment.
5. `go build ./... && go vet ./...` before commit.

## Procedure: add a TUI view

1. New file under `internal/tui/views/`, implementing the `View` interface (`Init`, `Update`, `View`, `Title`, `ShortHelp`, `FullHelp`).
2. Data loading uses `tea.Cmd` returning a message — **never** a goroutine that calls back via channel into the model.
3. No `tea.Tick` — with the single sanctioned exception of the welcome splash view's intro animation, which stops on the first keypress. Steady-state views must remain tick-free so copy-paste works.
4. Register the view in the app's view registry and add a navigation entry if user-reachable.

## Procedure: extend the ProxMox API client

1. Add the typed method to `internal/proxmox/` — signature returns `(T, error)`, not `interface{}`.
2. Return errors verbatim; do not wrap in "could not foo" prose. The TUI surfaces them.
3. If the endpoint mutates cluster state, prefix the method name with the verb (`CreateVM`, `DestroyLXC`, `MigrateContainer`).
4. Add a smoke test if the response shape is non-trivial — feed a recorded JSON fixture into the unmarshal path.

## App lifecycle (apps/deploy/teardown/update/patch tabs)

The app lifecycle lives in `internal/provision/` and is driven by entries in `cluster.yaml`'s `apps:` list. Each tab maps to one provision entrypoint:

| Tab        | TUI file              | Provision entrypoint                          | What it does |
|------------|-----------------------|-----------------------------------------------|--------------|
| `[a]pps`   | `apps.go`             | `Orchestrator.DeployApp`                      | Clone image → cloud-init → run `playbook:` or `post_deploy:` |
| `[d]eploy` | `deploy.go`           | `Orchestrator.DeployApp`                      | Multi-select bulk deploy |
| `[t]eardown` | `teardown.go`       | `Orchestrator.DestroyGuest`                   | Stop+delete VM/LXC |
| `[u]pdate` | `appupdate.go` (host) | `Orchestrator.UpgradeHost`                    | apt dist-upgrade on PVE nodes |
| `[p]atch`  | `patch.go`            | `Orchestrator.PatchApp` / `PatchAppInstance`  | SSH to running guest, run app's `update:` command |

Key conventions:

- **SSH user is live-discovered, not catalog-claimed.** `appSSHUser` queries the guest's `/qemu/{vmid}/agent/get-osinfo` and maps the reported distro ID to `cluster.ssh.users` (or to a builtin default). Don't trust `image.distro` — operators redeploy and drift.
- **`match_all: true` fans out per replica.** The patch picker emits one row per matching guest; each row dispatches its own `PatchAppInstance(vmid, node)` call so successes/failures are independent.
- **`update:` is just a shell command over SSH.** It's run with `sshStream` (the same streamer the host upgrade uses) so progress flows back as `StepConfigure` events.
- **Inspect, don't pre-store.** The patch tab calls `InspectApp` in parallel on view-load to fetch live OS / IP / `docker compose ps`. No background poller, no cache — just one fan-out per refresh.

### Procedure: add an app-lifecycle field

1. Add to `config.App` in `internal/config/types.go` with a yaml tag + a doc-comment that explains *when* the field applies (deploy? patch? both?).
2. Add to the `apps:` example in `configs/example.yaml` with a `#` comment.
3. Wire it through the relevant `Orchestrator` method(s) in `internal/provision/`.
4. Surface it in the matching TUI tab (`internal/tui/{apps,patch,...}.go`) if the operator should see/select it.

## Procedure: bump Go dependencies

```bash
go get -u ./... && go mod tidy && go build ./... && go test ./...
```

If a major version bump is required, update one module at a time with a focused commit per bump.

## Git

- **No AI attribution in commits or pushes.** No `Co-Authored-By: Claude…`, no "Generated with Claude Code" footers, no AI mention anywhere in commit messages, PR bodies, or push artifacts. (See CLAUDE.md.)
- Prefer new commits over amending published ones.

## Files

- `cmd/murmur/main.go` — entry point
- `internal/config/` — cluster config Go types, YAML loader, validator
- `internal/proxmox/` — typed ProxMox API client
- `internal/provision/` — app lifecycle orchestrator (deploy/teardown/host-upgrade/patch)
- `internal/tui/` — Bubble Tea app, components, views
- `configs/example.yaml` — canonical reference for the config surface
- `go.mod` / `go.sum` — module + pinned deps

## Gotchas

- **Don't import from TheLightLab.** Reference its code by reading it; never depend on it. Murmur must build standalone.
- **Two-file config pattern.** `cluster.yaml` is committable and uses `${VAR}`; `cluster.env` is gitignored KEY=value pairs loaded before YAML parse. Resolution happens at load — missing vars fail loudly with a pointer to the field that needed them.
- **Bubble Tea + alt-screen + copy-paste**: alt-screen mode trashes the scrollback. Default the program to non-alt-screen; require a flag to enable it.
