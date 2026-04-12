package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/KTCrisis/scout7/mesh"
)

// SearchResult represents a single search hit.
type SearchResult struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Body  string `json:"body"`
}

// mcpContent is the MCP-standard response shape.
type mcpContent struct {
	Content []struct {
		Text string `json:"text"`
		Type string `json:"type"`
	} `json:"content"`
}

var reURL = regexp.MustCompile(`(?i)^URL:\s*(.+)`)
var reTitle = regexp.MustCompile(`(?i)^Title:\s*(.+)`)
var reDesc = regexp.MustCompile(`(?i)^Description:\s*(.+)`)

// parseTextResults parses the searxng text block into structured results.
func parseTextResults(text string) []SearchResult {
	var results []SearchResult
	var cur SearchResult

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if m := reTitle.FindStringSubmatch(line); m != nil {
			// Start a new result — flush previous if it had a URL.
			if cur.URL != "" {
				results = append(results, cur)
			}
			cur = SearchResult{Title: strings.TrimSpace(m[1])}
		} else if m := reURL.FindStringSubmatch(line); m != nil {
			cur.URL = strings.TrimSpace(m[1])
		} else if m := reDesc.FindStringSubmatch(line); m != nil {
			cur.Body = strings.TrimSpace(m[1])
		}
	}
	// Flush last.
	if cur.URL != "" {
		results = append(results, cur)
	}
	return results
}

// Search queries searxng via agent-mesh and returns results.
func Search(mc *mesh.Client, query string, maxResults int) ([]SearchResult, error) {
	slog.Info("searching", "query", query)

	tr, err := mc.CallTool("searxng.searxng_web_search", map[string]any{
		"query":       query,
		"num_results": maxResults,
	})
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	// Try MCP content format first (searxng returns text blocks).
	var mcp mcpContent
	if err := json.Unmarshal(tr.Result, &mcp); err == nil && len(mcp.Content) > 0 {
		var all []SearchResult
		for _, c := range mcp.Content {
			all = append(all, parseTextResults(c.Text)...)
		}
		return all, nil
	}

	// Fallback: try structured JSON.
	var raw struct {
		Results []SearchResult `json:"results"`
	}
	if err := json.Unmarshal(tr.Result, &raw); err == nil && len(raw.Results) > 0 {
		return raw.Results, nil
	}

	var results []SearchResult
	if err := json.Unmarshal(tr.Result, &results); err == nil {
		return results, nil
	}

	return nil, fmt.Errorf("parse search results: unrecognized format (raw: %s)", string(tr.Result[:min(len(tr.Result), 200)]))
}

// FetchContent retrieves the text content of a URL via agent-mesh fetch.
func FetchContent(mc *mesh.Client, url string) (string, error) {
	slog.Info("fetching", "url", url)

	tr, err := mc.CallTool("fetch.fetch", map[string]any{
		"url": url,
	})
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", url, err)
	}

	// Try MCP content format.
	var mcp mcpContent
	if err := json.Unmarshal(tr.Result, &mcp); err == nil && len(mcp.Content) > 0 {
		var parts []string
		for _, c := range mcp.Content {
			parts = append(parts, c.Text)
		}
		return strings.Join(parts, "\n"), nil
	}

	// Fallback: structured or plain string.
	var content struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(tr.Result, &content); err == nil && content.Content != "" {
		return content.Content, nil
	}

	var s string
	if err := json.Unmarshal(tr.Result, &s); err == nil {
		return s, nil
	}

	return string(tr.Result), nil
}
