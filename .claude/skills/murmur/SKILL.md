---
name: murmur
description: Develop murmur — extend the cluster config schema, ProxMox client, MCP tool surface, or TUI views. Inputs `cmd/murmur/`, `internal/{config,proxmox,provision,mcpserver,tui}/`, `configs/example.yaml`, `go.mod`. Outputs new packages/fields/tools/views in-repo. Does NOT cover *using* murmur to manage a live cluster (that's the consumer-repo's job, e.g. `/lightlab`). Pre-v0.1 — schema and architecture still moving.
---

# The Murmur Skill

Develop the murmur codebase: extend the cluster config schema, the ProxMox API client, or the Bubble Tea TUI. Murmur is a configurable, generic ProxMox cluster manager — no cluster-specific facts (node names, IPs, domains) belong in code or fixtures.

## I/O contract

| | |
|---|---|
| Inputs | `cmd/murmur/`, `internal/config/`, `internal/proxmox/`, `internal/provision/`, `internal/mcpserver/`, `internal/tui/`, `configs/example.yaml`, `go.mod` |
| Outputs | new packages under `internal/`, new fields in the cluster config Go types + matching `example.yaml` entry, new MCP tools under `internal/mcpserver/`, new views under `internal/tui/` |
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

## Build workflow (mandatory after every code change)

After **every** Go change under `~/murmur/`, run:

```bash
cd ~/murmur && go build -o murmur ./cmd/murmur && go vet ./...
```

Consumers run the binary from here (e.g. an MCP client configured with `~/murmur/murmur --config <cluster.yaml> mcp`) — there is no copy step. If an MCP client holds the binary open and the build fails with `Text file busy`, build to a temp name and `mv` over it.

Pure docs/yaml/memory edits don't need the build.

## Architectural principles (load-bearing)

- **Configurable, not hardcoded.** No *real* node hostnames, IP literals, or domains anywhere in code or fixtures — `example.yaml` and code comments use generic placeholders only (`example` / `example.net`, `10.0.0.x`, `pve1`/`pve2`/`pve3`). Real cluster-specific values live in the consumer's `cluster.yaml`, never here. (This is a release boundary, not just style — keep it clean so the repo can ship without leaking a cluster.)
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

## App lifecycle (apps/deploy/teardown/update/patch/users tabs)

The app lifecycle lives in `internal/provision/` and is driven by entries in `cluster.yaml`'s `apps:` list. Each tab maps to one provision entrypoint:

| Tab          | TUI file              | Provision entrypoint                          | What it does |
|--------------|-----------------------|-----------------------------------------------|--------------|
| `[a]pps`     | `apps.go`             | `Orchestrator.Deploy(Request)`                | Per-app: pick → confirm → optional secrets prompt → provision → run post-deploy |
| `[d]eploy`   | `deploy.go`           | `Orchestrator.Deploy(Request)`                | Raw guest form (no app catalog) |
| `[t]eardown` | `teardown.go`         | `Orchestrator.Teardown(TeardownRequest)`      | Remove from HA (if managed) → stop → destroy+purge; resilient to stale status + LXC cleanup lag |
| `[u]pdate`   | `update.go`           | `Orchestrator.UpgradeHost(node)`              | apt dist-upgrade on PVE nodes (host-side, via root SSH) |
| `[p]atch`    | `patch.go`            | `Orchestrator.PatchApp` / `PatchAppInstance`  | SSH to running guest, run app's `update:` command |
| `[x]users`   | `users.go`            | `proxmox.{Create,Delete}User/Pool/Token/ACL`  | Admin-only — add/delete/rotate-token/suspend operators; auto-exports starter folder |

There's no `DeployApp` / `DestroyGuest` — apps.go builds a `provision.Request` from the picked `config.App` and the orchestrator handles VM vs LXC internally.

The top tab bar uses compressed "browser-tab" rendering: inactive tabs show `<key> <abbrev>` (first 3 chars of label), active expands to `<key> <full label>` with a heavy `━━━` underline below. `[x]users` uses capital-letter key namespace so it doesn't collide with `[u]pdate`; arrow-keys + Enter drive its submenu since letter hotkeys would shadow tab-switch keys.

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

## Multi-user identity + access control

Murmur supports multiple named operators with role-scoped permissions. Both the murmur side (TUI tab/list filtering) and the ProxMox side (per-user tokens with bundled ACLs) cooperate — murmur is the ergonomic layer, ProxMox ACLs are the actual perimeter.

### Schema additions (cluster.yaml)

