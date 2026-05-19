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
| Outputs | new packages under `internal/`, new fields in the cluster config Go types + matching `example.yaml` entry, new views under `internal/tui/` |
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

1. New file directly under `internal/tui/` (e.g. `internal/tui/myview.go`), implementing the `View` interface (`Init`, `Update`, `View`, `Title`, `Help`, `CapturesKeys`).
2. Data loading uses `tea.Cmd` returning a message — **never** a goroutine that calls back via channel into the model. Streaming progress (deploy / patch / host-update) uses a `chan tea.Msg` drained one-at-a-time via `readNext()`.
3. No `tea.Tick` — with the single sanctioned exception of the welcome splash view's intro animation, which stops on the first keypress. Steady-state views must remain tick-free so copy-paste works.
4. Register the view in `app.go`'s view registry (`a.views = append(a.views, ...)`) and add a top-bar tab + keybinding if user-reachable.
5. If the view captures text input (textinputs, secret prompts), set `CapturesKeys() bool` to return `true` in that state so the parent app stops intercepting global hotkeys (`q`/`?` etc.).

## Procedure: extend the ProxMox API client

1. Add the typed method to `internal/proxmox/` — signature returns `(T, error)`, not `interface{}`.
2. Return errors verbatim; do not wrap in "could not foo" prose. The TUI surfaces them.
3. If the endpoint mutates cluster state, prefix the method name with the verb (`CreateVM`, `DestroyLXC`, `MigrateContainer`).
4. Add a smoke test if the response shape is non-trivial — feed a recorded JSON fixture into the unmarshal path.

## App lifecycle (apps/deploy/teardown/update/patch tabs)

The app lifecycle lives in `internal/provision/` and is driven by entries in `cluster.yaml`'s `apps:` list. Each tab maps to one provision entrypoint:

| Tab          | TUI file              | Provision entrypoint                          | What it does |
|--------------|-----------------------|-----------------------------------------------|--------------|
| `[a]pps`     | `apps.go`             | `Orchestrator.Deploy(Request)`                | Per-app: pick → confirm → optional secrets prompt → provision → run post-deploy |
| `[d]eploy`   | `deploy.go`           | `Orchestrator.Deploy(Request)`                | Raw guest form (no app catalog) |
| `[t]eardown` | `teardown.go`         | `Orchestrator.Teardown(TeardownRequest)`      | Stop+destroy VM/LXC with purge |
| `[u]pdate`   | `update.go`           | `Orchestrator.UpgradeHost(node)`              | apt dist-upgrade on PVE nodes (host-side, via root SSH) |
| `[p]atch`    | `patch.go`            | `Orchestrator.PatchApp` / `PatchAppInstance`  | SSH to running guest, run app's `update:` command |

There's no `DeployApp` / `DestroyGuest` — apps.go builds a `provision.Request` from the picked `config.App` and the orchestrator handles VM vs LXC internally.

Key conventions:

- **`post_deploy:` runs ON the guest, `playbook:` runs LOCALLY.** When an app declares `post_deploy:`, the orchestrator sets `Request.PostDeployRemote=true` and the runner SSHes to the guest with `bash -s`, exporting GUEST_*/secret env vars via prepended `export K='V'` lines (single-quoted, `'\''`-escaped). Ansible playbook commands stay local since ansible owns its own SSH.
- **`$${VAR}` escape lets post_deploy / update reference runtime env.** The loader substitutes `${VAR}` from cluster.env at parse time. To pass through to the guest's shell unchanged (operator secrets, GUEST_IP, etc.), write `$${VAR}` — the loader emits one literal `$` and the remote shell does the expansion.
- **`match_all` semantics differ by tab.** On `[a]apps`: deploys ONE missing node per invocation so rollouts stay deliberate. On `[p]atch`: fans out across every matching guest, each as its own `PatchAppInstance` call so failures are independent. Picker UIs show `(N/M nodes deployed)` so progress is visible.
- **Secrets are prompted at deploy time, never logged.** `App.Secrets` lists per-replica env vars to collect (Twingate tokens etc.). The apps tab inserts an `appsSecrets` state between confirm and run; values are exported alongside GUEST_* to post_deploy and never appear in progress events (only key names do).
- **SSH user is live-discovered, not catalog-claimed.** `appSSHUser` queries the guest's `/qemu/{vmid}/agent/get-osinfo` and maps the reported distro ID to `cluster.ssh.users` (or to a builtin default). LXCs always log in as `root`. Don't trust `image.distro` — operators redeploy and drift.
- **`update:` is just a shell command over SSH.** Run with `sshStream`, which ratchets progress 20% → 95% as output streams in and emits raw lines without an `err:` prefix (docker/apt write legitimate output to stderr).
- **Inspect, don't pre-store.** The patch tab calls `InspectApp` in parallel on view-load to fetch live OS / IP / `docker compose ps` AND an update-availability probe. The probe tries `/opt/<app>/{compose.yml,docker-compose.yml,…}` first, falls back to `docker ps --format '{{.Image}}'`, then compares each image's local RepoDigest against the registry digest (`docker buildx imagetools inspect`, fallback `docker manifest inspect`). Output contract: `UPDATES:n/total`. Apps can override with `update_check:`.
- **LXC apps need nesting=1.** Orchestrator sets `Nesting: true` on every LXC create — required for docker-in-LXC. `KeyCtl` is NOT set: the PVE API rejects it for non-root tokens with HTTP 403 ("changing feature flags except nesting is only allowed for root@pam"). Apps that need keyctl have to flip it manually in the PVE UI.
- **Failure context is captured.** `runPostDeploy` keeps the last 12 stderr lines in a ring buffer and appends them to the returned error so TUI failures show docker / apt errors instead of just "exit 125".

