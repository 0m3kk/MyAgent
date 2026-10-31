package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"myagent/internal/api"
	"myagent/internal/app"
	"myagent/internal/model"
)

type testClient struct {
	t     *testing.T
	base  string
	token string
}

func (c *testClient) do(method, path string, body any) (map[string]any, int) {
	c.t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out, resp.StatusCode
}

func (c *testClient) must(method, path string, body any) map[string]any {
	c.t.Helper()
	out, status := c.do(method, path, body)
	if status >= 400 {
		c.t.Fatalf("%s %s -> %d: %+v", method, path, status, out)
	}
	return out
}

const testWorkflow = `schema: 1
name: test-digest
description: Test workflow created by the API test.
inputs: {}
outputs:
  file: ${steps.write.output.path}
settings:
  unattended: ask
steps:
  - id: list
    tool: fs.list
    with: {path: "."}
  - id: review
    needs: [list]
    approval:
      message: "Write the test file?"
  - id: write
    needs: [review]
    when: ${steps.review.output.approved}
    tool: fs.write_text
    side_effect: write
    with: {path: "out.txt", content: "hello"}
`

const testToolManifest = `schema: 1
name: test.echo
description: Return a greeting. Created by the API test.
kind: starlark
runtime: {entry: main.star}
side_effect: none
input_schema:
  type: object
  additionalProperties: false
  required: [name]
  properties:
    name: {type: string, description: Person to greet}
output_schema:
  type: object
  required: [message]
  properties:
    message: {type: string}
permissions:
  network: {domains: []}
  fs: {read: [], write: []}
  exec: []
secrets: []
limits: {timeout_s: 10, memory_mb: 64, output_kb: 64}
tests:
  - name: greets
    input: {name: Ada}
    expect: ['output.message == "Hello Ada"']
  - name: rejects missing input
    input: {}
    expect_error: invalid_input
`

const testToolCode = `def run(input, ctx):
    return {"message": "Hello " + input["name"]}
`

