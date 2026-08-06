package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/rtx-monster/murmur/internal/config"
	"github.com/rtx-monster/murmur/internal/events"
	"github.com/rtx-monster/murmur/internal/provision"
	"github.com/rtx-monster/murmur/internal/proxmox"
)

// cmdSweep runs one guarded unattended-update pass for a rulespace agent.
// The guardrails are code, not model output:
//  1. explicit per-app allowlist (agents[].unattended_apps)
//  2. update window enforcement (agents[].update_window)
//  3. one app per sweep tick
//  4. post-update verification, escalating on failure
//
// Every step lands on the event spine. Loud-fail everywhere.
func cmdSweep(cfg *config.Config, active *config.ActiveUser, args []string) error {
	fs := flag.NewFlagSet("sweep", flag.ExitOnError)
	agentName := fs.String("agent", "", "agents: entry to run (default: sole agent with unattended_apps)")
	dryRun := fs.Bool("dry-run", false, "report what would be updated without touching anything")
	forceWindow := fs.Bool("force-window", false, "run outside the agent's update_window (manual invocations only)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	agent, err := resolveSweepAgent(cfg, *agentName)
	if err != nil {
		return err
	}

	spine, err := sweepSpine()
	if err != nil {
		return err
	}
	emit := func(severity, kind, subject, msg string, payload map[string]any) {
		e := events.Event{Agent: agent.Name, Severity: severity, Kind: kind, Subject: subject, Message: msg, Payload: payload}
		if err := spine.Emit(e); err != nil {
			fmt.Fprintln(os.Stderr, "event emit failed:", err)
		}
	}

	// Guardrail 2: window.
	if agent.UpdateWindow != "" && !*forceWindow && !*dryRun {
		ok, err := config.InWindow(agent.UpdateWindow, time.Now())
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("outside update window %s — refusing (use --dry-run to inspect, --force-window for a deliberate manual run)", agent.UpdateWindow)
		}
	}

	client, err := proxmox.New(proxmox.Config{
		Endpoint:      cfg.Cluster.API.Endpoint,
		TokenID:       active.TokenID,
		TokenSecret:   active.TokenSecret,
		TLSSkipVerify: cfg.Cluster.API.TLSSkipVerify,
	})
	if err != nil {
		return err
	}
	orch := provision.New(cfg, client)
	orch.SetActiveUser(active)
	orch.SetProgress(func(e provision.ProgressEvent) { fmt.Fprintf(os.Stderr, "progress: %s %s\n", e.Step, e.Message) })

	ctx := context.Background()
	checked, updated := 0, 0

	for _, appName := range agent.UnattendedApps {
		app, ok := appByName(cfg, appName)
		if !ok {
			// validateAgents prevents this; belt and suspenders.
			emit(events.SevWarn, "update", appName, "allowlisted app missing from catalog — skipping", nil)
			continue
		}
		info := orch.InspectApp(ctx, app)
		checked++
		if info.VMID == 0 {
			emit(events.SevWarn, "update", appName, "allowlisted app is not deployed — skipping", nil)
			continue
		}
		if !info.UpdateChecked {
			emit(events.SevInfo, "update", appName, "update probe could not run (guest down or unreachable) — skipping", nil)
			continue
		}
		if info.UpdatesAvailable == 0 {
			continue
		}

		preRunning := composeRunning(info.Compose)
		payload := map[string]any{
			"vmid": info.VMID, "node": info.Node,
			"updates_available": info.UpdatesAvailable, "updates_total": info.UpdatesTotal,
			"compose_running_before": preRunning,
		}
		if *dryRun {
			emit(events.SevInfo, "update", appName, fmt.Sprintf("dry-run: %d/%d component update(s) available, would update", info.UpdatesAvailable, info.UpdatesTotal), payload)
			continue // dry-run inspects every app; no one-per-tick cut
		}

		emit(events.SevInfo, "update", appName, fmt.Sprintf("unattended update starting: %d/%d component(s)", info.UpdatesAvailable, info.UpdatesTotal), payload)
		if err := orch.PatchAppInstance(ctx, app, info.VMID, info.Node); err != nil {
			payload["error"] = err.Error()
			emit(events.SevEscalate, "update", appName, "unattended update FAILED — human needed: "+err.Error(), payload)
			return fmt.Errorf("update of %s failed: %w", appName, err)
		}

		// Guardrail 4: verify. The app must be running with at least as many
		// compose services as before; anything less escalates.
		post := orch.InspectApp(ctx, app)
		postRunning := composeRunning(post.Compose)
		payload["compose_running_after"] = postRunning
		payload["status_after"] = post.Status
		if post.Status != "running" || postRunning < preRunning {
			emit(events.SevEscalate, "update", appName,
				fmt.Sprintf("post-update verification FAILED: status=%s, compose running %d→%d — human needed", post.Status, preRunning, postRunning), payload)
			return fmt.Errorf("post-update verification failed for %s", appName)
		}
		emit(events.SevInfo, "update", appName,
			fmt.Sprintf("unattended update OK: compose running %d→%d", preRunning, postRunning), payload)
		updated++
		break // Guardrail 3: one app per tick.
	}

	mode := ""
	if *dryRun {
		mode = " (dry-run)"
	}
	emit(events.SevInfo, "patrol", "update-sweep",
		fmt.Sprintf("update sweep complete%s: %d app(s) checked, %d updated", mode, checked, updated),
		map[string]any{"checked": checked, "updated": updated, "dry_run": *dryRun})
	return nil
}

func resolveSweepAgent(cfg *config.Config, name string) (config.Agent, error) {
	if name != "" {
		a, ok := cfg.AgentByName(name)
		if !ok {
			return config.Agent{}, fmt.Errorf("no agents: entry named %q", name)
		}
		return a, nil
	}
	var withGrants []config.Agent
	for _, a := range cfg.Agents {
		if len(a.UnattendedApps) > 0 {
			withGrants = append(withGrants, a)
		}
	}
	switch len(withGrants) {
	case 0:
		return config.Agent{}, fmt.Errorf("no agents: entry has unattended_apps — nothing to sweep")
	case 1:
		return withGrants[0], nil
	default:
		return config.Agent{}, fmt.Errorf("multiple agents have unattended_apps — pass --agent")
	}
}

func sweepSpine() (events.Sink, error) {
	dir, err := events.DefaultDir()
	if err != nil {
		return nil, err
	}
	local, err := events.NewFileSink(dir)
	if err != nil {
		return nil, err
	}
	spine := events.Multi{local}
	if hub := os.Getenv("MURMUR_EVENTS_URL"); hub != "" {
		spine = append(spine, events.BestEffort{Sink: events.NewHTTPSink(hub)})
	}
	return spine, nil
}

func appByName(cfg *config.Config, name string) (config.App, bool) {
	for _, a := range cfg.Apps {
		if a.Name == name {
			return a, true
		}
	}
	return config.App{}, false
}

func composeRunning(cs []provision.ComposeContainer) int {
	n := 0
	for _, c := range cs {
		if c.State == "running" {
			n++
		}
	}
	return n
}
