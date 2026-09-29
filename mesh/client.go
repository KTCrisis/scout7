// Package mesh provides a client for tool calls through flux7-mesh, directly
// or behind a gateway: over its REST API or over MCP, with a declared agent
// name or an OAuth token.
package mesh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// transport is how a tool call reaches the mesh: REST (POST /tool/<name>) or
// MCP (JSON-RPC on one endpoint). Both return the same ToolResult.
type transport interface {
	callTool(tool string, params map[string]any) (*ToolResult, error)
}

// Client calls tools via flux7-mesh. The agent only sees CallTool; the
// transport and the credential are chosen once, at construction.
type Client struct {
	t transport
}

// Options chooses the transport and the credential.
type Options struct {
	URL       string      // mesh base URL (REST) or MCP endpoint, e.g. http://localhost:8010/mcp
	Transport string      // "rest" (default) or "mcp"
	Tokens    TokenSource // default: AgentName(AgentID)
	AgentID   string
	SessionID string // REST only: sent as X-Session-Id
}

// New builds a client from options.
func New(o Options) (*Client, error) {
	tokens := o.Tokens
	if tokens == nil {
		tokens = AgentName(o.AgentID)
	}
	httpClient := &http.Client{Timeout: 120 * time.Second}
	switch o.Transport {
	case "", "rest":
		return &Client{t: &restTransport{baseURL: o.URL, sessionID: o.SessionID, tokens: tokens, http: httpClient}}, nil
	case "mcp":
		return &Client{t: &mcpTransport{endpoint: o.URL, tokens: tokens, http: httpClient}}, nil
	default:
		return nil, fmt.Errorf("unknown mesh transport %q (rest or mcp)", o.Transport)
	}
}

// NewClient creates a REST client that declares its agent name, as scout7
// always did.
func NewClient(baseURL, agentID, sessionID string) *Client {
	c, _ := New(Options{URL: baseURL, AgentID: agentID, SessionID: sessionID})
	return c
}

// ToolResult is the response from a tool call.
type ToolResult struct {
	Result    json.RawMessage `json:"result"`
	TraceID   string          `json:"trace_id"`
	Policy    string          `json:"policy"`
	LatencyMs int             `json:"latency_ms"`
	Error     string          `json:"error"`
}

// CallTool invokes a tool through flux7-mesh.
func (c *Client) CallTool(tool string, params map[string]any) (*ToolResult, error) {
	return c.t.callTool(tool, params)
}

// restTransport is the mesh's REST data plane: POST {baseURL}/tool/{tool}
// with {"params": ...}.
type restTransport struct {
	baseURL   string
	sessionID string
	tokens    TokenSource
	http      *http.Client
}

func (r *restTransport) callTool(tool string, params map[string]any) (*ToolResult, error) {
	envelope := map[string]any{"params": params}
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}

	url := fmt.Sprintf("%s/tool/%s", r.baseURL, tool)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	token, err := r.tokens.Token()
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if r.sessionID != "" {
		req.Header.Set("X-Session-Id", r.sessionID)
	}

	resp, err := r.http.Do(req)
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

// Compile-time checks that both transports satisfy the interface.
var (
	_ transport = (*restTransport)(nil)
	_ transport = (*mcpTransport)(nil)
)
