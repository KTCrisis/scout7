package scout7

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds the scout7 configuration.
type Config struct {
	MeshURL   string        `yaml:"mesh_url"`
	AgentID   string        `yaml:"agent_id"`
	Interval  time.Duration `yaml:"interval"`
	Output OutputConfig `yaml:"output"`
	Search    SearchConfig  `yaml:"search"`
	Evaluate  EvalConfig    `yaml:"evaluate"`
	Ollama    OllamaConfig  `yaml:"ollama"`
}

// OutputConfig controls how scout7 materializes results.
type OutputConfig struct {
	Tool      string         `yaml:"tool"`      // MCP tool name (e.g. "arch7.create_diagram")
	Format    string         `yaml:"format"`    // diagram | markdown | json | memory
	Dir       string         `yaml:"dir"`       // output directory for file-based formats
	Extension string         `yaml:"extension"` // file extension (e.g. ".excalidraw", ".md")
	Params    map[string]any `yaml:"params"`    // static params passed to the tool
}

// SearchConfig controls what to search for.
type SearchConfig struct {
	Queries    []string `yaml:"queries"`
	MaxResults int      `yaml:"max_results"`
}

// EvalConfig controls novelty filtering.
type EvalConfig struct {
	MinNoveltyScore int `yaml:"min_novelty_score"`
}

// OllamaConfig controls which model to use.
type OllamaConfig struct {
	Model string `yaml:"model"`
}

// LoadConfig reads a YAML config file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{
		MeshURL:  "http://localhost:9090",
		AgentID:  "scout7",
		Interval: 6 * time.Hour,
		Output: OutputConfig{
			Tool:      "arch7.create_diagram",
			Format:    "diagram",
			Dir:       "./diagrams",
			Extension: ".excalidraw",
			Params: map[string]any{
				"direction": "LR",
				"theme":     "professional",
			},
		},
		Search: SearchConfig{
			MaxResults: 10,
		},
		Evaluate: EvalConfig{
			MinNoveltyScore: 7,
		},
		Ollama: OllamaConfig{
			Model: "gemma4:e4b",
		},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Resolve output dir to absolute path for file-based outputs.
	if cfg.Output.Dir != "" && !filepath.IsAbs(cfg.Output.Dir) {
		abs, err := filepath.Abs(cfg.Output.Dir)
		if err != nil {
			return nil, fmt.Errorf("resolve output dir: %w", err)
		}
		cfg.Output.Dir = abs
	}

	if len(cfg.Search.Queries) == 0 {
		cfg.Search.Queries = []string{
			"agentic AI architecture 2026",
			"AI agent framework design pattern",
			"multi-agent system architecture",
			"MCP agent orchestration",
			"autonomous AI agent infrastructure",
		}
	}

	return cfg, nil
}
