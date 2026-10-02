package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"time"

	scout7 "github.com/KTCrisis/scout7"
)

// The judge takes two decisions of a cycle away from the LLM: does a page
// describe an agent architecture, and how new is it next to what scout7 has
// already seen. A System One judge (Jev) answers typed questions with
// probabilities in a few hundred milliseconds and writes no text; the code
// decides with thresholds measured on a bench (~/work/jev/banc-scout7,
// 02/10/2026). Extraction stays with the LLM.
//
// It runs before extraction: a page the judge drops costs one call of a
// third of a second instead of an LLM extraction.

// Verdict is what the judge answered for one page.
type Verdict struct {
	Describes float64 // P(the page describes an agent system: components and connections)
	Listing   float64 // P(the page is a ranking or a product listing)
	Novelty   float64 // expected level: 0 rehash, 1 variant, 2 new component, 3 new paradigm
	Category  string  // most probable category
	Model     string
}

// Keep reports whether the page is worth extracting.
func (v *Verdict) Keep(cfg scout7.JudgeConfig) bool {
	return v.Describes >= cfg.KeepMin && v.Listing < cfg.ListingMax
}

// Evaluation turns the verdict into what the rest of the cycle stores. The
// score keeps scout7's 0-10 scale so memories stay comparable; the decision
// to draw a diagram comes from the judge's own threshold.
func (v *Verdict) Evaluation(cfg scout7.JudgeConfig) *Evaluation {
	return &Evaluation{
		Score:     int(math.Round(v.Novelty / 3 * 10)),
		Reason:    fmt.Sprintf("judge %s: novelty %.2f/3, describes %.2f, listing %.2f", v.Model, v.Novelty, v.Describes, v.Listing),
		Category:  v.Category,
		DiagramIt: v.Novelty >= cfg.NoveltyMin,
	}
}

var noveltyLevels = []string{
	"rehash: a pattern already in `seen` or a textbook pattern (basic RAG, ReAct loop, single orchestrator) restated",
	"variant: a known pattern with a minor twist (another framework, other components of the same kinds)",
	"new component: introduces a component, a connection or a control point absent from every architecture in `seen`",
	"new paradigm: a different way to build or run agents, that no architecture in `seen` resembles",
}

// judgeQuestions are the bench's questions, minus new_control_point, which
// separated nothing (0.33 to 0.63 on every page).
var judgeQuestions = map[string]any{
	"describes_architecture": map[string]any{
		"type":         "noul",
		"instructions": "The article describes the architecture of a specific AI agent system or framework: named components and how they connect.",
		"criteria": map[string]string{
			"true":  "Names the parts of an agent system (agents, tools, memory, gateways, queues...) and how they interact",
			"false": "News, funding, opinion, a list of products, a tutorial or a glossary without a described system",
		},
	},
	"product_listing": map[string]any{
		"type":         "noul",
		"instructions": "The article is mainly a ranking, a comparison table or a marketing page about several products, rather than the description of one system.",
	},
	"novelty": map[string]any{
		"type":         "score",
		"instructions": "Compared with the architectures listed in `seen`, how new is the architecture this article describes?",
		"criteria":     noveltyLevels,
	},
	"category": map[string]any{
		"type":         "choice",
		"instructions": "What kind of contribution does the article describe?",
		"criteria": map[string]string{
			"framework":      "A library or framework to build agents",
			"pattern":        "A design pattern or a way to organise agents",
			"infrastructure": "Runtime, deployment, gateways, protocols or operations for agents",
			"research":       "A paper or an experimental system",
			"product":        "A commercial product or service",
		},
	},
}

// Judge calls a System One endpoint: Jev on Cloudflare Workers AI or on
// TypeSafe's API, or a local model served by Ollama (nimble).
type Judge struct {
	cfg  scout7.JudgeConfig
	http *http.Client
}

// NewJudge returns a judge for the configuration, or nil when the cycle is
// left to the LLM.
func NewJudge(cfg scout7.JudgeConfig) *Judge {
	if cfg.Provider == "" {
		return nil
	}
	return &Judge{cfg: cfg, http: &http.Client{Timeout: cfg.Timeout}}
}

