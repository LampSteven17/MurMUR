package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/rtx-monster/murmur/internal/config"
)

func main() {
	cfgPath := flag.String("config", "", "path to cluster.yaml (overrides discovery)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: murmur [--config PATH] <command>")
		fmt.Fprintln(os.Stderr, "commands:")
		fmt.Fprintln(os.Stderr, "  validate    load and validate the cluster config")
	}
	flag.Parse()

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
	case "":
		flag.Usage()
		os.Exit(2)
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
