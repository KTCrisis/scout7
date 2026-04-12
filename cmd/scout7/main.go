package main

import (
	"flag"
	"log/slog"
	"os"

	scout7 "github.com/KTCrisis/scout7"
	"github.com/KTCrisis/scout7/agent"
	"github.com/KTCrisis/scout7/mesh"
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
		"agent", cfg.AgentID,
		"model", cfg.Ollama.Model,
		"queries", len(cfg.Search.Queries),
		"interval", cfg.Interval,
	)

	mc := mesh.NewClient(cfg.MeshURL, cfg.AgentID, "")

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
			"diagrammed", stats.Diagrammed,
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
