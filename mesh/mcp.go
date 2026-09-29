package mesh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// mcpTransport calls tools over MCP Streamable HTTP: one POST per JSON-RPC
// message to a single endpoint (mesh7's /mcp, or a gateway in front of it such
// as Kong's ai-mcp-proxy). It opens the session lazily on the first call and
// opens a new one if the server has forgotten it.
type mcpTransport struct {
	endpoint string
	tokens   TokenSource
	http     *http.Client

	mu      sync.Mutex // one call at a time: the session and the request ids
	session string
	nextID  int
}

const mcpProtocol = "2025-11-25"

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

// callTool implements transport.
func (t *mcpTransport) callTool(tool string, params map[string]any) (*ToolResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.session == "" {
		if err := t.initialize(); err != nil {
			return nil, err
		}
	}
	res, status, err := t.post("tools/call", tool, map[string]any{"name": tool, "arguments": params})
	if status == http.StatusNotFound { // session expired on the server: once more, fresh
		t.session = ""
		if err := t.initialize(); err != nil {
			return nil, err
		}
		res, _, err = t.post("tools/call", tool, map[string]any{"name": tool, "arguments": params})
	}
	if err != nil {
		return nil, err
	}
	return toolResult(res)
}

func (t *mcpTransport) initialize() error {
	_, _, err := t.post("initialize", "", map[string]any{
		"protocolVersion": mcpProtocol,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "scout7", "version": "1"},
	})
	if err != nil {
		return fmt.Errorf("mcp initialize: %w", err)
	}
	if t.session == "" {
		return fmt.Errorf("mcp initialize: no Mcp-Session-Id in the answer")
	}
	return nil
}

// post sends one JSON-RPC request and returns its result, the HTTP status, and
// an error for anything that is not a result: transport, HTTP or JSON-RPC.
func (t *mcpTransport) post(method, name string, params map[string]any) (json.RawMessage, int, error) {
	t.nextID++
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": t.nextID, "method": method, "params": params})
	if err != nil {
		return nil, 0, fmt.Errorf("marshal %s: %w", method, err)
	}
	req, err := http.NewRequest(http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	token, err := t.tokens.Token()
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	// MCP 2026-07-28 repeats the method and the tool in headers, so a gateway
	// can route or refuse without parsing the body.
	req.Header.Set("Mcp-Method", method)
	if name != "" {
		req.Header.Set("Mcp-Name", name)
	}
	if t.session != "" {
		req.Header.Set("Mcp-Session-Id", t.session)
	}

	resp, err := t.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.session = sid
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, resp.StatusCode, fmt.Errorf("not authenticated (401): %s", snippet(data))
	case resp.StatusCode == http.StatusForbidden:
		return nil, resp.StatusCode, fmt.Errorf("denied by policy (403): %s", snippet(data))
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, resp.StatusCode, fmt.Errorf("rate limited: %s", snippet(data))
	case resp.StatusCode >= 400:
		return nil, resp.StatusCode, fmt.Errorf("mcp %s failed (%d): %s", method, resp.StatusCode, snippet(data))
	}

	var rpc rpcResponse
	if err := json.Unmarshal(sseData(data), &rpc); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("parse %s answer: %w", method, err)
	}
	if rpc.Error != nil {
		return nil, resp.StatusCode, fmt.Errorf("mcp %s: %s (%d)", method, rpc.Error.Message, rpc.Error.Code)
	}
	return rpc.Result, resp.StatusCode, nil
}

// toolResult turns a tools/call result into the ToolResult the REST transport
// gives, so the agent reads both the same way. mesh7 returns the upstream
// tool's MCP result serialized as one text block; when that text is JSON, it
// is the upstream result itself and becomes Result as is.
func toolResult(res json.RawMessage) (*ToolResult, error) {
	var call struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(res, &call); err != nil {
		return nil, fmt.Errorf("parse tools/call result: %w", err)
	}
	var text strings.Builder
	for _, c := range call.Content {
		text.WriteString(c.Text)
	}
	s := text.String()
	if call.IsError {
		return nil, fmt.Errorf("tool error: %s", s)
	}
	// mesh7 answers a call that waits for a human with text, not an error:
	// the agent must not read it as the tool's output.
	if strings.HasPrefix(s, "Approval required") {
		return nil, fmt.Errorf("%w: %s", ErrPendingApproval, firstLine(s))
	}
	if strings.HasPrefix(s, "Policy denied") || strings.HasPrefix(s, "Backend error") {
		return nil, fmt.Errorf("%s", firstLine(s))
	}
	if json.Valid([]byte(s)) {
		// The upstream's own isError travels inside the serialized result:
		// a failed fetch must not reach the agent as page content.
		var inner struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal([]byte(s), &inner) == nil && inner.IsError {
			msg := ""
			for _, c := range inner.Content {
				msg += c.Text
			}
			return nil, fmt.Errorf("tool error: %s", firstLine(msg))
		}
		return &ToolResult{Result: json.RawMessage(s), Policy: "allow"}, nil
	}
	// Plain text: hand it over in the MCP content shape the agent already reads.
	wrapped, _ := json.Marshal(map[string]any{"content": []map[string]string{{"type": "text", "text": s}}})
	return &ToolResult{Result: wrapped, Policy: "allow"}, nil
}

// sseData returns the JSON of the first "data:" line when the server answered
// as an event stream, and the body unchanged otherwise.
func sseData(body []byte) []byte {
	trimmed := bytes.TrimSpace(body)
	if !bytes.HasPrefix(trimmed, []byte("event:")) && !bytes.HasPrefix(trimmed, []byte("data:")) {
		return body
	}
	for _, line := range strings.Split(string(trimmed), "\n") {
		if strings.HasPrefix(line, "data:") {
			return []byte(strings.TrimSpace(line[len("data:"):]))
		}
	}
	return body
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
