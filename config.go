package scout7

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/KTCrisis/scout7/mesh"
	"gopkg.in/yaml.v3"
)

// Config holds the scout7 configuration.
type Config struct {
	MeshURL   string     `yaml:"mesh_url"`
	AgentID   string     `yaml:"agent_id"`
	Transport string     `yaml:"transport"` // rest (default) | mcp
	Auth      AuthConfig `yaml:"auth"`
	// ApprovalWait: how long to wait for a human when the mesh holds a call
	// for approval (e.g. 5m); zero does not wait, the call fails at once.
	ApprovalWait time.Duration `yaml:"approval_wait"`
	Interval     time.Duration `yaml:"interval"`
	Output       OutputConfig  `yaml:"output"`
	Search       SearchConfig  `yaml:"search"`
	Evaluate     EvalConfig    `yaml:"evaluate"`
	Ollama       OllamaConfig  `yaml:"ollama"`
	Judge        JudgeConfig   `yaml:"judge"`
}

// AuthConfig chooses how scout7 proves who it is to the mesh, or to a gateway
// in front of it.
//
//	mode: agent  sends "Bearer agent:<agent_id>"; a mesh on the same machine
//	             trusts the declared name (the default)
//	mode: oidc   gets an OAuth token with the client credentials grant from
//	             token_url; the secret is read from the variable named by
//	             client_secret_env, never from this file
//	mode: token  uses the token found in the variable named by token_env, as
//	             handed over by a launcher acting for a human (a chat that
//	             exchanged the human's token for scout7)
type AuthConfig struct {
	Mode            string   `yaml:"mode"`
	TokenURL        string   `yaml:"token_url"`
	ClientID        string   `yaml:"client_id"`
	ClientSecretEnv string   `yaml:"client_secret_env"`
	Scopes          []string `yaml:"scopes"`
	TokenEnv        string   `yaml:"token_env"`
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

// JudgeConfig hands the cycle's decisions (keep a page, how new it is) to a
// System One judge. Jev is a model, not an agent: scout7 calls it directly,
// as sup7 does, and a gateway may front it later through url. Empty provider
// leaves every decision to the LLM, as before.
type JudgeConfig struct {
	Provider     string        `yaml:"provider"`       // "" (the LLM decides) | jev
	Backend      string        `yaml:"backend"`        // cloudflare (default) | typesafe | local
	Model        string        `yaml:"model"`          // default per backend: typesafe/jev, jev-latest, nimble
	URL          string        `yaml:"url"`            // overrides the backend's endpoint (a gateway, a local server)
	APIKeyEnv    string        `yaml:"api_key_env"`    // variable holding the key, never the key itself
	AccountIDEnv string        `yaml:"account_id_env"` // cloudflare only
	KeepMin      float64       `yaml:"keep_min"`       // describes_architecture at or above: extract the page
	ListingMax   float64       `yaml:"listing_max"`    // product_listing at or above: drop it
	NoveltyMin   float64       `yaml:"novelty_min"`    // novelty level (0-3) at or above: draw a diagram
	StateChars   int           `yaml:"state_chars"`    // characters of the page sent to the judge
	Timeout      time.Duration `yaml:"timeout"`
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
		// thresholds measured on ~/work/jev/banc-scout7 (02/10/2026, 30 pages)
		Judge: JudgeConfig{
			Backend:      "cloudflare",
			APIKeyEnv:    "CLOUDFLARE_WORKERS_AI_TOKEN",
			AccountIDEnv: "CLOUDFLARE_ACCOUNT_ID",
			KeepMin:      0.5,
			ListingMax:   0.5,
			NoveltyMin:   1.0,
			StateChars:   8000,
			Timeout:      30 * time.Second,
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

	switch cfg.Auth.Mode {
	case "", "agent":
	case "oidc":
		if cfg.Auth.TokenURL == "" || cfg.Auth.ClientID == "" || cfg.Auth.ClientSecretEnv == "" {
			return nil, fmt.Errorf("auth: oidc needs token_url, client_id and client_secret_env")
		}
	case "token":
		if cfg.Auth.TokenEnv == "" {
			return nil, fmt.Errorf("auth: token needs token_env")
		}
	default:
		return nil, fmt.Errorf("auth: unknown mode %q (agent, oidc or token)", cfg.Auth.Mode)
	}

	switch cfg.Judge.Provider {
	case "", "jev":
	default:
		return nil, fmt.Errorf("judge: unknown provider %q (jev, or empty for the LLM)", cfg.Judge.Provider)
	}
	switch cfg.Judge.Backend {
	case "cloudflare", "typesafe", "local":
	default:
		return nil, fmt.Errorf("judge: unknown backend %q (cloudflare, typesafe or local)", cfg.Judge.Backend)
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

// NewMeshClient builds the mesh client this config describes. A new client
// per cycle keeps the REST session id per cycle, as before.
func (c *Config) NewMeshClient(sessionID string) (*mesh.Client, error) {
	o := mesh.Options{URL: c.MeshURL, Transport: c.Transport, AgentID: c.AgentID, SessionID: sessionID,
		ApprovalWait: c.ApprovalWait}
	if c.Auth.Mode == "token" {
		t := os.Getenv(c.Auth.TokenEnv)
		if t == "" {
			return nil, fmt.Errorf("auth: environment variable %s is empty", c.Auth.TokenEnv)
		}
		o.Tokens = mesh.StaticToken(t)
	}
	if c.Auth.Mode == "oidc" {
		cc, err := mesh.NewClientCredentials(c.Auth.TokenURL, c.Auth.ClientID, c.Auth.ClientSecretEnv, c.Auth.Scopes)
		if err != nil {
			return nil, err
		}
		o.Tokens = cc
	}
	return mesh.New(o)
}
