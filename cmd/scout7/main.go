package main

import (
	"cmp"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	scout7 "github.com/KTCrisis/scout7"
	"github.com/KTCrisis/scout7/agent"
)

func main() {
	configPath := flag.String("config", "scout7.yaml", "path to config file")
	once := flag.Bool("once", false, "run once then exit (no loop)")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg, err := scout7.LoadConfig(*configPath)
	if err != nil {
		slog.Error("failed to load config", "path", *configPath, "err", err)
		os.Exit(1)
	}

	slog.Info("scout7 starting",
		"mesh", cfg.MeshURL,
		"transport", cmp.Or(cfg.Transport, "rest"),
		"auth", cmp.Or(cfg.Auth.Mode, "agent"),
		"agent", cfg.AgentID,
		"model", cfg.Ollama.Model,
		"queries", len(cfg.Search.Queries),
		"interval", cfg.Interval,
	)

	sessionID := fmt.Sprintf("scout7-%d", time.Now().Unix())
	mc, err := cfg.NewMeshClient(sessionID)
	if err != nil {
		slog.Error("failed to build the mesh client", "err", err)
		os.Exit(1)
	}

	if *once {
		stats, err := agent.Run(mc, cfg)
		if err != nil {
			slog.Error("run failed", "err", err)
			os.Exit(1)
		}
		slog.Info("done",
			"searched", stats.Searched,
			"fetched", stats.Fetched,
			"extracted", stats.Extracted,
			"produced", stats.Produced,
			"skipped", stats.Skipped,
			"errors", stats.Errors,
		)
		return
	}

	if err := agent.Loop(mc, cfg); err != nil {
		slog.Error("loop failed", "err", err)
		os.Exit(1)
	}
}
