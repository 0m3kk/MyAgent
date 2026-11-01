package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"myagent/internal/testutil"
)

const workflowDraftTestWorkflow = `schema: 1
name: draft-test
description: Workflow used by the draft test.
inputs: {}
settings:
  unattended: ask
steps:
  - id: list
    tool: fs.list
    with: {path: "."}
`

func TestDraftEditsDoNotConsumeVersions(t *testing.T) {
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

	first, err := a.SaveWorkflowDraft(ctx, workflowDraftTestWorkflow, "initial", "user", 0)
	if err != nil {
		t.Fatal(err)
	}
	if first.Version.Version != 1 {
		t.Fatalf("first version = %d", first.Version.Version)
	}
	edited := strings.Replace(
		workflowDraftTestWorkflow,
		"Workflow used by the draft test.",
		"Edited draft.",
		1,
	)
	second, err := a.SaveWorkflowDraft(ctx, edited, "edit draft", "user", 1)
	if err != nil {
		t.Fatal(err)
	}
	if second.Version.ID != first.Version.ID || second.Version.Version != 1 {
		t.Fatalf("draft edit created a new version: %+v -> %+v", first.Version, second.Version)
	}
	if second.Version.YAML != edited {
		t.Fatal("draft YAML was not updated in place")
	}
	versions, err := a.Store.WorkflowVersions(ctx, first.Workflow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 {
		t.Fatalf("draft edit created extra versions: %d", len(versions))
	}
	if err := a.ApproveWorkflowVersion(ctx, first.Workflow.ID, first.Version.ID); err != nil {
		t.Fatal(err)
	}
	third, err := a.SaveWorkflowDraft(
		ctx,
		workflowDraftTestWorkflow,
		"edit after approval",
		"user",
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	if third.Version.Version != 2 || third.Version.ID == first.Version.ID {
		t.Fatalf("expected a new draft v2, got %+v", third.Version)
	}
}
