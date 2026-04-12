package agent

import (
	"fmt"
	"log/slog"

	"github.com/KTCrisis/scout7/mesh"
)

// Architecture is the structured output extracted from an article.
type Architecture struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Components  []Component  `json:"components"`
	Connections []Connection `json:"connections"`
	Patterns    []string     `json:"patterns"`
	Source      string       `json:"source"` // URL
}

// Component is a building block in an architecture.
type Component struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Type  string `json:"type"` // service, database, queue, llm, agent, gateway, etc.
}

// Connection is a link between two components.
type Connection struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label"`
}

const extractPrompt = `You are an expert at analyzing AI agent architectures.
Given an article or README about an AI agent system, extract the architecture as structured JSON.

Return ONLY a JSON object with this schema:
{
  "name": "short name of the architecture/framework",
  "description": "one sentence summary",
  "components": [
    {"id": "unique-kebab-id", "label": "Human readable name", "type": "service|database|queue|llm|agent|gateway|memory|tool|orchestrator|monitor"}
  ],
  "connections": [
    {"from": "component-id", "to": "component-id", "label": "short description of the relationship"}
  ],
  "patterns": ["list", "of", "architecture", "patterns", "used"]
}

Rules:
- Extract 3-15 components, focus on the meaningful ones
- Types must be one of: service, database, queue, llm, agent, gateway, memory, tool, orchestrator, monitor
- Every connection must reference valid component IDs
- Patterns examples: "event-driven", "orchestrator", "choreography", "RAG", "ReAct", "tool-use", "multi-agent", "supervisor", "swarm"
- If the article doesn't describe a clear architecture, return {"name":"","description":"no architecture found","components":[],"connections":[],"patterns":[]}`

// Extract analyzes article content and returns a structured architecture.
func Extract(mc *mesh.Client, model, content, url string) (*Architecture, error) {
	slog.Info("extracting architecture", "url", url, "content_len", len(content))

	// Truncate content to avoid overwhelming the model.
	if len(content) > 12000 {
		content = content[:12000]
	}

	arch, err := ChatJSON[Architecture](mc, model, []ChatMessage{
		{Role: "system", Content: extractPrompt},
		{Role: "user", Content: fmt.Sprintf("Extract the architecture from this article:\n\n%s", content)},
	})
	if err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}

	arch.Source = url

	if arch.Name == "" || len(arch.Components) == 0 {
		return nil, fmt.Errorf("no architecture found in article")
	}

	slog.Info("extracted", "name", arch.Name, "components", len(arch.Components), "connections", len(arch.Connections))
	return &arch, nil
}