### Procedure: add an app-lifecycle field

1. Add to `config.App` in `internal/config/types.go` with a yaml tag + a doc-comment that explains *when* the field applies (deploy? patch? both?).
2. Add to the `apps:` example in `configs/example.yaml` with a `#` comment.
3. Wire it through the relevant `Orchestrator` method(s) in `internal/provision/` and `provision.Request` if it must reach the orchestrator.
4. Surface it in the matching TUI tab (`internal/tui/{apps,patch,...}.go`) if the operator should see/select it.

### Current App schema (cluster.yaml `apps:` entries)

| Field          | Required | Applies to        | Notes |
|----------------|----------|-------------------|-------|
| `name`         | yes      | all               | guest hostname; unique within `apps:` |
| `type`         | no       | deploy            | `vm` (default) or `lxc`. LXCs auto-enable nesting. |
| `image`        | yes      | deploy            | references `images:` or a builtin (debian-13, ubuntu-24.04, …) |
| `flavor`       | yes      | deploy            | references `flavors:` or a builtin (1vcpu.512mb / 1vcpu.1gb / 1vcpu.2gb / 2vcpu.4gb / 4vcpu.8gb) |
| `playbook`     | xor with `post_deploy` | deploy | ansible playbook path, run locally with `ansible-playbook -i ${GUEST_IP},` |
| `post_deploy`  | xor with `playbook`    | deploy | raw shell run ON the guest via SSH `bash -s`, GUEST_*/secret env exported |
| `update`       | no       | patch             | shell command run via SSH for `[p]atch`. Apps without this are filtered out of the patch picker. |
| `update_check` | no       | patch             | optional override for the built-in image-digest probe; must print `UPDATES:n/total` |
| `match_all`    | no       | deploy + patch    | true ⇒ one guest per cluster node. Deploy queues one missing node per invocation; patch fans out. |
| `secrets`      | no       | deploy            | list of `{name, prompt}` — prompted at deploy time, exported as env to `post_deploy` |

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
- **`${VAR}` vs `$${VAR}` matters.** Single `$` is loader-substituted at parse time. Double `$$` is the escape that survives to the rendered shell. Use single `$` for cluster-wide values from cluster.env (e.g. `${TWINGATE_NETWORK}`); use `$$` for per-replica secrets or runtime vars that the guest's shell will expand (e.g. `$${TWINGATE_ACCESS_TOKEN}`).
- **`post_deploy:` body runs on the guest, not on the murmur host.** Write the commands as if you're SSHed in — no need to wrap with `ssh user@$GUEST_IP '…'`, that wrapping happens for you. If you instead want to run something locally (e.g. `scp` from the murmur host), use `playbook:` or call the local tool that handles its own SSH.
- **Don't use `--sysctl` for non-namespaced kernel settings inside an LXC.** Things like `net.ipv4.ping_group_range` are host-global; docker inside an unprivileged LXC can't write them and runc refuses to start the container with an OCI error. Set them on the PVE host (or via the LXC's raw lxc.sysctl) and inherit. Twingate's default `docker run` snippet hits this — drop the `--sysctl` line when adapting it.
- **Bubble Tea + alt-screen + copy-paste**: alt-screen mode trashes the scrollback. Default the program to non-alt-screen; require a flag to enable it.
- **The cluster.yaml `nodes:` list is the source of truth for match_all and best-fit placement.** Murmur does NOT auto-discover Proxmox cluster members. Operators must list every node they want murmur to target, with `address:` (SSH-reachable IP) and `roles:`. If a node is missing from `nodes:`, match_all skips it silently.
