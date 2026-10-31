package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id,omitempty"`
	Index    int          `json:"index,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolDef struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

type FunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Strict      bool           `json:"strict,omitempty"`
}

type ChatRequest struct {
	Messages     []Message
	Tools        []ToolDef
	Temperature  *float64
	MaxTokens    int
	OutputSchema map[string]any
	Stream       bool
	Extra        map[string]any
	// SessionID is a stable per-conversation or per-run identifier. Some
	// gateways (notably OpenCode Go) require it in a session header.
	SessionID string
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatResult struct {
	Message      Message
	Usage        Usage
	FinishReason string
	Model        string
}

type StreamEvent struct {
	TextDelta    string
	ToolCall     *ToolCall
	Usage        *Usage
	FinishReason string
}

type Profile struct {
	ID       string
	Name     string
	BaseURL  string
	Model    string
	Role     string
	Params   map[string]any
	APIKey   string
	SecretID string
}

type ProviderError struct {
	Status    int
	Message   string
	Retryable bool
}

func (e *ProviderError) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("model provider returned %d: %s", e.Status, e.Message)
	}
	return e.Message
}

func (e *ProviderError) RetryableError() bool { return e.Retryable }

type Client struct {
	HTTP *http.Client
}

func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 0}}
}

func IsMock(p Profile) bool {
	return strings.HasPrefix(p.BaseURL, "mock://") || p.Model == "demo" || strings.HasPrefix(p.BaseURL, "demo:")
}

func (c *Client) Chat(ctx context.Context, p Profile, req ChatRequest, onDelta func(StreamEvent)) (ChatResult, error) {
	if IsMock(p) {
		return c.mock(ctx, p, req, onDelta)
	}
	if strings.TrimSpace(p.BaseURL) == "" {
		return ChatResult{}, &ProviderError{Message: "model profile has no base URL", Retryable: false}
	}
	if strings.TrimSpace(p.Model) == "" {
		return ChatResult{}, &ProviderError{Message: "model profile has no model name", Retryable: false}
	}
	payload := map[string]any{
		"model":    p.Model,
		"messages": req.Messages,
		"stream":   true,
		"stream_options": map[string]any{
			"include_usage": true,
		},
	}
	if len(req.Tools) > 0 {
		payload["tools"] = req.Tools
		payload["tool_choice"] = "auto"
	}
	if req.Temperature != nil {
		payload["temperature"] = *req.Temperature
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if req.OutputSchema != nil {
		payload["response_format"] = map[string]any{"type": "json_object"}
	}
	for k, v := range req.Extra {
		payload[k] = v
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ChatResult{}, err
	}
	url := chatURL(p.BaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ChatResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if p.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	applyHeaders(httpReq, p.Params)
	if header := sessionHeaderName(p); header != "" && req.SessionID != "" {
		if !strings.ContainsAny(req.SessionID, "\r\n") {
			// The dynamic session value wins over a static headers entry.
			httpReq.Header.Set(header, req.SessionID)
		}
	}
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return ChatResult{}, &ProviderError{Message: err.Error(), Retryable: true}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		msg := strings.TrimSpace(string(raw))
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &apiErr) == nil && apiErr.Error.Message != "" {
			msg = apiErr.Error.Message
		}
		return ChatResult{}, &ProviderError{Status: resp.StatusCode, Message: msg, Retryable: retryableStatus(resp.StatusCode)}
	}
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		// Some OpenAI-compatible servers ignore stream=true. Handle a regular
		// JSON completion so the app still works with them.
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
		return parseCompletion(raw)
	}
	return readSSE(resp.Body, onDelta)
}

func applyHeaders(req *http.Request, params map[string]any) {
	headers, ok := params["headers"].(map[string]any)
	if !ok {
		return
	}
	for k, v := range headers {
		req.Header.Set(k, fmt.Sprint(v))
	}
}

// sessionHeaderName returns the configured header name, or the OpenCode Go
// default when the profile points at opencode.ai. A stable value is supplied
// per conversation/run by ChatRequest.SessionID.
func sessionHeaderName(p Profile) string {
	if v, ok := p.Params["session_header"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	u, err := url.Parse(strings.TrimSpace(p.BaseURL))
	if err == nil {
		host := strings.ToLower(u.Hostname())
		if host == "opencode.ai" || strings.HasSuffix(host, ".opencode.ai") {
			return "x-opencode-session"
		}
	}
	return ""
}

func chatURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(base, "/chat/completions") {
		return base
	}
	return base + "/chat/completions"
}

func retryableStatus(code int) bool {
	return code == 408 || code == 409 || code == 429 || code >= 500
}

func parseCompletion(raw []byte) (ChatResult, error) {
	var resp struct {
		Model   string `json:"model"`
		Choices []struct {
			Message      Message `json:"message"`
			FinishReason string  `json:"finish_reason"`
		} `json:"choices"`
		Usage Usage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ChatResult{}, &ProviderError{Message: "invalid completion response", Retryable: false}
	}
	if len(resp.Choices) == 0 {
		return ChatResult{}, &ProviderError{Message: "model returned no choices", Retryable: false}
	}
	return ChatResult{Message: resp.Choices[0].Message, Usage: resp.Usage, FinishReason: resp.Choices[0].FinishReason, Model: resp.Model}, nil
}

func readSSE(body io.Reader, onDelta func(StreamEvent)) (ChatResult, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var content strings.Builder
	toolCalls := map[int]*ToolCall{}
	var usage Usage
	var finishReason, model string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage *Usage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Model != "" {
			model = chunk.Model
		}
		if chunk.Usage != nil {
			usage = *chunk.Usage
			if onDelta != nil {
				onDelta(StreamEvent{Usage: chunk.Usage})
			}
		}
		for _, choice := range chunk.Choices {
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
			if choice.Delta.Content != "" {
				content.WriteString(choice.Delta.Content)
				if onDelta != nil {
					onDelta(StreamEvent{TextDelta: choice.Delta.Content})
				}
			}
			for _, tc := range choice.Delta.ToolCalls {
				idx := tc.Index
				cur := toolCalls[idx]
				if cur == nil {
					cur = &ToolCall{Index: idx, Type: "function"}
					toolCalls[idx] = cur
				}
				if tc.ID != "" {
					cur.ID = tc.ID
				}
				if tc.Type != "" {
					cur.Type = tc.Type
				}
				if tc.Function.Name != "" {
					cur.Function.Name += tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					cur.Function.Arguments += tc.Function.Arguments
				}
				if onDelta != nil {
					ev := StreamEvent{ToolCall: &ToolCall{Index: idx, ID: tc.ID, Type: tc.Type,
						Function: FunctionCall{Name: tc.Function.Name, Arguments: tc.Function.Arguments}}}
					onDelta(ev)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return ChatResult{}, &ProviderError{Message: err.Error(), Retryable: true}
	}
	keys := make([]int, 0, len(toolCalls))
	for k := range toolCalls {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	msg := Message{Role: "assistant", Content: content.String()}
	for _, k := range keys {
		tc := toolCalls[k]
		if tc.ID == "" {
			tc.ID = fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), k)
		}
		msg.ToolCalls = append(msg.ToolCalls, *tc)
	}
	if msg.Content == "" && len(msg.ToolCalls) == 0 && finishReason == "" {
		return ChatResult{}, &ProviderError{Message: "model stream ended without content (check the model and base URL)", Retryable: true}
	}
	return ChatResult{Message: msg, Usage: usage, FinishReason: finishReason, Model: model}, nil
}

func (c *Client) mock(ctx context.Context, p Profile, req ChatRequest, onDelta func(StreamEvent)) (ChatResult, error) {
	lastUser := ""
	for _, m := range req.Messages {
		if m.Role == "user" {
			lastUser = m.Content
		}
	}
	lower := strings.ToLower(lastUser)
	toolNames := map[string]bool{}
	for _, t := range req.Tools {
		toolNames[t.Function.Name] = true
	}

	// If the latest message is a tool result, finish the turn with a useful
	// summary. This makes the offline demo exercise the real agent loop.
	if len(req.Messages) > 0 && req.Messages[len(req.Messages)-1].Role == "tool" {
		lastTool := req.Messages[len(req.Messages)-1]
		toolName := ""
		for i := len(req.Messages) - 2; i >= 0; i-- {
			if req.Messages[i].Role == "assistant" && len(req.Messages[i].ToolCalls) > 0 {
				toolName = req.Messages[i].ToolCalls[0].Function.Name
				break
			}
		}
		text := summarizeMockTool(toolName, lastTool.Content, lower)
		return c.mockText(ctx, text, lastUser, onDelta)
	}

	// A tiny scripted policy demonstrates real tool calls, approvals and run
	// cards without requiring an API key.
	if toolNames["save_workflow_draft"] && strings.Contains(lower, "create") && strings.Contains(lower, "workflow") {
		yaml := "schema: 1\nname: demo-digest\ndescription: Save a simple workspace digest after approval.\ninputs:\n  output:\n    type: string\n    format: path\n    default: \"digests/demo.md\"\n    description: Destination file\nsteps:\n  - id: list\n    tool: fs.list\n    with: {path: \".\", limit: 20}\n  - id: review\n    needs: [list]\n    approval:\n      message: \"Save the demo digest to ${inputs.output}?\"\n      show: ${steps.list.output.entries}\n  - id: write\n    needs: [review]\n    when: ${steps.review.output.approved}\n    tool: fs.write_text\n    side_effect: write\n    with:\n      path: ${inputs.output}\n      content: \"${steps.list.output.entries}\"\noutputs:\n  file: ${inputs.output}\n"
		args, _ := json.Marshal(map[string]any{"yaml": yaml, "change_note": "Demo model draft"})
		return c.mockToolCall(ctx, "save_workflow_draft", string(args), lastUser, onDelta), nil
	}
	if toolNames["run_workflow"] && (strings.Contains(lower, "run ") || strings.Contains(lower, "weekly report") || strings.Contains(lower, "chạy")) {
		name := "workspace-digest"
		if strings.Contains(lower, "weekly") || strings.Contains(lower, "report") {
			name = "weekly-report"
		}
		return c.mockToolCall(ctx, "run_workflow", fmt.Sprintf(`{"name":%q,"inputs":{}}`, name), lastUser, onDelta), nil
	}
	if toolNames["fs__write_text"] && strings.Contains(lower, "write") && (strings.Contains(lower, "file") || strings.Contains(lower, "note")) {
		return c.mockToolCall(ctx, "fs__write_text", `{"path":"demo-note.md","content":"Hello from the offline demo agent.\n"}`, lastUser, onDelta), nil
	}
	if toolNames["fs__list"] && (strings.Contains(lower, "summar") || strings.Contains(lower, "workspace") || strings.Contains(lower, "digest") || strings.Contains(lower, "file")) {
		return c.mockToolCall(ctx, "fs__list", `{"path":".","limit":20}`, lastUser, onDelta), nil
	}

	text := "I'm the offline demo model. Add an OpenAI-compatible model in Settings to enable real reasoning. " +
		"Everything else in the app already works: you can run example workflows, inspect runs, author tools and tune results."
	if strings.Contains(lower, "hello") || strings.Contains(lower, "xin chào") {
		text = "Hello! This is the offline demo model. Open Settings → Models and add any OpenAI-compatible endpoint (OpenAI, OpenRouter, Ollama, LM Studio, vLLM…) to start chatting with a real model. The seeded workflows run without a model."
	}
	if toolNames["fs__list"] {
		text += "\n\nI can still demo a real tool call: ask me to summarize the files in your workspace."
	}
	return c.mockText(ctx, text, lastUser, onDelta)
}

func (c *Client) mockText(ctx context.Context, text, lastUser string, onDelta func(StreamEvent)) (ChatResult, error) {
	words := strings.SplitAfter(text, " ")
	for _, w := range words {
		if w == "" {
			continue
		}
		if onDelta != nil {
			onDelta(StreamEvent{TextDelta: w})
		}
		select {
		case <-ctx.Done():
			return ChatResult{}, ctx.Err()
		case <-time.After(8 * time.Millisecond):
		}
	}
	usage := Usage{PromptTokens: len(lastUser) / 4, CompletionTokens: len(text) / 4, TotalTokens: len(lastUser)/4 + len(text)/4}
	if onDelta != nil {
		onDelta(StreamEvent{Usage: &usage, FinishReason: "stop"})
	}
	return ChatResult{Message: Message{Role: "assistant", Content: text}, Usage: usage, FinishReason: "stop", Model: "demo"}, nil
}

func (c *Client) mockToolCall(ctx context.Context, name, args, lastUser string, onDelta func(StreamEvent)) ChatResult {
	call := ToolCall{ID: "call_demo_" + fmt.Sprint(time.Now().UnixNano()), Type: "function",
		Function: FunctionCall{Name: name, Arguments: args}}
	if onDelta != nil {
		onDelta(StreamEvent{ToolCall: &call})
	}
	usage := Usage{PromptTokens: len(lastUser) / 4, CompletionTokens: 12, TotalTokens: len(lastUser)/4 + 12}
	if onDelta != nil {
		onDelta(StreamEvent{Usage: &usage, FinishReason: "tool_calls"})
	}
	return ChatResult{Message: Message{Role: "assistant", ToolCalls: []ToolCall{call}}, Usage: usage, FinishReason: "tool_calls", Model: "demo"}
}

func summarizeMockTool(toolName, content, userText string) string {
	if strings.Contains(content, `"error"`) {
		return "The tool call failed. The error is in the tool card; fix the request or add the missing configuration, then try again."
	}
	switch toolName {
	case "fs__list":
		var res struct {
			Count   int `json:"count"`
			Entries []struct {
				Name  string `json:"name"`
				IsDir bool   `json:"is_dir"`
			} `json:"entries"`
		}
		_ = json.Unmarshal([]byte(content), &res)
		names := []string{}
		for _, e := range res.Entries {
			suffix := ""
			if e.IsDir {
				suffix = "/"
			}
			names = append(names, e.Name+suffix)
		}
		if len(names) > 8 {
			names = append(names[:8], "…")
		}
		text := fmt.Sprintf("I found %d items in the workspace: %s.", res.Count, strings.Join(names, ", "))
		if strings.Contains(userText, "digest") || strings.Contains(userText, "summar") {
			text += " The workspace-digest workflow can read them and save a markdown index after you approve it. Open Workflows → workspace-digest to run it."
		}
		return text
	case "run_workflow":
		var res map[string]any
		_ = json.Unmarshal([]byte(content), &res)
		if id, _ := res["run_id"].(string); id != "" {
			return "I started the run. It will pause at the approval step; open the run card to review the content and allow the write."
		}
		return "The workflow could not be started. The tool card contains the exact validation or approval error."
	case "fs__write_text":
		var res map[string]any
		_ = json.Unmarshal([]byte(content), &res)
		if p, _ := res["path"].(string); p != "" {
			return "The file is saved. The approval card you accepted is recorded in the trace."
		}
		return "The file could not be written. The tool card contains the error."
	case "save_workflow_draft":
		var res map[string]any
		_ = json.Unmarshal([]byte(content), &res)
		if saved, _ := res["saved"].(bool); saved {
			return "The workflow draft is saved and validated. Review the graph and approve it in Workflows before enabling a trigger."
		}
		return "The draft was not saved because validation returned errors. Open the tool card to see the E-codes."
	}
	return "Done. The tool result is shown above; open the card for details."
}

type Capabilities struct {
	Configured       bool   `json:"configured"`
	Streaming        bool   `json:"streaming"`
	ToolCalling      bool   `json:"tool_calling"`
	ParallelToolCall bool   `json:"parallel_tool_calls"`
	JSONMode         bool   `json:"json_mode"`
	LatencyMS        int64  `json:"latency_ms"`
	Error            string `json:"error,omitempty"`
	ProbedAt         int64  `json:"probed_at"`
}

// Probe asks the model to use a harmless tool and reports what actually worked.
func (c *Client) Probe(ctx context.Context, p Profile) Capabilities {
	caps := Capabilities{Configured: true, ProbedAt: time.Now().UnixMilli()}
	if IsMock(p) {
		caps.Streaming = true
		caps.JSONMode = true
		return caps
	}
	start := time.Now()
	tools := []ToolDef{{Type: "function", Function: FunctionDef{
		Name:        "report_capability",
		Description: "Call this once with the requested phrase.",
		Parameters: map[string]any{
			"type": "object", "additionalProperties": false,
			"required":   []any{"phrase"},
			"properties": map[string]any{"phrase": map[string]any{"type": "string"}},
		},
	}}}
	req := ChatRequest{
		Messages:  []Message{{Role: "user", Content: "Call report_capability with phrase \"ok\". Do not answer with text."}},
		Tools:     tools,
		MaxTokens: 64,
		SessionID: uuid.NewString(),
	}
	res, err := c.Chat(ctx, p, req, nil)
	caps.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		caps.Error = err.Error()
		return caps
	}
	caps.Streaming = true
	caps.JSONMode = true
	caps.ToolCalling = len(res.Message.ToolCalls) > 0
	if caps.ToolCalling {
		// A single probe cannot prove parallel calls; this is an inference from
		// providers known to support it. Probe result is advisory.
		caps.ParallelToolCall = strings.Contains(strings.ToLower(p.BaseURL), "openai") ||
			strings.Contains(strings.ToLower(p.BaseURL), "openrouter") ||
			strings.Contains(strings.ToLower(p.BaseURL), "deepseek")
	}
	return caps
}

func CostMicro(p Profile, usage Usage) int64 {
	in, _ := number(p.Params["input_cost_per_mtok"])
	out, _ := number(p.Params["output_cost_per_mtok"])
	cost := float64(usage.PromptTokens)/1_000_000*in + float64(usage.CompletionTokens)/1_000_000*out
	return int64(cost * 1_000_000)
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		var f float64
		_, err := fmt.Sscanf(x, "%f", &f)
		return f, err == nil
	}
	return 0, false
}

func IsRetryable(err error) bool {
	var pe *ProviderError
	if errors.As(err, &pe) {
		return pe.Retryable
	}
	return false
}
