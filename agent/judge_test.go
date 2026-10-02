package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	scout7 "github.com/KTCrisis/scout7"
)

// answers as Jev returned them on the bench (s16, 02/10/2026)
const jevAnswers = `{"model":"jev-1.13.0","answers":{
 "describes_architecture":{"type":"noul","noul":0.92},
 "product_listing":{"type":"noul","noul":0.04},
 "novelty":{"type":"score","score":1.12,"probabilities":{"0":0.05,"1":0.78,"2":0.15,"3":0.02}},
 "category":{"type":"choice","probabilities":{"framework":0.1,"pattern":0.2,"infrastructure":0.6,"research":0.05,"product":0.05}}}}`

func cfgFor(url string) scout7.JudgeConfig {
	return scout7.JudgeConfig{Provider: "jev", Backend: "cloudflare", URL: url,
		APIKeyEnv: "TEST_JUDGE_TOKEN", AccountIDEnv: "TEST_JUDGE_ACCOUNT",
		KeepMin: 0.5, ListingMax: 0.5, NoveltyMin: 1.0, StateChars: 8000, Timeout: 5 * time.Second}
}

func TestJudgeCloudflareEnvelope(t *testing.T) {
	t.Setenv("TEST_JUDGE_TOKEN", "secret")
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("authorization header: %q", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		io.WriteString(w, `{"result":{"state":"Completed","result":`+jevAnswers+`},"success":true}`)
	}))
	defer srv.Close()

	v, err := NewJudge(cfgFor(srv.URL)).Judge("an article", "a title", []string{"seen-arch"})
	if err != nil {
		t.Fatal(err)
	}
	if got["model"] != "typesafe/jev" || got["input"] == nil {
		t.Errorf("cloudflare body: %v", got)
	}
	if v.Describes != 0.92 || v.Listing != 0.04 || v.Novelty != 1.12 || v.Category != "infrastructure" || v.Model != "jev-1.13.0" {
		t.Errorf("verdict: %+v", v)
	}
	cfg := cfgFor("")
	if !v.Keep(cfg) {
		t.Error("an architecture page should be kept")
	}
	ev := v.Evaluation(cfg)
	if !ev.DiagramIt || ev.Score != 4 {
		t.Errorf("evaluation: %+v (novelty 1.12 >= 1.0 draws a diagram; 1.12/3*10 rounds to 4)", ev)
	}
}

func TestJudgeDropsListings(t *testing.T) {
	cfg := cfgFor("")
	for _, v := range []Verdict{
		{Describes: 0.25, Listing: 0.90}, // a ranking of tools
		{Describes: 0.73, Listing: 0.63}, // a framework guide that is mostly a comparison
		{Describes: 0.12, Listing: 0.15}, // a definition page
	} {
		if v.Keep(cfg) {
			t.Errorf("should be dropped: %+v", v)
		}
	}
	rehash := Verdict{Describes: 0.72, Listing: 0.05, Novelty: 0.26}
	if !rehash.Keep(cfg) || rehash.Evaluation(cfg).DiagramIt {
		t.Error("a known pattern is kept but not drawn")
	}
}

func TestJudgeLocalBackendFlatBody(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("a local server takes no key")
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		io.WriteString(w, jevAnswers)
	}))
	defer srv.Close()
	cfg := cfgFor(srv.URL)
	cfg.Backend = "local"
	if _, err := NewJudge(cfg).Judge("x", "t", nil); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "nimble" || got["state"] == nil || got["questions"] == nil {
		t.Errorf("local body: %v", got)
	}
}

func TestJudgeFailures(t *testing.T) {
	// missing key: fails before any request, so the loop falls back to the LLM
	if _, err := NewJudge(cfgFor("http://127.0.0.1:1")).Judge("x", "t", nil); err == nil ||
		!strings.Contains(err.Error(), "TEST_JUDGE_TOKEN") {
		t.Errorf("missing key: %v", err)
	}
	t.Setenv("TEST_JUDGE_TOKEN", "secret")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"errors":[{"message":"bad input"}]}`)
	}))
	defer srv.Close()
	if _, err := NewJudge(cfgFor(srv.URL)).Judge("x", "t", nil); err == nil {
		t.Error("HTTP 400 should fail")
	}
	if _, err := parseVerdict(map[string]any{"answers": map[string]any{}}); err == nil {
		t.Error("answers without the questions should fail")
	}
}

func TestNoJudgeWithoutProvider(t *testing.T) {
	if NewJudge(scout7.JudgeConfig{}) != nil {
		t.Error("an empty provider leaves the LLM in charge")
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("éèà", 2); got != "éè" {
		t.Errorf("got %q", got)
	}
	if got := truncateRunes("abc", 10); got != "abc" {
		t.Errorf("got %q", got)
	}
}
