// Package mesh provides a client for tool calls through flux7-mesh, directly
// or behind a gateway: over its REST API or over MCP, with a declared agent
// name or an OAuth token.
package mesh

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"time"
)

// ErrPendingApproval is the error of a call the mesh holds until a human
// decides. errors.Is recognises it whatever the message around it.
var ErrPendingApproval = errors.New("pending human approval")

// transport is how a tool call reaches the mesh: REST (POST /tool/<name>) or
// MCP (JSON-RPC on one endpoint). Both return the same ToolResult.
type transport interface {
	callTool(tool string, params map[string]any) (*ToolResult, error)
}

// Client calls tools via flux7-mesh. The agent only sees CallTool; the
// transport and the credential are chosen once, at construction.
type Client struct {
	t transport
	// approvalWait: how long a call held for a human is retried before
	// giving up; zero gives up at once, as before.
	approvalWait time.Duration
	approvalPoll time.Duration
}

// Options chooses the transport and the credential.
type Options struct {
	URL       string      // mesh base URL (REST) or MCP endpoint, e.g. http://localhost:8010/mcp
	Transport string      // "rest" (default) or "mcp"
	Tokens    TokenSource // default: AgentName(AgentID)
	AgentID   string
	SessionID string // REST only: sent as X-Session-Id
	// ApprovalWait: a call held for a human is retried (the mesh answers the
	// same approval each time) until approved, refused, or this long.
	ApprovalWait time.Duration
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
		return &Client{t: &restTransport{baseURL: o.URL, sessionID: o.SessionID, tokens: tokens, http: httpClient},
			approvalWait: o.ApprovalWait, approvalPoll: 3 * time.Second}, nil
	case "mcp":
		return &Client{t: &mcpTransport{endpoint: o.URL, tokens: tokens, http: httpClient},
			approvalWait: o.ApprovalWait, approvalPoll: 3 * time.Second}, nil
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

var approvalID = regexp.MustCompile(`id: ([0-9a-f]+)`)

// CallTool invokes a tool through flux7-mesh. A call held for a human is
// retried while approvalWait allows: once approved, the retry runs.
func (c *Client) CallTool(tool string, params map[string]any) (*ToolResult, error) {
	tr, err := c.t.callTool(tool, params)
	if c.approvalWait <= 0 || !errors.Is(err, ErrPendingApproval) {
		return tr, err
	}
	id := ""
	if m := approvalID.FindStringSubmatch(err.Error()); m != nil {
		id = m[1]
	}
	slog.Info("waiting for approval", "tool", tool, "approval", id, "max", c.approvalWait)
	deadline := time.Now().Add(c.approvalWait)
	for time.Now().Before(deadline) {
		time.Sleep(c.approvalPoll)
		tr, err = c.t.callTool(tool, params)
		if !errors.Is(err, ErrPendingApproval) {
			if err == nil {
				slog.Info("approved", "tool", tool, "approval", id)
			}
			return tr, err
		}
	}
	return nil, fmt.Errorf("no decision within %s: %w", c.approvalWait, err)
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
