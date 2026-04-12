// Package mesh provides an HTTP client for agent-mesh tool calls.
package mesh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client calls tools via the agent-mesh HTTP API.
type Client struct {
	baseURL    string
	agentID    string
	sessionID  string
	httpClient *http.Client
}

// NewClient creates a mesh client pointing at the given agent-mesh URL.
func NewClient(baseURL, agentID, sessionID string) *Client {
	return &Client{
		baseURL:   baseURL,
		agentID:   agentID,
		sessionID: sessionID,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// ToolResult is the response from a tool call.
type ToolResult struct {
	Result    json.RawMessage `json:"result"`
	TraceID   string          `json:"trace_id"`
	Policy    string          `json:"policy"`
	LatencyMs int             `json:"latency_ms"`
	Error     string          `json:"error"`
}

// CallTool invokes a tool through agent-mesh.
func (c *Client) CallTool(tool string, params map[string]any) (*ToolResult, error) {
	envelope := map[string]any{"params": params}
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}

	url := fmt.Sprintf("%s/tool/%s", c.baseURL, tool)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer agent:"+c.agentID)
	if c.sessionID != "" {
		req.Header.Set("X-Session-Id", c.sessionID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("denied by policy: %s", string(data))
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rate limited: %s", string(data))
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("tool call failed (%d): %s", resp.StatusCode, string(data))
	}

	var result ToolResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if result.Error != "" {
		return &result, fmt.Errorf("tool error: %s", result.Error)
	}

	return &result, nil
}

// ResultAs unmarshals a ToolResult's Result field into the given target.
func ResultAs[T any](tr *ToolResult) (T, error) {
	var v T
	if err := json.Unmarshal(tr.Result, &v); err != nil {
		return v, fmt.Errorf("unmarshal result: %w", err)
	}
	return v, nil
}