```yaml
users:
  - name: alice                          # short identity used by --as / MURMUR_USER
    role: deployer                       # must match a name in roles: (or a builtin)
    proxmox_user: alice@pve              # backing PVE user
    proxmox_token: murmur                # token name (full id assembled as "alice@pve!murmur")
    token_secret: ${ALICE_TOKEN}         # resolved LAZILY at identity-selection time
    comment: Alice — application deployer

roles:                                   # extends 2 builtins (admin/deployer)
  - name: lab-only-deployer
    tabs:    [overview, apps, deploy, patch]
    actions: [deploy, patch]
    apps:    [edge-connector, twingate-connector]
    guests:  own                         # "own" → list views filter by murmur-owner tag
```

Both sections are optional. If both empty, murmur uses `cluster.api.token_*` as an implicit-admin fallback (v0.1 behavior).

### Identity selection (`cmd/murmur/main.go` + `internal/config/identity.go`)

`config.ResolveActive(asFlag)` returns `*ActiveUser`. Resolution order:
1. `--as <name>` CLI flag
2. `$MURMUR_USER` env var
3. unambiguous single-entry default (one user in `users:`)
4. credential inference: the one operator whose `token_secret` resolves in the
   environment (each operator's `cluster.env` carries only their own token, so a
   single match is an unambiguous identity). >1 match → ambiguous error.
5. loud error listing valid names

The active user's `${VAR}` token secret is expanded against `os.Getenv` at this point. Missing env vars fail loud with the var name — but ONLY the active user's secret is checked. Other operators' tokens stay unresolved so each operator's `cluster.env` only contains their own secret.

`whoami` CLI prints the resolved identity + role for sanity-checking without hitting the API.

### Lazy token resolution (`internal/config/load.go` `stashUserTokenSecrets`)

The loader's `substituteScalars` would fail loudly on undefined env vars, but that would force every operator's `cluster.env` to declare every other operator's `${TOKEN_VAR}`. Workaround: before `substituteScalars` runs, walk the AST, stash `users[*].token_secret` raw values into a map keyed by name, replace the AST scalars with empty strings so substitution skips them. After decode, restore the raw values onto `cfg.Users`. Resolution happens later in `ResolveActive` for the active user only.

### Pool + owner-tag stamping (`internal/provision/orchestrator.go`)

