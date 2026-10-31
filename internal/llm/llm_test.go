package llm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamingChatAndToolCalls(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing auth header")
		}
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`data: {"model":"test-model","choices":[{"delta":{"content":"Hello "},"finish_reason":""}]}`,
			`data: {"model":"test-model","choices":[{"delta":{"content":"world"},"finish_reason":""}]}`,
			`data: {"model":"test-model","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"fs__list","arguments":"{\"path\":"}}]},"finish_reason":""}]}`,
			`data: {"model":"test-model","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\".\"}"}}]},"finish_reason":""}]}`,
			`data: {"model":"test-model","choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`data: {"model":"test-model","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
			`data: [DONE]`,
		}
		for _, c := range chunks {
			fmt.Fprint(w, c+"\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	c := NewClient()
	var deltas []string
	res, err := c.Chat(context.Background(), Profile{
		BaseURL: srv.URL + "/v1", Model: "test-model", APIKey: "test-key",
	}, ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Tools:    []ToolDef{{Type: "function", Function: FunctionDef{Name: "fs__list"}}},
	}, func(ev StreamEvent) {
		if ev.TextDelta != "" {
			deltas = append(deltas, ev.TextDelta)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Message.Content != "Hello world" {
		t.Fatalf("content %q", res.Message.Content)
	}
	if len(res.Message.ToolCalls) != 1 || res.Message.ToolCalls[0].Function.Arguments != `{"path":"."}` {
		t.Fatalf("tool calls %+v", res.Message.ToolCalls)
	}
	if res.Usage.TotalTokens != 15 {
		t.Fatalf("usage %+v", res.Usage)
	}
	if strings.Join(deltas, "") != "Hello world" {
		t.Fatalf("deltas %+v", deltas)
	}
	if !strings.Contains(gotBody, `"stream":true`) || !strings.Contains(gotBody, `"tools"`) {
		t.Fatalf("body %s", gotBody)
	}
}

func TestSessionHeader(t *testing.T) {
	var session, static string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session = r.Header.Get("x-opencode-session")
		static = r.Header.Get("x-static")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	c := NewClient()
	_, err := c.Chat(context.Background(), Profile{
		BaseURL: srv.URL, Model: "m",
		Params: map[string]any{
			"session_header": "x-opencode-session",
			"headers":        map[string]any{"x-opencode-session": "static-value", "x-static": "yes"},
		},
	}, ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}}, SessionID: "conv-123",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if session != "conv-123" {
		t.Fatalf("session header = %q, want per-conversation value", session)
	}
	if static != "yes" {
		t.Fatalf("static header missing: %q", static)
	}
}

func TestSessionHeaderAutoDetect(t *testing.T) {
	if got := sessionHeaderName(Profile{BaseURL: "https://opencode.ai/zen/go/v1"}); got != "x-opencode-session" {
		t.Fatalf("opencode auto header = %q", got)
	}
	if got := sessionHeaderName(Profile{BaseURL: "https://api.example.com/v1"}); got != "" {
		t.Fatalf("non-opencode header = %q", got)
	}
	if got := sessionHeaderName(Profile{BaseURL: "https://opencode.ai/zen/go/v1", Params: map[string]any{"session_header": "x-custom-session"}}); got != "x-custom-session" {
		t.Fatalf("explicit header = %q", got)
	}
}

func TestProviderErrorRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"rate limited"}}`, 429)
	}))
	defer srv.Close()
	c := NewClient()
	_, err := c.Chat(context.Background(), Profile{BaseURL: srv.URL, Model: "x"}, ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsRetryable(err) {
		t.Fatalf("429 should be retryable: %v", err)
	}
}

func TestMockDemoCallsTools(t *testing.T) {
	c := NewClient()
	prof := Profile{BaseURL: "mock://demo", Model: "demo"}
	tools := []ToolDef{
		{Type: "function", Function: FunctionDef{Name: "fs__list"}},
		{Type: "function", Function: FunctionDef{Name: "run_workflow"}},
	}
	res, err := c.Chat(context.Background(), prof, ChatRequest{
		Messages: []Message{{Role: "user", Content: "Summarize the files in my workspace"}},
		Tools:    tools,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Message.ToolCalls) != 1 || res.Message.ToolCalls[0].Function.Name != "fs__list" {
		t.Fatalf("expected fs__list call, got %+v", res.Message.ToolCalls)
	}
}

func TestCost(t *testing.T) {
	p := Profile{Params: map[string]any{"input_cost_per_mtok": float64(2), "output_cost_per_mtok": float64(4)}}
	cost := CostMicro(p, Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000})
	if cost != 6_000_000 {
		t.Fatalf("cost %d", cost)
	}
}