// request builds the URL, headers and body for the configured backend.
func (j *Judge) request(body map[string]any) (string, map[string]string, map[string]any, error) {
	headers := map[string]string{"Content-Type": "application/json"}
	if j.cfg.APIKeyEnv != "" && j.cfg.Backend != "local" {
		token := os.Getenv(j.cfg.APIKeyEnv)
		if token == "" {
			return "", nil, nil, fmt.Errorf("judge: %s is not set", j.cfg.APIKeyEnv)
		}
		headers["Authorization"] = "Bearer " + token
	}
	model, url := j.cfg.Model, j.cfg.URL
	switch j.cfg.Backend {
	case "cloudflare":
		account := os.Getenv(j.cfg.AccountIDEnv)
		if account == "" && url == "" {
			return "", nil, nil, fmt.Errorf("judge: %s is not set", j.cfg.AccountIDEnv)
		}
		if url == "" {
			url = "https://api.cloudflare.com/client/v4/accounts/" + account + "/ai/run"
		}
		if model == "" {
			model = "typesafe/jev"
		}
		return url, headers, map[string]any{"model": model, "input": body}, nil
	case "typesafe":
		if url == "" {
			url = "https://api.typesafe.ai/v1/systemone"
		}
		if model == "" {
			model = "jev-latest"
		}
	case "local":
		if url == "" {
			url = "http://localhost:11434/v1/systemone"
		}
		if model == "" {
			model = "nimble"
		}
	default:
		return "", nil, nil, fmt.Errorf("judge: unknown backend %q", j.cfg.Backend)
	}
	flat := map[string]any{"model": model}
	for k, v := range body {
		flat[k] = v
	}
	return url, headers, flat, nil
}

// Judge asks the questions about one page.
func (j *Judge) Judge(content, title string, seen []string) (*Verdict, error) {
	body := map[string]any{
		"state":     map[string]any{"article": truncateRunes(content, j.cfg.StateChars), "title": title, "seen": seen},
		"questions": judgeQuestions,
	}
	url, headers, payload, err := j.request(body)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	start := time.Now()
	resp, err := j.http.Do(req)
	if err != nil {
		// never log the URL: on Cloudflare it carries the account id
		return nil, fmt.Errorf("judge: request failed: %T", err)
	}
	defer resp.Body.Close()
	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("judge: HTTP %d, response is not JSON", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("judge: HTTP %d", resp.StatusCode)
	}
	v, err := parseVerdict(data)
	if err != nil {
		return nil, err
	}
	slog.Info("judged", "title", title, "describes", v.Describes, "listing", v.Listing,
		"novelty", v.Novelty, "category", v.Category, "ms", time.Since(start).Milliseconds())
	return v, nil
}

// parseVerdict reads the answers whatever the envelope: TypeSafe and Ollama
// return {"model", "answers"}; Cloudflare wraps it twice,
// {"result": {"state": "Completed", "result": {...}}}.
func parseVerdict(data map[string]any) (*Verdict, error) {
	payload := data
	for i := 0; i < 3 && payload != nil; i++ {
		if _, ok := payload["answers"]; ok {
			break
		}
		payload, _ = payload["result"].(map[string]any)
	}
	answers, _ := payload["answers"].(map[string]any)
	if answers == nil {
		return nil, fmt.Errorf("judge: no answers in the response")
	}
	v := &Verdict{}
	v.Model, _ = payload["model"].(string)

	field := func(name, key string) (float64, error) {
		a, _ := answers[name].(map[string]any)
		f, ok := a[key].(float64)
		if !ok {
			return 0, fmt.Errorf("judge: answer %s has no %s", name, key)
		}
		return f, nil
	}
	var err error
	if v.Describes, err = field("describes_architecture", "noul"); err != nil {
		return nil, err
	}
	if v.Listing, err = field("product_listing", "noul"); err != nil {
		return nil, err
	}
	if v.Novelty, err = field("novelty", "score"); err != nil {
		return nil, err
	}
	// the category is a label for memory: a missing one is not an error
	if c, ok := answers["category"].(map[string]any); ok {
		if probs, ok := c["probabilities"].(map[string]any); ok {
			best := -1.0
			for name, p := range probs {
				if f, ok := p.(float64); ok && f > best {
					best, v.Category = f, name
				}
			}
		}
	}
	return v, nil
}

// truncateRunes cuts s to n characters without splitting a UTF-8 sequence.
func truncateRunes(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
