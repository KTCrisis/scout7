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
	"github.com/KTCrisis/scout7/mesh"
)

func main() {
	configPath := flag.String("config", "scout7.yaml", "path to config file")
	once := flag.Bool("once", false, "run once then exit (no loop)")
	probe := flag.Bool("probe", false, "check the mesh connection with one read and one search, then exit")
	query := flag.String("query", "", "search this instead of the configured queries (a human's request)")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg, err := scout7.LoadConfig(*configPath)
	if err != nil {
		slog.Error("failed to load config", "path", *configPath, "err", err)
		os.Exit(1)
	}

	if *query != "" {
		cfg.Search.Queries = []string{*query}
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

	if *probe {
		os.Exit(runProbe(mc))
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

// runProbe makes two cheap calls through the mesh, as the agent would, and
// says what came back: a way to check the transport, the credential and the
// policy before a real cycle.
func runProbe(mc *mesh.Client) int {
	code := 0
	calls := []struct {
		tool   string
		params map[string]any
	}{
		{"memory.memory_list", map[string]any{"agent": "scout7", "limit": 1}},
		{"searxng.searxng_web_search", map[string]any{"query": "MCP gateway", "num_results": 1}},
	}
	for _, c := range calls {
		tr, err := mc.CallTool(c.tool, c.params)
		if err != nil {
			fmt.Printf("%-28s ERROR %v\n", c.tool, err)
			code = 1
			continue
		}
		fmt.Printf("%-28s ok    %d bytes\n", c.tool, len(tr.Result))
	}
	// The agent's own parser, so both transports are compared on what the
	// agent reads, not on bytes.
	results, err := agent.Search(mc, "MCP gateway", 2)
	if err != nil {
		fmt.Printf("%-28s ERROR %v\n", "agent.Search", err)
		return 1
	}
	for _, r := range results {
		fmt.Printf("%-28s %s\n", "agent.Search", r.URL)
	}
	return code
}
