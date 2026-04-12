package agent

import (
	"encoding/json"
	"fmt"

	"github.com/KTCrisis/scout7/mesh"
)

// ChatMessage is a single message in a conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatResponse is what ollama.chat returns.
type ChatResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

// Chat sends messages to ollama via agent-mesh and returns the response text.
func Chat(mc *mesh.Client, model string, messages []ChatMessage) (string, error) {
	// Convert to generic interface for JSON.
	msgs := make([]map[string]any, len(messages))
	for i, m := range messages {
		msgs[i] = map[string]any{"role": m.Role, "content": m.Content}
	}

	tr, err := mc.CallTool("ollama.chat", map[string]any{
		"model":    model,
		"messages": msgs,
	})
	if err != nil {
		return "", fmt.Errorf("chat: %w", err)
	}

	var resp ChatResponse
	if err := json.Unmarshal(tr.Result, &resp); err != nil {
		return "", fmt.Errorf("parse chat response: %w", err)
	}

	if len(resp.Content) == 0 {
		return "", fmt.Errorf("empty chat response")
	}

	return resp.Content[0].Text, nil
}

// ChatJSON sends messages and unmarshals the response as JSON into target.
func ChatJSON[T any](mc *mesh.Client, model string, messages []ChatMessage) (T, error) {
	var zero T
	text, err := Chat(mc, model, messages)
	if err != nil {
		return zero, err
	}

	// Try to extract JSON from the response (may be wrapped in markdown code blocks).
	cleaned := extractJSON(text)

	var result T
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return zero, fmt.Errorf("parse JSON from LLM (raw: %s): %w", text, err)
	}
	return result, nil
}

// extractJSON tries to pull a JSON object/array from text that may contain markdown fences.
func extractJSON(s string) string {
	// Look for ```json ... ``` blocks.
	start := -1
	for i := 0; i < len(s)-6; i++ {
		if s[i:i+7] == "```json" {
			start = i + 7
			// Skip whitespace/newline after ```json.
			for start < len(s) && (s[start] == '\n' || s[start] == '\r' || s[start] == ' ') {
				start++
			}
			break
		}
		if s[i:i+3] == "```" && start == -1 {
			start = i + 3
			for start < len(s) && (s[start] == '\n' || s[start] == '\r' || s[start] == ' ') {
				start++
			}
		}
	}

	if start >= 0 {
		end := len(s)
		for i := start; i < len(s)-2; i++ {
			if s[i:i+3] == "```" {
				end = i
				break
			}
		}
		return s[start:end]
	}

	// No fences — find first { or [.
	for i, c := range s {
		if c == '{' || c == '[' {
			return s[i:]
		}
	}

	return s
}
