package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/KTCrisis/scout7/mesh"
)

// extractMCPText extracts concatenated text from an MCP content response.
func extractMCPText(raw json.RawMessage) string {
	var mcp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &mcp); err != nil {
		return string(raw)
	}
	var parts []string
	for _, c := range mcp.Content {
		parts = append(parts, c.Text)
	}
	return strings.Join(parts, "\n")
}

// MemoryEntry is what we store in mem7 for each processed article.
type MemoryEntry struct {
	URL         string   `json:"url"`
	Name        string   `json:"name"`
	Score       int      `json:"score"`
	Category    string   `json:"category"`
	Patterns    []string `json:"patterns"`
	DiagramPath string   `json:"diagram_path,omitempty"`
	Reason      string   `json:"reason"`
}

// IsURLSeen checks mem7 to see if we already processed this URL.
func IsURLSeen(mc *mesh.Client, url string) bool {
	tr, err := mc.CallTool("memory.memory_search", map[string]any{
		"query": url,
	})
	if err != nil {
		slog.Warn("memory search failed, treating as unseen", "url", url, "err", err)
		return false
	}

	// Extract text from MCP content response and check if the URL appears.
	text := extractMCPText(tr.Result)
	if text == "" || text == "No memories found." {
		return false
	}
	return strings.Contains(text, url)
}

// StoreResult saves a processed article to mem7.
func StoreResult(mc *mesh.Client, entry MemoryEntry) error {
	slog.Info("storing memory", "name", entry.Name, "url", entry.URL, "score", entry.Score)

	content, _ := json.Marshal(entry)

	_, err := mc.CallTool("memory.memory_store", map[string]any{
		"key":     fmt.Sprintf("scout7:%s", slugify(entry.Name)),
		"content": string(content),
		"metadata": map[string]any{
			"source":   "scout7",
			"url":      entry.URL,
			"score":    entry.Score,
			"category": entry.Category,
		},
	})
	if err != nil {
		return fmt.Errorf("store memory: %w", err)
	}

	return nil
}

// ListSeenNames returns the names of architectures already stored.
func ListSeenNames(mc *mesh.Client) []string {
	tr, err := mc.CallTool("memory.memory_search", map[string]any{
		"query": "scout7",
	})
	if err != nil {
		slog.Warn("memory list failed", "err", err)
		return nil
	}

	// Try to parse results and extract names.
	var results []struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(tr.Result, &results); err != nil {
		return nil
	}

	var names []string
	for _, r := range results {
		var entry MemoryEntry
		if err := json.Unmarshal([]byte(r.Content), &entry); err == nil && entry.Name != "" {
			names = append(names, entry.Name)
		}
	}

	return names
}
