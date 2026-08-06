package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"syscall"

	"github.com/rtx-monster/murmur/internal/config"
	"github.com/rtx-monster/murmur/internal/console"
	"github.com/rtx-monster/murmur/internal/events"
	"github.com/rtx-monster/murmur/internal/mcpserver"
	"github.com/rtx-monster/murmur/internal/proxmox"
	"github.com/rtx-monster/murmur/internal/tui"
)

func main() {
	cfgPath := flag.String("config", "", "path to cluster.yaml (overrides discovery)")
	asFlag := flag.String("as", "", "select operator by name from cluster.yaml users: (or set MURMUR_USER)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: murmur [--config PATH] [--as NAME] <command>")
		fmt.Fprintln(os.Stderr, "commands:")
		fmt.Fprintln(os.Stderr, "  validate    load and validate the cluster config")
		fmt.Fprintln(os.Stderr, "  status      connect to the cluster and print version + resource summary")
		fmt.Fprintln(os.Stderr, "  tui         launch the interactive TUI")
		fmt.Fprintln(os.Stderr, "  mcp         serve cluster operations as MCP tools over stdio (for AI operators)")
		fmt.Fprintln(os.Stderr, "  console     serve the self-hosted event console (web UI + event ingest hub)")
		fmt.Fprintln(os.Stderr, "  emit        append an event to the spine (local file, or hub via MURMUR_EVENTS_URL)")
		fmt.Fprintln(os.Stderr, "  sweep       one guarded unattended-update pass (agents: rulespace; --dry-run / --agent NAME)")
		fmt.Fprintln(os.Stderr, "  whoami      print the resolved operator identity for this invocation")
	}
	flag.Parse()

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	// console and emit are event-spine commands: they run on hosts that have
	// no cluster.yaml or PVE credentials (the coordinator LXC, warden guests,
	// ad-hoc scripts), so they are dispatched before config/identity loading.
	switch flag.Arg(0) {
	case "console":
		if err := cmdConsole(flag.Args()[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	case "emit":
		if err := cmdEmit(flag.Args()[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	resolvedPath, cfg, err := loadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	active, err := cfg.ResolveActive(*asFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	switch flag.Arg(0) {
	case "validate":
		fmt.Printf("ok: cluster=%s domain=%s nodes=%d flavors=%d images=%d users=%d roles=%d\n",
			cfg.Cluster.Name, cfg.Cluster.Domain,
			len(cfg.Cluster.Nodes), len(cfg.Flavors), len(cfg.Images),
			len(cfg.Users), len(cfg.Roles))
	case "whoami":
		cmdWhoami(active)
	case "status":
		if err := cmdStatus(cfg, active); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "tui":
		if err := cmdTUI(cfg, active, resolvedPath); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "mcp":
		if err := cmdMCP(cfg, active); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "sweep":
		if err := cmdSweep(cfg, active, flag.Args()[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", flag.Arg(0))
		flag.Usage()
		os.Exit(2)
	}
}

func cmdWhoami(a *config.ActiveUser) {
	if a.Fallback {
		fmt.Printf("operator: <implicit admin via cluster.api.token_id>\n")
		fmt.Printf("token id: %s\n", a.TokenID)
		fmt.Printf("role:     admin (builtin, fallback — no users: section)\n")
		return
	}
	fmt.Printf("operator: %s\n", a.Name)
	fmt.Printf("token id: %s\n", a.TokenID)
	fmt.Printf("role:     %s\n", a.Role.Name)
	fmt.Printf("tabs:     %v\n", a.Role.Tabs)
	fmt.Printf("actions:  %v\n", a.Role.Actions)
	fmt.Printf("apps:     %v\n", a.Role.Apps)
	fmt.Printf("guests:   %s\n", a.Role.Guests)
}

func loadConfig(path string) (string, *config.Config, error) {
	if path != "" {
		cfg, err := config.LoadFile(path)
		return path, cfg, err
	}
	return config.Load()
}

func cmdStatus(cfg *config.Config, active *config.ActiveUser) error {
	client, err := proxmox.New(proxmox.Config{
		Endpoint:      cfg.Cluster.API.Endpoint,
		TokenID:       active.TokenID,
		TokenSecret:   active.TokenSecret,
		TLSSkipVerify: cfg.Cluster.API.TLSSkipVerify,
	})
	if err != nil {
		return err
	}
	ctx := context.Background()

	version, err := client.GetVersion(ctx)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	resources, err := client.GetResources(ctx, "")
	if err != nil {
		return fmt.Errorf("list resources: %w", err)
	}

	// Tally by type.
	tally := map[string]int{}
	for _, r := range resources {
		tally[r.Type]++
	}
	keys := make([]string, 0, len(tally))
	for k := range tally {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	fmt.Printf("cluster: %s (%s)\n", cfg.Cluster.Name, cfg.Cluster.API.Endpoint)
	fmt.Printf("proxmox: %s (release %s)\n", version.Version, version.Release)
	fmt.Printf("resources:\n")
	for _, k := range keys {
		fmt.Printf("  %-10s %d\n", k+":", tally[k])
	}

	// Loud-fail semantic check on configured storage IDs.
	required := []string{cfg.Cluster.Storage.VMDisk, cfg.Cluster.Storage.Shared}
	if cfg.Cluster.Storage.ISO != "" && cfg.Cluster.Storage.ISO != cfg.Cluster.Storage.Shared {
		required = append(required, cfg.Cluster.Storage.ISO)
	}
	if err := client.EnsureStorages(ctx, required); err != nil {
		return err
	}
	fmt.Printf("storage:   all configured IDs present (%v)\n", required)
	return nil
}

func cmdConsole(args []string) error {
	fs := flag.NewFlagSet("console", flag.ExitOnError)
	listen := fs.String("listen", ":8686", "address to serve the console on")
	dirFlag := fs.String("events-dir", "", "events directory (default $MURMUR_EVENTS_DIR or ~/.local/state/murmur/events)")
	cfgFlag := fs.String("config", "", "optional cluster.yaml — enables the live fleet view (use a read-only identity)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := *dirFlag
	if dir == "" {
		var err error
		if dir, err = events.DefaultDir(); err != nil {
			return err
		}
	}
	srv, err := console.New(dir)
	if err != nil {
		return err
	}

	// Optional fleet view. Without --config the console stays credential-free
	// and serves events alone; with one it also renders live cluster state
	// using that identity (which should be read-only).
	if *cfgFlag != "" {
		cfg, err := config.LoadFile(*cfgFlag)
		if err != nil {
			return fmt.Errorf("fleet view: %w", err)
		}
		active, err := cfg.ResolveActive("")
		if err != nil {
			return fmt.Errorf("fleet view: %w", err)
		}
		client, err := proxmox.New(proxmox.Config{
			Endpoint:      cfg.Cluster.API.Endpoint,
			TokenID:       active.TokenID,
			TokenSecret:   active.TokenSecret,
			TLSSkipVerify: cfg.Cluster.API.TLSSkipVerify,
		})
		if err != nil {
			return fmt.Errorf("fleet view: %w", err)
		}
		srv.SetFleet(console.NewFleet(client, cfg))
		fmt.Fprintf(os.Stderr, "console: fleet view enabled as %s\n", active.TokenID)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return srv.Run(ctx, *listen)
}

func cmdEmit(args []string) error {
	fs := flag.NewFlagSet("emit", flag.ExitOnError)
	agent := fs.String("agent", "cli", "agent name recorded on the event")
	severity := fs.String("severity", events.SevInfo, "debug|info|warn|escalate")
	kind := fs.String("kind", "", "event kind (required), e.g. finding, patrol, update")
	subject := fs.String("subject", "", "what the event is about (guest, app, node)")
	message := fs.String("message", "", "human-readable message")
	payload := fs.String("payload", "", "optional JSON object with structured detail")
	if err := fs.Parse(args); err != nil {
		return err
	}
	e := events.Event{Agent: *agent, Severity: *severity, Kind: *kind, Subject: *subject, Message: *message}
	if *payload != "" {
		if err := json.Unmarshal([]byte(*payload), &e.Payload); err != nil {
			return fmt.Errorf("--payload is not a JSON object: %w", err)
		}
	}
	if url := os.Getenv("MURMUR_EVENTS_URL"); url != "" {
		return events.NewHTTPSink(url).Emit(e)
	}
	dir, err := events.DefaultDir()
	if err != nil {
		return err
	}
	sink, err := events.NewFileSink(dir)
	if err != nil {
		return err
	}
	return sink.Emit(e)
}

func cmdMCP(cfg *config.Config, active *config.ActiveUser) error {
	client, err := proxmox.New(proxmox.Config{
		Endpoint:      cfg.Cluster.API.Endpoint,
		TokenID:       active.TokenID,
		TokenSecret:   active.TokenSecret,
		TLSSkipVerify: cfg.Cluster.API.TLSSkipVerify,
	})
	if err != nil {
		return err
	}
	return mcpserver.Run(context.Background(), cfg, client, active, "0.2.0")
}

func cmdTUI(cfg *config.Config, active *config.ActiveUser, configPath string) error {
	client, err := proxmox.New(proxmox.Config{
		Endpoint:      cfg.Cluster.API.Endpoint,
		TokenID:       active.TokenID,
		TokenSecret:   active.TokenSecret,
		TLSSkipVerify: cfg.Cluster.API.TLSSkipVerify,
	})
	if err != nil {
		return err
	}
	return tui.Run(cfg, client, active, configPath)
}
