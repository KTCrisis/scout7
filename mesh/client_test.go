package mesh

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// The REST transport with a declared name is what scout7 always sent.
func TestRESTKeepsTheOldRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tool/memory.memory_list" || r.Header.Get("Authorization") != "Bearer agent:scout7" ||
			r.Header.Get("X-Session-Id") != "s1" {
			t.Errorf("unexpected request %s %q %q", r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-Session-Id"))
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["params"]; !ok {
			t.Errorf("params envelope missing: %v", body)
		}
		fmt.Fprint(w, `{"result":{"content":[{"type":"text","text":"[]"}]},"policy":"allow"}`)
	}))
	defer srv.Close()

	tr, err := NewClient(srv.URL, "scout7", "s1").CallTool("memory.memory_list", map[string]any{"limit": 5})
	if err != nil || !strings.Contains(string(tr.Result), `"content"`) {
		t.Fatalf("got %v, %v", tr, err)
	}
}

// fakeMesh answers MCP like mesh7: a session on initialize, and the upstream
// result serialized as text on tools/call.
type fakeMesh struct {
	inits    atomic.Int32
	forget   atomic.Bool // answer 404 once, as after a mesh restart
	lastAuth atomic.Value
	answer   string // text of the tools/call content
}

func (f *fakeMesh) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.lastAuth.Store(r.Header.Get("Authorization"))
	var req struct {
		ID     int            `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if r.Header.Get("Mcp-Method") != req.Method {
		http.Error(w, "Mcp-Method does not match the body", http.StatusBadRequest)
		return
	}
	switch req.Method {
	case "initialize":
		n := f.inits.Add(1)
		w.Header().Set("Mcp-Session-Id", fmt.Sprintf("sess-%d", n))
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2025-11-25"}}`, req.ID)
	case "tools/call":
		if r.Header.Get("Mcp-Session-Id") == "" || r.Header.Get("Mcp-Name") != req.Params["name"] {
			http.Error(w, "missing session or Mcp-Name", http.StatusBadRequest)
			return
		}
		if f.forget.CompareAndSwap(true, false) {
			http.Error(w, `{"error":"Unknown session"}`, http.StatusNotFound)
			return
		}
		text, _ := json.Marshal(f.answer)
		// as an event stream, which Streamable HTTP allows
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"content\":[{\"type\":\"text\",\"text\":%s}]}}\n\n", req.ID, text)
	}
}

func mcpClient(t *testing.T, f *fakeMesh, tokens TokenSource) (*Client, func()) {
	srv := httptest.NewServer(f)
	c, err := New(Options{URL: srv.URL + "/mcp", Transport: "mcp", AgentID: "scout7", Tokens: tokens})
	if err != nil {
		t.Fatal(err)
	}
	return c, srv.Close
}

// Over MCP the agent reads the same Result as over REST: mesh7's text is the
// upstream result, unwrapped.
func TestMCPUnwrapsTheUpstreamResult(t *testing.T) {
	f := &fakeMesh{answer: `{"content":[{"type":"text","text":"hello"}]}`}
	c, stop := mcpClient(t, f, nil)
	defer stop()

	tr, err := c.CallTool("ollama.chat", map[string]any{"model": "m"})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Content []struct{ Text string } `json:"content"`
	}
	if err := json.Unmarshal(tr.Result, &got); err != nil || got.Content[0].Text != "hello" {
		t.Fatalf("result not unwrapped: %s (%v)", tr.Result, err)
	}
	if a := f.lastAuth.Load(); a != "Bearer agent:scout7" {
		t.Fatalf("default credential: %v", a)
	}
	c.CallTool("ollama.chat", nil)
	if n := f.inits.Load(); n != 1 {
		t.Fatalf("one session for two calls, got %d initialize", n)
	}
}

// A call waiting for a human is not the tool's output.
func TestMCPApprovalIsAnError(t *testing.T) {
	f := &fakeMesh{answer: "Approval required (id: 4935d1b1) for memory.memory_store, valid 299s.\nAsk the user..."}
	c, stop := mcpClient(t, f, nil)
	defer stop()

	_, err := c.CallTool("memory.memory_store", nil)
	if err == nil || !strings.Contains(err.Error(), "pending human approval") {
		t.Fatalf("expected a pending approval error, got %v", err)
	}
}

// After a mesh restart the session is gone: one fresh session, one retry.
func TestMCPReopensAForgottenSession(t *testing.T) {
	f := &fakeMesh{answer: `{"content":[]}`}
	c, stop := mcpClient(t, f, nil)
	defer stop()
	c.CallTool("memory.memory_list", nil)
	f.forget.Store(true)

	if _, err := c.CallTool("memory.memory_list", nil); err != nil {
		t.Fatalf("retry on a new session failed: %v", err)
	}
	if n := f.inits.Load(); n != 2 {
		t.Fatalf("expected a second initialize, got %d", n)
	}
}

// A gateway refusal reads as a refusal, not as a parse error.
func TestMCPForbiddenIsDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Mcp-Method") == "initialize" {
			w.Header().Set("Mcp-Session-Id", "s")
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
			return
		}
		http.Error(w, "<html>403 Forbidden</html>", http.StatusForbidden)
	}))
	defer srv.Close()
	c, _ := New(Options{URL: srv.URL, Transport: "mcp", AgentID: "scout7"})

	_, err := c.CallTool("memory.memory_store", nil)
	if err == nil || !strings.Contains(err.Error(), "denied by policy (403)") {
		t.Fatalf("expected a 403 refusal, got %v", err)
	}
}

// The OAuth token is fetched once, sent as the bearer, and reused while valid.
func TestClientCredentialsFetchesOnceAndSends(t *testing.T) {
	var fetched atomic.Int32
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_secret") != "s3cret" ||
			r.Form.Get("scope") != "web:read memory:write" {
			http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
			return
		}
		fetched.Add(1)
		fmt.Fprint(w, `{"access_token":"eyJ.token","expires_in":300}`)
	}))
	defer idp.Close()

	t.Setenv("SCOUT7_TEST_SECRET", "s3cret")
	cc, err := NewClientCredentials(idp.URL, "scout7", "SCOUT7_TEST_SECRET", []string{"web:read", "memory:write"})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeMesh{answer: `{"content":[]}`}
	c, stop := mcpClient(t, f, cc)
	defer stop()
	c.CallTool("memory.memory_list", nil)
	c.CallTool("memory.memory_list", nil)

	if a := f.lastAuth.Load(); a != "Bearer eyJ.token" {
		t.Fatalf("bearer not the OAuth token: %v", a)
	}
	if n := fetched.Load(); n != 1 {
		t.Fatalf("token fetched %d times for one session and two calls", n)
	}
}

// A missing secret fails at start, with the variable's name.
func TestClientCredentialsNeedsTheSecret(t *testing.T) {
	t.Setenv("SCOUT7_ABSENT", "")
	_, err := NewClientCredentials("http://idp", "scout7", "SCOUT7_ABSENT", nil)
	if err == nil || !strings.Contains(err.Error(), "SCOUT7_ABSENT") {
		t.Fatalf("expected an error naming the variable, got %v", err)
	}
}