func TestEndToEndAPI(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	a, err := app.New(ctx, app.Config{
		DataDir: dir, WorkspaceRoot: filepath.Join(dir, "workspace"),
		DBPath: filepath.Join(dir, "test.db"), ArtifactDir: filepath.Join(dir, "artifacts"),
		ToolsDir: filepath.Join(dir, "tools"), Version: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	srv := httptest.NewServer(api.New(a, "test-token", filepath.Join(dir, "ui")).Handler())
	defer srv.Close()
	c := &testClient{t: t, base: srv.URL, token: "test-token"}

	boot := c.must("GET", "/api/bootstrap", nil)
	if boot["workspace"] == nil {
		t.Fatal("missing workspace")
	}
	if profiles := boot["model_profiles"].([]any); len(profiles) != 0 {
		t.Fatalf("fresh install should not seed model profiles: %+v", profiles)
	}
	if workflows := c.must("GET", "/api/workflows", nil)["workflows"].([]any); len(workflows) != 0 {
		t.Fatalf("fresh install should not seed workflows: %+v", workflows)
	}

	// A workflow is authored, approved and then run through a human approval.
	saved := c.must("POST", "/api/workflows", map[string]any{
		"yaml": testWorkflow, "change_note": "api test", "approve": true,
	})
	workflowID := saved["workflow"].(map[string]any)["id"].(string)
	runResp := c.must(
		"POST",
		"/api/workflows/"+workflowID+"/run",
		map[string]any{"inputs": map[string]any{}},
	)
	run := runResp["run"].(map[string]any)
	runID := run["id"].(string)

	var status string
	deadline := time.Now().Add(20 * time.Second)
	var approvals []any
	for time.Now().Before(deadline) {
		detail := c.must("GET", "/api/runs/"+runID, nil)
		r := detail["run"].(map[string]any)
		status = fmt.Sprint(r["status"])
		if status == "waiting_approval" {
			approvals = c.must("GET", "/api/approvals", nil)["approvals"].([]any)
			break
		}
		if status == "failed" || status == "succeeded" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if status != "waiting_approval" {
		t.Fatalf("expected waiting_approval, got %s", status)
	}
	if len(approvals) == 0 {
		t.Fatal("no approval created")
	}
	ap := approvals[0].(map[string]any)
	c.must(
		"POST",
		"/api/approvals/"+ap["id"].(string)+"/decision",
		map[string]any{"decision": "approve", "comment": "ok"},
	)

	for time.Now().Before(deadline) {
		detail := c.must("GET", "/api/runs/"+runID, nil)
		r := detail["run"].(map[string]any)
		status = fmt.Sprint(r["status"])
		if status == "succeeded" || status == "failed" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if status != "succeeded" {
		detail := c.must("GET", "/api/runs/"+runID, nil)
		t.Fatalf("run did not succeed: %s %+v", status, detail["run"])
	}
	detail := c.must("GET", "/api/runs/"+runID, nil)
	outputs, _ := detail["outputs"].(map[string]any)
	actualFile := fmt.Sprint(outputs["file"])
	if !strings.HasSuffix(actualFile, filepath.Join("workspace", "out.txt")) {
		t.Fatalf("outputs: %+v", outputs)
	}
	if _, err := os.Stat(actualFile); err != nil {
		t.Fatalf("test file was not written: %v", err)
	}

	// Appearance settings are persisted by the core, not just the browser.
	c.must("PATCH", "/api/settings", map[string]any{"theme": "dark", "font_scale": 1.25})
	settings := c.must("GET", "/api/settings", nil)
	if settings["theme"] != "dark" || settings["font_scale"] != 1.25 {
		t.Fatalf("settings not persisted: %+v", settings)
	}

	// Step limits allow arbitrary values and -1 for unlimited; 0 falls back to
	// the default so missing/legacy values stay finite.
	c.must("PATCH", "/api/settings", map[string]any{"agent_max_steps": -1})
	settings = c.must("GET", "/api/settings", nil)
	if settings["agent_max_steps"] != float64(-1) {
		t.Fatalf("unlimited steps not persisted: %+v", settings)
	}
	c.must("PATCH", "/api/settings", map[string]any{"agent_max_steps": 100})
	settings = c.must("GET", "/api/settings", nil)
	if settings["agent_max_steps"] != float64(100) {
		t.Fatalf("large step limit not persisted: %+v", settings)
	}
	c.must("PATCH", "/api/settings", map[string]any{"agent_max_steps": 0})
	settings = c.must("GET", "/api/settings", nil)
	if settings["agent_max_steps"] != float64(12) {
		t.Fatalf("zero step limit should use the default: %+v", settings)
	}

	// A tool is created, tested and left as a draft for user approval.
	created := c.must("POST", "/api/tools", map[string]any{
		"manifest_yaml": testToolManifest,
		"files":         map[string]any{"main.star": testToolCode},
	})
	toolResult := created["result"].(map[string]any)
	toolID := toolResult["tool"].(map[string]any)["id"].(string)
	versionID := toolResult["version"].(map[string]any)["id"].(string)
	res := c.must("POST", "/api/tools/"+toolID+"/versions/"+versionID+"/test", nil)
	if res["passed"] != true {
		t.Fatalf("tool tests failed: %+v", res)
	}
}

func TestConcurrentChats(t *testing.T) {
	release := make(chan struct{})
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(
			w,
			"data: {\"model\":\"fake\",\"choices\":[{\"delta\":{\"content\":\"thinking\"}}]}\n\n",
		)
		if flusher != nil {
			flusher.Flush()
		}
		<-release
		fmt.Fprint(
			w,
			"data: {\"model\":\"fake\",\"choices\":[{\"delta\":{\"content\":\" done\"},\"finish_reason\":\"stop\"}]}\n\n",
		)
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer llmSrv.Close()

	dir := t.TempDir()
	ctx := context.Background()
	a, err := app.New(ctx, app.Config{
		DataDir: dir, WorkspaceRoot: filepath.Join(dir, "workspace"),
		DBPath: filepath.Join(dir, "test.db"), ArtifactDir: filepath.Join(dir, "artifacts"),
		ToolsDir: filepath.Join(dir, "tools"), Version: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := a.SaveModelProfile(ctx, model.ModelProfile{
		Name: "fake", BaseURL: llmSrv.URL + "/v1", Model: "fake", Role: "executor",
	}, ""); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(api.New(a, "test-token", filepath.Join(dir, "ui")).Handler())
	defer srv.Close()
	c := &testClient{t: t, base: srv.URL, token: "test-token"}

	first := c.must("POST", "/api/chat", map[string]any{"text": "first chat"})
	firstID := first["conversation"].(map[string]any)["id"].(string)
	if !a.ChatActive(firstID) {
		t.Fatal("first chat should be active")
	}
	firstDetail := c.must("GET", "/api/conversations/"+firstID, nil)
	if firstDetail["active"] != true {
		t.Fatalf("conversation should report active: %+v", firstDetail)
	}
	progress, ok := firstDetail["progress"].(map[string]any)
	if !ok {
		t.Fatalf("conversation should report progress: %+v", firstDetail)
	}
	if progress["max_steps"].(float64) <= 0 || progress["budget_tokens"].(float64) <= 0 {
		t.Fatalf("conversation should report limits: %+v", progress)
	}

	second := c.must("POST", "/api/chat", map[string]any{"text": "second chat"})
	secondID := second["conversation"].(map[string]any)["id"].(string)
	if secondID == firstID {
		t.Fatal("second chat should be a new conversation")
	}
	if !a.ChatActive(secondID) {
		t.Fatal("second chat should be active")
	}

	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && (a.ChatActive(firstID) || a.ChatActive(secondID)) {
		time.Sleep(20 * time.Millisecond)
	}
	if a.ChatActive(firstID) || a.ChatActive(secondID) {
		t.Fatal("chats did not finish")
	}

	for _, id := range []string{firstID, secondID} {
		detail := c.must("GET", "/api/conversations/"+id, nil)
		if detail["active"] != false {
			t.Fatalf("conversation %s should not be active: %+v", id, detail)
		}
		if detail["progress"] != nil {
			t.Fatalf("conversation %s should not report progress: %+v", id, detail)
		}
		messages := detail["messages"].([]any)
		if len(messages) < 2 {
			t.Fatalf("conversation %s has no assistant reply: %+v", id, messages)
		}
		last := messages[len(messages)-1].(map[string]any)
		content := last["content"].(map[string]any)
		if !strings.Contains(fmt.Sprint(content["text"]), "done") {
			t.Fatalf("unexpected assistant reply: %+v", content)
		}
	}
}
