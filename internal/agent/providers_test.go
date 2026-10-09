package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testKey = "sk" + "-ant-TESTKEYTESTKEYTESTKEY1234567890"

func TestAnthropicRequestAndResponse(t *testing.T) {
	var got map[string]any
	var gotKey, gotVer, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotVer, path = r.Header.Get("x-api-key"), r.Header.Get("anthropic-version"), r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		io.WriteString(w, `{"content":[{"type":"text","text":"hello"},{"type":"tool_use","id":"tu_1","name":"list_nodes","input":{}}],"usage":{"input_tokens":11,"output_tokens":7}}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)
	llm, err := NewLLM(ProviderConfig{Kind: "anthropic", Model: "m", BaseURL: srv.URL, APIKeyEnv: "TEST_KEY"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := llm.Complete(context.Background(), Request{System: "sys", MaxTokens: 100,
		Messages: []Message{{Role: "user", Content: "hi"}, {Role: "assistant", ToolCalls: []ToolCall{{ID: "tu_0", Name: "list_nodes", Args: json.RawMessage(`{}`)}}}, {Role: "tool", ToolCallID: "tu_0", Content: "result"}},
		Tools:    []ToolSpec{{Name: "list_nodes", Description: "d", Schema: map[string]any{"type": "object"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1/messages" || gotKey != testKey || gotVer == "" {
		t.Fatalf("path %q key ok=%v version %q", path, gotKey == testKey, gotVer)
	}
	if resp.Text != "hello" || len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "tu_1" || resp.InputTokens != 11 || resp.OutputTokens != 7 {
		t.Fatalf("%+v", resp)
	}
	b, _ := json.Marshal(got)
	for _, want := range []string{`"system":"sys"`, `"tool_result"`, `"tool_use_id":"tu_0"`, `"input_schema"`, `"max_tokens":100`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("request lacks %s: %s", want, b)
		}
	}
}

func TestOpenAICompatibleRequestAndResponse(t *testing.T) {
	var got map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		io.WriteString(w, `{"choices":[{"message":{"content":"hi","tool_calls":[{"id":"c1","function":{"name":"scan_workloads","arguments":"{\"severity\":\"high\"}"}}]}}],"usage":{"prompt_tokens":5,"completion_tokens":3}}`)
	}))
	defer srv.Close()
	llm, err := NewLLM(ProviderConfig{Kind: "openai-compatible", Model: "llama", BaseURL: srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := llm.Complete(context.Background(), Request{System: "sys", MaxTokens: 50, Messages: []Message{{Role: "user", Content: "q"}},
		Tools: []ToolSpec{{Name: "scan_workloads", Description: "d", Schema: map[string]any{"type": "object"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		t.Fatalf("an Authorization header was sent without a configured key: %q", auth)
	}
	if resp.Text != "hi" || len(resp.ToolCalls) != 1 || string(resp.ToolCalls[0].Args) != `{"severity":"high"}` || resp.InputTokens != 5 {
		t.Fatalf("%+v", resp)
	}
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"role":"system"`) || !strings.Contains(string(b), `"type":"function"`) {
		t.Fatalf("%s", b)
	}
}

func TestProviderErrorsNeverLeakTheKeyOrBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		io.WriteString(w, `{"error":"invalid x-api-key `+r.Header.Get("x-api-key")+`"}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)
	llm, _ := NewLLM(ProviderConfig{Kind: "anthropic", Model: "m", BaseURL: srv.URL, APIKeyEnv: "TEST_KEY"})
	_, err := llm.Complete(context.Background(), Request{MaxTokens: 10, Messages: []Message{{Role: "user", Content: "x"}}})
	if err == nil || strings.Contains(err.Error(), testKey) || strings.Contains(err.Error(), "invalid x-api-key") {
		t.Fatalf("error leaks provider text or key: %v", err)
	}
	// Transport errors must not carry the URL (it could hold a key in a query string).
	dead, _ := NewLLM(ProviderConfig{Kind: "openai-compatible", Model: "m", BaseURL: "http://127.0.0.1:1/v1?key=SECRETINURL"})
	_, err = dead.Complete(context.Background(), Request{MaxTokens: 10, Messages: []Message{{Role: "user", Content: "x"}}})
	if err == nil || strings.Contains(err.Error(), "SECRETINURL") {
		t.Fatalf("transport error leaks the URL: %v", err)
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("redirect target was contacted")
		w.WriteHeader(200)
	}))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)
	llm, _ := NewLLM(ProviderConfig{Kind: "anthropic", Model: "m", BaseURL: srv.URL, APIKeyEnv: "TEST_KEY"})
	if _, err := llm.Complete(context.Background(), Request{MaxTokens: 10, Messages: []Message{{Role: "user", Content: "x"}}}); err == nil {
		t.Fatal("redirect treated as success")
	}
}

func TestOversizedAndMalformedResponsesAreRejected(t *testing.T) {
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, strings.Repeat("x", maxResponseBytes+10))
	}))
	defer big.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "not json") }))
	defer bad.Close()
	for _, u := range []string{big.URL, bad.URL} {
		llm, _ := NewLLM(ProviderConfig{Kind: "openai-compatible", Model: "m", BaseURL: u})
		if _, err := llm.Complete(context.Background(), Request{MaxTokens: 10, Messages: []Message{{Role: "user", Content: "x"}}}); err == nil {
			t.Fatalf("accepted a bad response from %s", u)
		}
	}
}

func TestConfigurationIsValidated(t *testing.T) {
	t.Setenv("EMPTY_KEY", "")
	cases := []ProviderConfig{
		{Kind: "anthropic", Model: "m", APIKeyEnv: "EMPTY_KEY"},                                   // missing key
		{Kind: "anthropic", Model: "", APIKeyEnv: "EMPTY_KEY"},                                    // missing model
		{Kind: "openai-compatible", Model: "m"},                                                   // missing base url
		{Kind: "openai-compatible", Model: "m", BaseURL: "http://models.example.com/v1"},          // plain http to a remote host
		{Kind: "openai-compatible", Model: "m", BaseURL: "https://user:pw@models.example.com/v1"}, // credentials in the URL
		{Kind: "openai-compatible", Model: "m", BaseURL: "file:///etc/passwd"},                    // wrong scheme
		{Kind: "other", Model: "m"},                                                               // unknown kind
	}
	for _, c := range cases {
		if _, err := NewLLM(c); err == nil {
			t.Errorf("accepted %+v", c)
		}
	}
	for _, ok := range []ProviderConfig{
		{Kind: "openai-compatible", Model: "m", BaseURL: "http://localhost:11434/v1"},
		{Kind: "openai-compatible", Model: "m", BaseURL: "http://127.0.0.1:8000/v1"},
		{Kind: "openai-compatible", Model: "m", BaseURL: "https://models.example.com/v1"},
		{Kind: "openai-compatible", Model: "m", BaseURL: "http://models.internal/v1", AllowInsecure: true},
	} {
		if _, err := NewLLM(ok); err != nil {
			t.Errorf("rejected %+v: %v", ok, err)
		}
	}
}
