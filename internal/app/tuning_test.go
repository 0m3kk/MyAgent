package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

const tuningTestWorkflow = `schema: 1
name: tuning-test
description: Workflow used by the tuning test.
inputs: {}
settings:
  unattended: ask
steps:
  - id: list
    tool: fs.list
    with: {path: "."}
`

func TestDecideProposalUsesBaseVersion(t *testing.T) {
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

	base, err := a.SaveWorkflowDraft(ctx, tuningTestWorkflow, "initial", "user", 0)
	if err != nil {
		t.Fatal(err)
	}
	proposed := strings.Replace(tuningTestWorkflow, "Workflow used by the tuning test.", "Updated by the tuning test.", 1)
	p, err := a.createProposal(ctx, base.Workflow.ID, base.Version.ID, proposed, "test proposal", "tuner")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := a.DecideProposal(ctx, p.ID, "accept", "")
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Status != "accepted" {
		t.Fatalf("status = %q", accepted.Status)
	}
	w, err := a.Store.GetWorkflowByID(ctx, base.Workflow.ID)
	if err != nil {
		t.Fatal(err)
	}
	current, err := a.Store.GetWorkflowVersion(ctx, w.CurrentVersionID)
	if err != nil {
		t.Fatal(err)
	}
	if current.YAML != proposed {
		t.Fatalf("current version does not contain the accepted proposal")
	}
}

func TestUnifiedDiffKeepsVietnameseReadable(t *testing.T) {
	base := "prompt: |\n  Viết cho người không chuyên.\n  Giữ câu ngắn.\n"
	proposed := "prompt: |\n  Viết cho người không chuyên bằng tiếng Việt.\n  Giữ câu ngắn.\n"

	diff := unifiedDiff(base, proposed)
	if diff == "" {
		t.Fatal("expected a diff")
	}
	if !strings.Contains(diff, "-") || !strings.Contains(diff, "+") {
		t.Fatalf("expected added and removed lines, got:\n%s", diff)
	}
	if !strings.Contains(diff, "Viết cho người không chuyên") {
		t.Fatalf("expected readable Vietnamese text, got:\n%s", diff)
	}
	if strings.Contains(diff, "%E1") || strings.Contains(diff, "%0A") {
		t.Fatalf("diff should not contain URL-encoded text, got:\n%s", diff)
	}
}