`Orchestrator.SetActiveUser(a)` installs the operator identity on the orchestrator. When non-nil and not a fallback, every deploy:
- stamps `murmur-owner-<name>` + `murmur-app-<app>` tags (VM via `ConfigureVMHardware.Tags`; LXC via `CreateLXCRequest.Tags`). Delimiter is `-`, not `=`: PVE rejects `=` (and most punctuation) in tags.
- assigns the new guest to pool `murmur-<name>` (VM via `CloneVMRequest.Pool`; LXC via `CreateLXCRequest.Pool`)
- auto-creates the pool if missing (defensive — `[U]sers` add flow normally creates it, this is the orchestrator's safety net)

ProxMox ACLs scoped to `/pool/murmur-<name>` enforce that non-admin deployers literally cannot touch other operators' guests via raw `pvesh`. Tags carry murmur-side metadata for the apps-tab catalog matching.

**Reverse-proxy description** is stamped on the same deploy path but is *not* owner-gated — `guestDescription(req)` (right beside `ownerTagSet`) runs for every deploy. Precedence: the per-app `route:` wins and is stamped **verbatim**; else the cluster-wide `reverse_proxy.description_template` with `{name}`/`{app}` substituted; else empty (no description). VM via `ConfigureVMHardware.Description`, LXC via `CreateLXCRequest.Description`. There is **no `{port}` placeholder** — murmur can't see the guest's listening port (it lives in the on-guest compose file), so ports go in a per-app `route:`. An external sync (Traefik scraper, Caddy, …) reads the description and publishes the route; murmur only stamps the string, never runs the proxy. Stamping happens at create time only — murmur does not reconcile descriptions on existing guests.

### Role-based TUI filtering (`internal/tui/access.go`)

- `ownerFilter(active, resources)` — filters resource lists by the `murmur-owner-<name>` tag when `role.guests == "own"`; admin's `guests: all` passes through. Wired into the VMs/LXCs, apps, teardown and patch views.
- `appAllowed(active, appName)` — `role.apps` gate, supports `*` wildcard. Filters the apps-tab catalog (`AppsView.catalog`) and the patch catalog, so a scoped operator only sees/deploys/patches their allowed apps. (`actionAllowed` was removed — `role.actions` is informational; enforcement is by tabs + appAllowed + the PVE ACLs.)
- `App.tabAllowed(name)` — `role.tabs` gate; disallowed tabs are **hidden** from the top bar (not greyed). Pressing a hidden tab's hotkey still raises a one-shot vermilion footer toast ("permission denied…"), no `tea.Tick`.

`InspectApp` accepts the orchestrator's active user implicitly and filters replicas by owner when `role.guests == "own"` so the `[p]atch` tab's match_all counts reflect only the operator's deployments.

### [U]sers tab (admin-only)

Tab key `x` (capital U was unergonomic). Admin-only: greyed in the tab bar for any role whose `tabs:` doesn't include `users`. List shows ONLY cluster.yaml-managed users — orphan PVE accounts (`root@pam`, `traefik-sync@pve`, etc.) are hidden because murmur isn't trying to be a generic PVE user manager.

Navigation: arrow-keys + Enter only (no letter hotkeys — they'd collide with the tab bar's a/d/t/u/p/x). Row 0 is a virtual `+ add new user` row; rows 1..N are users. Enter on +add opens the form; Enter on a user opens an action submenu (rotate / suspend / delete / close) where ↑↓ picks and Enter activates.

Auto-export on add (`internal/config/export.go`): after the PVE-side bundle succeeds and `cluster.yaml` is updated, murmur writes a sibling folder `<dirname>-<opname>/` containing a verbatim cluster.yaml copy, a minimal cluster.env (operator's identity + token + placeholder hints for other vars admin's env declared — never echoing admin's values), and a README. Refuses if the folder exists. Path surfaces in the secret modal.

Mutation guards:
- **Roles are immutable.** No `[e]dit` action; the only mutation paths on existing users are rotate token / suspend / delete. Role change = delete + re-add.
- **No self-delete for admin.** The action is visible in the submenu but greyed.
- **Safe-delete pre-check.** Before showing the confirm prompt, an async query counts guests tagged `murmur-owner-<name>`. Running > 0 → refuse outright. Running = 0, stopped > 0 → proceed with augmented blurb naming the orphan-guest count.
- **Type-the-name confirm** for both delete and rotate.

Secret modal: shown once. Single-line copy-friendly format. `[c]` tries xclip / wl-copy / pbcopy in order; `[y]` clears the secret from memory and closes.

### ACL bundles per role (`internal/tui/users.go` `aclBundleFor`)

Add-user flow applies a hard-coded PVE ACL bundle per role:

| Role | ACL set |
|---|---|
| `admin`    | `Administrator` on `/` |
| `deployer` | `PVEVMAdmin` + `PVEPoolAdmin` on `/pool/murmur-<name>`, `PVETemplateUser` on `/pool/murmur-templates`, `PVEDatastoreUser` on `/storage`, `PVESDNUser` on `/sdn`, `PVEAuditor` on `/` |

Two PVE gotchas drive the deployer bundle:
- **"Deeper level replaces inherited."** `PVEVMAdmin` on `/pool/murmur-<name>` shadows the `PVEAuditor` inherited from `/` on that path, so `Pool.Audit`/`Pool.Allocate` must be re-granted there explicitly (`PVEPoolAdmin`) — otherwise the operator can't see or allocate into their own pool.
- **Clone needs the source.** Templates live in the shared `murmur-templates` pool (added at build time); `PVETemplateUser` there is what lets a deployer clone them without any access to other operators' guests.

Only `admin` and `deployer` are builtin. User-defined roles in `cluster.yaml` are valid for murmur-side gating but the `[a]dd` form only configures ACLs for these two. Custom-role users need manual ACL setup via the PVE web UI.

### Procedure: extend the users/roles surface

1. Add to `User` or `Role` in `internal/config/types.go` with a `yaml:"..."` tag + doc comment.
2. Add validation in `internal/config/validate.go` (required fields, enum constraints).
3. If the new field is a secret/identity that should be lazy-resolved per operator, extend `stashUserTokenSecrets` in `load.go` and the restore path in `LoadFile`.
4. Surface it in the `[U]sers` tab's add form (`internal/tui/users.go`) if operator-editable.
5. Add the field to `configs/example.yaml` users:/roles: block with a `#` comment.

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
| `route`        | no       | deploy            | reverse-proxy hint stamped **verbatim** on the guest's PVE description; an external sync (Traefik scraper, Caddy, …) reads it. Murmur doesn't run the proxy or parse the format. Falls back to cluster-wide `reverse_proxy.description_template` ({name}/{app} subst, no {port}) when empty. |

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
- `internal/mcpserver/` — MCP tool surface for AI operators (`murmur mcp`): read tools for every role, role-gated + JSONL-audited mutating tools, no destructive tools without explicit-confirmation semantics. New tools follow the same split — register in `server.go`, handlers in `tools.go`, policy/audit in `rails.go`.
- `internal/tui/` — Bubble Tea app, components, views
- `configs/example.yaml` — canonical reference for the config surface
- `docs/` — README assets (the `demo.gif` splash + TUI walkthrough); not part of the build
- `README.md` — public-facing; Quickstart → Why → Features → Configuration
- `go.mod` / `go.sum` — module + pinned deps

## Gotchas

- **Don't import from TheLightLab.** Reference its code by reading it; never depend on it. Murmur must build standalone.
- **Two-file config pattern.** `cluster.yaml` is committable and uses `${VAR}`; `cluster.env` is gitignored KEY=value pairs loaded before YAML parse. Resolution happens at load — missing vars fail loudly with a pointer to the field that needed them.
- **`${VAR}` vs `$${VAR}` matters.** Single `$` is loader-substituted at parse time. Double `$$` is the escape that survives to the rendered shell. Use single `$` for cluster-wide values from cluster.env (e.g. `${TWINGATE_NETWORK}`); use `$$` for per-replica secrets or runtime vars that the guest's shell will expand (e.g. `$${TWINGATE_ACCESS_TOKEN}`).
- **`post_deploy:` body runs on the guest, not on the murmur host.** Write the commands as if you're SSHed in — no need to wrap with `ssh user@$GUEST_IP '…'`, that wrapping happens for you. If you instead want to run something locally (e.g. `scp` from the murmur host), use `playbook:` or call the local tool that handles its own SSH.
- **Don't use `--sysctl` for non-namespaced kernel settings inside an LXC.** Things like `net.ipv4.ping_group_range` are host-global; docker inside an unprivileged LXC can't write them and runc refuses to start the container with an OCI error. Set them on the PVE host (or via the LXC's raw lxc.sysctl) and inherit. Twingate's default `docker run` snippet hits this — drop the `--sysctl` line when adapting it.
- **Bubble Tea + alt-screen + copy-paste**: alt-screen mode trashes the scrollback. Default the program to non-alt-screen; require a flag to enable it.
- **The cluster.yaml `nodes:` list is the source of truth for match_all and best-fit placement.** Murmur does NOT auto-discover Proxmox cluster members. Operators must list every node they want murmur to target, with `address:` (SSH-reachable IP) and `roles:`. If a node is missing from `nodes:`, match_all skips it silently.
- **Env loader supports inline `#` comments** — but only when whitespace-prefixed. `KEY=value # comment` → `value`; `KEY=value-with-#-no-space` → `value-with-#-no-space` (the `#` is part of the value). Quoted values suppress stripping: `KEY="x # y"` → `x # y`. This was added after an opaque 401 trap where an inline comment leaked into a `PROXMOX_TOKEN_SECRET` value.
- **The `[x]users` tab uses arrow-keys + Enter, not letter hotkeys.** Letters would collide with the tab bar (a=apps, d=deploy, r=refresh, s=...). When extending the tab, follow the same convention; add new actions to the `userAction` submenu instead of binding new letters.
- **No role editing — period.** The schema allows it but the UI doesn't expose it. Role changes = delete + re-add. Same goes for self-delete (admin can't escalate-out-of-existence). Don't add an `[e]dit` action without re-litigating the design decision.
- **Auto-exported operator kit is a single zip** `<dirname>-<opname>.zip` (sibling of cluster.yaml; the staging folder is removed after zipping). Refuses if either the folder or the zip exists. Contains a verbatim cluster.yaml, the murmur binary, a README with launch + TUI-usage docs, and a cluster.env that **defines every `${VAR}` the config references** (operator's own token real; everything else empty placeholders, SSH_IDENTITY defaulted) so the kit loads standalone. `$${VAR}` runtime escapes are excluded from that scan.
- **Teardown is status-agnostic + HA-aware.** Don't gate the stop on `/cluster/resources` status — it's the pvestatd cache and lags both ways. Instead: attempt destroy; on "is running" hard-stop, wait, and retry (LXC needs a poll loop for cgroup/mount cleanup lag). HA-managed guests are removed from HA first (`DeleteHAResource`), or the HA stack restarts them mid-teardown and destroy 500s forever.
- **PVE string-encodes some ints.** The token-create response returns `info.privsep`/`expire` as quoted strings; `proxmox.FlexInt` unmarshals number|string|null. Use it for PVE int fields that may come back string-encoded rather than a plain `int`.
