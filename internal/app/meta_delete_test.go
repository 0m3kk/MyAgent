package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"myagent/internal/model"
)

const deleteWorkflowYAML = `schema: 1
name: delete-me
description: Workflow used by the meta delete test.
inputs: {}
steps:
  - id: only
    set:
      value: 1
`

func newMetaDeleteTestApp(t *testing.T) (*App, context.Context) {
	t.Helper()
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	a, err := New(ctx, Config{
		DataDir: dir, WorkspaceRoot: filepath.Join(dir, "workspace"),
		DBPath: filepath.Join(dir, "test.db"), ArtifactDir: filepath.Join(dir, "artifacts"),
		ToolsDir: filepath.Join(dir, "tools"), Version: "test",
	})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		a.Close()
	})
	return a, ctx
}

func waitForDeleteApproval(t *testing.T, a *App) model.Approval {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		list, err := a.PendingApprovals(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, ap := range list {
			if ap.Kind == "delete" {
				return ap
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("delete approval was not requested")
	return model.Approval{}
}

type metaDeleteResult struct {
	out any
	err error
}

func TestAgentDeleteWorkflowRequiresApproval(t *testing.T) {
	a, ctx := newMetaDeleteTestApp(t)
	saved, err := a.SaveWorkflowDraft(ctx, deleteWorkflowYAML, "test", "user", 0)
	if err != nil {
		t.Fatalf("save workflow: %v %+v", err, saved.Validation.Errors)
	}
	name := saved.Workflow.Name

	done := make(chan metaDeleteResult, 1)
	go func() {
		out, err := a.executeMetaTool(ctx, "", "delete_workflow", map[string]any{
			"name": name, "reason": "duplicate workflow",
		})
		done <- metaDeleteResult{out: out, err: err}
	}()

	ap := waitForDeleteApproval(t, a)
	var payload map[string]any
	if err := json.Unmarshal(ap.Request, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["workflow"] != name || payload["object"] != "workflow" {
		t.Fatalf("approval payload = %+v", payload)
	}
	if payload["reason"] != "duplicate workflow" {
		t.Fatalf("reason = %v", payload["reason"])
	}
	if _, err := a.Store.GetWorkflowByName(ctx, name); err != nil {
		t.Fatalf("workflow deleted before approval: %v", err)
	}

	if _, err := a.ResolveApproval(ctx, ap.ID, "approve", "ok"); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	out, _ := got.out.(map[string]any)
	if out["deleted"] != true {
		t.Fatalf("result = %+v", got.out)
	}
	if _, err := a.Store.GetWorkflowByName(ctx, name); err == nil {
		t.Fatal("workflow still exists after approval")
	}
}

func TestAgentDeleteToolRequiresApproval(t *testing.T) {
	a, ctx := newMetaDeleteTestApp(t)
	created, err := a.CreateToolDraft(ctx, toolVersioningManifest, "user", 0)
	if err != nil {
		t.Fatalf("create tool: %v %+v", err, created.Issues)
	}
	name := created.Tool.Name

	done := make(chan metaDeleteResult, 1)
	go func() {
		out, err := a.executeMetaTool(ctx, "", "delete_tool", map[string]any{
			"name": name, "reason": "unused tool",
		})
		done <- metaDeleteResult{out: out, err: err}
	}()

	ap := waitForDeleteApproval(t, a)
	var payload map[string]any
	if err := json.Unmarshal(ap.Request, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["tool"] != name || payload["object"] != "tool" {
		t.Fatalf("approval payload = %+v", payload)
	}
	if _, err := a.Store.GetToolByName(ctx, name); err != nil {
		t.Fatalf("tool deleted before approval: %v", err)
	}

	if _, err := a.ResolveApproval(ctx, ap.ID, "approve", "ok"); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	out, _ := got.out.(map[string]any)
	if out["deleted"] != true {
		t.Fatalf("result = %+v", got.out)
	}
	if _, err := a.Store.GetToolByName(ctx, name); err == nil {
		t.Fatal("tool still exists after approval")
	}
}

func TestAgentDeleteToolRejectKeepsTool(t *testing.T) {
	a, ctx := newMetaDeleteTestApp(t)
	created, err := a.CreateToolDraft(ctx, toolVersioningManifest, "user", 0)
	if err != nil {
		t.Fatalf("create tool: %v %+v", err, created.Issues)
	}
	name := created.Tool.Name

	done := make(chan metaDeleteResult, 1)
	go func() {
		out, err := a.executeMetaTool(ctx, "", "delete_tool", map[string]any{"name": name})
		done <- metaDeleteResult{out: out, err: err}
	}()

	ap := waitForDeleteApproval(t, a)
	if _, err := a.ResolveApproval(ctx, ap.ID, "reject", "keep it"); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	out, _ := got.out.(map[string]any)
	if out["ok"] != false || out["status"] != "rejected" {
		t.Fatalf("result = %+v", got.out)
	}
	if _, err := a.Store.GetToolByName(ctx, name); err != nil {
		t.Fatalf("tool deleted after rejection: %v", err)
	}
}
