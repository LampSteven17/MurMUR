package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/rtx-monster/murmur/internal/config"
	"github.com/rtx-monster/murmur/internal/proxmox"
	"github.com/rtx-monster/murmur/internal/tui"
)

func main() {
	cfgPath := flag.String("config", "", "path to cluster.yaml (overrides discovery)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: murmur [--config PATH] <command>")
		fmt.Fprintln(os.Stderr, "commands:")
		fmt.Fprintln(os.Stderr, "  validate    load and validate the cluster config")
		fmt.Fprintln(os.Stderr, "  status      connect to the cluster and print version + resource summary")
		fmt.Fprintln(os.Stderr, "  tui         launch the interactive TUI")
	}
	flag.Parse()

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	switch flag.Arg(0) {
	case "validate":
		fmt.Printf("ok: cluster=%s domain=%s nodes=%d flavors=%d images=%d\n",
			cfg.Cluster.Name, cfg.Cluster.Domain,
			len(cfg.Cluster.Nodes), len(cfg.Flavors), len(cfg.Images))
	case "status":
		if err := cmdStatus(cfg); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "tui":
		if err := cmdTUI(cfg); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", flag.Arg(0))
		flag.Usage()
		os.Exit(2)
	}
}

func loadConfig(path string) (*config.Config, error) {
	if path != "" {
		return config.LoadFile(path)
	}
	return config.Load()
}

func cmdStatus(cfg *config.Config) error {
	client, err := proxmox.New(proxmox.Config{
		Endpoint:      cfg.Cluster.API.Endpoint,
		TokenID:       cfg.Cluster.API.TokenID,
		TokenSecret:   cfg.Cluster.API.TokenSecret,
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

func cmdTUI(cfg *config.Config) error {
	client, err := proxmox.New(proxmox.Config{
		Endpoint:      cfg.Cluster.API.Endpoint,
		TokenID:       cfg.Cluster.API.TokenID,
		TokenSecret:   cfg.Cluster.API.TokenSecret,
		TLSSkipVerify: cfg.Cluster.API.TLSSkipVerify,
	})
	if err != nil {
		return err
	}
	return tui.Run(cfg, client)
}
