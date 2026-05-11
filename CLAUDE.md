# CLAUDE.md — murmur

## Project

`murmur` is a generic, configurable ProxMox cluster manager (TUI + CLI). Pre-v0.1, private until the shape settles. Reference repo for the lab-specific consumer is `~/TheLightLab` — read it, do not import from it.

See `.claude/skills/murmur/SKILL.md` for the development I/O contract.

## Build & run

```bash
go build -o murmur ./cmd/murmur
./murmur --config configs/example.yaml validate
```

Go 1.25.5. No tests required at the v0.1 stage; add them once the surface stabilizes.

## Git conventions

- **No AI attribution.** Never include `Co-Authored-By: Claude…`, "Generated with Claude Code", or any AI footer in commit messages, PR bodies, or pushed artifacts. This rule applies to commits AND pushes — both stay clean.
- Default to creating new commits, not amending published ones.
- Commit subject under ~72 chars; body explains the *why*.

## Design principles

- **Configurable, not hardcoded.** No cluster-specific names, IPs, domains, or storage IDs anywhere in code or fixtures. Anything cluster-specific lives in `cluster.yaml`.
- **Event-driven TUI, no tick.** Refresh happens on user action or async-fetch-complete messages — never `tea.Tick`. Copy-paste must work in a normal terminal.
- **Loud failures.** Validate at load. Surface errors with the field path. Don't silently fall back.

## Config

Two-file pattern:
- `cluster.yaml` — committable, uses `${VAR}` for secrets
- `cluster.env` — gitignored sidecar, `KEY=value`

See `configs/example.yaml` for the canonical schema.
