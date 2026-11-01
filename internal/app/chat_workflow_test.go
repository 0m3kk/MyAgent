package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"myagent/internal/model"
	"myagent/internal/testutil"
)

const chatRunWorkflow = `schema: 1
name: chat-run-test
description: Workflow started by the chat agent.
inputs: {}
outputs:
  message: ${steps.compose.output.message}
steps:
  - id: compose
    set:
      message: hello from the workflow
`

func TestStartWorkflowInChatRecordsRunCard(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	a, err := New(ctx, Config{
		DataDir: dir, WorkspaceRoot: filepath.Join(dir, "workspace"),
		DBPath: filepath.Join(dir, "test.db"), ArtifactDir: filepath.Join(dir, "artifacts"),
		ToolsDir: filepath.Join(dir, "tools"), Version: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := testutil.InstallTool(
		ctx, a.Store, a.Workspace.ID, a.cfg.ToolsDir,
		testutil.FSListManifest, testutil.FSListCode,
	); err != nil {
		t.Fatal(err)
	}
	conv, err := a.Store.CreateConversation(ctx, a.Workspace.ID, "Workflow chat")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := a.SaveWorkflowDraft(
		ctx,
		workflowDraftTestWorkflow,
		"chat workflow test",
		"user",
		0,
	)
	if err != nil {
		t.Fatal(err)
	}

	run, msg, err := a.StartWorkflowInChat(
		ctx,
		conv.ID,
		saved.Workflow.ID,
		saved.Version.ID,
		map[string]any{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if run.Source != "chat" || run.ConversationID != conv.ID {
		t.Fatalf("run is not linked to the chat: %+v", run)
	}
	if run.TargetName != saved.Workflow.Name {
		t.Fatalf("run target name = %q, want %q", run.TargetName, saved.Workflow.Name)
	}
	if msg.Role != "assistant" {
		t.Fatalf("card message role = %q", msg.Role)
	}
	content := msg.ContentStruct()
	if len(content.Parts) != 1 || content.Parts[0].Type != "workflow-run" {
		t.Fatalf("expected one workflow-run part: %+v", content.Parts)
	}
	var card map[string]any
	if err := json.Unmarshal(content.Parts[0].Result, &card); err != nil {
		t.Fatal(err)
	}
	if card["run_id"] != run.ID || card["workflow_name"] != saved.Workflow.Name {
		t.Fatalf("unexpected card payload: %+v", card)
	}

	finished, err := a.waitForRun(ctx, run.ID, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !model.RunTerminal(finished.Status) {
		t.Fatalf("run did not finish: %s", finished.Status)
	}
}

func TestAgentRunWorkflowAddsChatCard(t *testing.T) {
	var calls atomic.Int32
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(v any) {
			raw, _ := json.Marshal(v)
			fmt.Fprintf(w, "data: %s\n\n", raw)
		}
		if calls.Add(1) == 1 {
			send(map[string]any{
				"model": "fake",
				"choices": []any{map[string]any{
					"delta": map[string]any{"tool_calls": []any{map[string]any{
						"index": 0, "id": "call_1", "type": "function",
						"function": map[string]any{
							"name":      "run_workflow",
							"arguments": `{"name":"chat-run-test","inputs":{}}`,
						},
					}}},
					"finish_reason": "tool_calls",
				}},
			})
		} else {
			send(map[string]any{
				"model": "fake",
				"choices": []any{map[string]any{
					"delta":         map[string]any{"content": "Workflow started."},
					"finish_reason": "stop",
				}},
			})
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer llmSrv.Close()

	dir := t.TempDir()
	ctx := context.Background()
	a, err := New(ctx, Config{
		DataDir: dir, WorkspaceRoot: filepath.Join(dir, "workspace"),
		DBPath: filepath.Join(dir, "test.db"), ArtifactDir: filepath.Join(dir, "artifacts"),
		ToolsDir: filepath.Join(dir, "tools"), Version: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := a.SaveModelProfile(ctx, model.ModelProfile{
		Name: "fake", BaseURL: llmSrv.URL + "/v1", Model: "fake",
	}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SaveWorkflowDraft(
		ctx,
		chatRunWorkflow,
		"chat workflow test",
		"user",
		0,
	); err != nil {
		t.Fatal(err)
	}

	reply, err := a.SendChat(ctx, "", "run my workflow")
	if err != nil {
		t.Fatal(err)
	}
	convID := reply.Conversation.ID
	deadline := time.Now().Add(10 * time.Second)
	for a.ChatActive(convID) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if a.ChatActive(convID) {
		t.Fatal("chat agent did not finish")
	}
	msgs, err := a.Store.ListMessages(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	foundCard := false
	foundText := false
	for _, msg := range msgs {
		content := msg.ContentStruct()
		if msg.Role != "assistant" {
			continue
		}
		if content.Text == "Workflow started." {
			foundText = true
		}
		for _, part := range content.Parts {
			if part.Type != "workflow-run" {
				continue
			}
			var card map[string]any
			if err := json.Unmarshal(part.Result, &card); err != nil {
				t.Fatal(err)
			}
			if card["workflow_name"] == "chat-run-test" {
				foundCard = true
			}
		}
	}
	if !foundCard {
		t.Fatalf("agent run did not add a workflow card: %+v", msgs)
	}
	if !foundText {
		t.Fatalf("missing final assistant text: %+v", msgs)
	}
}
