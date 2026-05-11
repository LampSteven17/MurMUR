# murmur

TUI + CLI for managing a ProxMox cluster.

**Status:** pre-v0.1. Private until the shape settles.

## Why

A configurable, opinionated ProxMox cluster manager. Define your cluster in YAML, get a TUI that's actually pleasant to copy/paste out of, and run provisioning + updates without the magic refresh ticks that fight your terminal.

## Design principles

- **Configurable, not hardcoded.** One cluster YAML describes nodes, storage, network, monitoring. No `prxy-*` / `192.168.x.x` baked into the binary.
- **Event-driven TUI, no periodic refresh.** Redraws happen when the user acts or when an async fetch returns — not on a tick. Copy-paste works the way you expect.
- **Explicit updates.** No "auto-detected, mysteriously failed to apply." Show diffs, show apply output, fail loudly.
- **Charm stack.** Bubble Tea, Lip Gloss, Bubbles.

## Status

Not yet usable. See tasks in TheLightLab project notes.
