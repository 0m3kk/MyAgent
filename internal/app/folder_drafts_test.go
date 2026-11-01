package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"myagent/internal/testutil"
)

func TestAgentDraftFoldersAreCreatedAndReused(t *testing.T) {
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

	out, err := a.executeMetaTool(ctx, "", "create_tool_draft", map[string]any{
		"manifest_yaml": toolVersioningManifest,
		"folder":        "Research/Sources",
	})
	if err != nil {
		t.Fatal(err)
	}
	created := out.(map[string]any)
	if created["folder"] != "Research / Sources" {
		t.Fatalf("tool folder = %v", created["folder"])
	}
	tool, err := a.Store.GetToolByName(ctx, "versioning.tool")
	if err != nil {
		t.Fatal(err)
	}
	toolFolders, err := a.FolderList(ctx, "tool")
	if err != nil {
		t.Fatal(err)
	}
	if len(toolFolders) != 2 {
		t.Fatalf("tool folders = %+v", toolFolders)
	}
	if got := strings.Join(
		folderPathMap(toolFolders)[tool.FolderID],
		"/",
	); got != "Research/Sources" {
		t.Fatalf("tool folder path = %q", got)
	}

	// A second tool with different casing must reuse the same folder path.
	secondManifest := strings.Replace(
		toolVersioningManifest,
		"name: versioning.tool",
		"name: versioning.second",
		1,
	)
	if _, err := a.executeMetaTool(ctx, "", "create_tool_draft", map[string]any{
		"manifest_yaml": secondManifest,
		"folder":        "research/sources",
	}); err != nil {
		t.Fatal(err)
	}
	second, err := a.Store.GetToolByName(ctx, "versioning.second")
	if err != nil {
		t.Fatal(err)
	}
	if second.FolderID != tool.FolderID {
		t.Fatalf("second tool folder = %q, want %q", second.FolderID, tool.FolderID)
	}
	toolFolders, err = a.FolderList(ctx, "tool")
	if err != nil {
		t.Fatal(err)
	}
	if len(toolFolders) != 2 {
		t.Fatalf("folder reuse created duplicates: %+v", toolFolders)
	}

	// Workflow folders are a separate tree and accept an array path.
	out, err = a.executeMetaTool(ctx, "", "save_workflow_draft", map[string]any{
		"yaml":   workflowDraftTestWorkflow,
		"folder": []any{"Ops", "Daily"},
	})
	if err != nil {
		t.Fatal(err)
	}
	saved := out.(map[string]any)
	if saved["saved"] != true || saved["folder"] != "Ops / Daily" {
		t.Fatalf("workflow result = %+v", saved)
	}
	wf, err := a.Store.GetWorkflowByName(ctx, "draft-test")
	if err != nil {
		t.Fatal(err)
	}
	wfFolders, err := a.FolderList(ctx, "workflow")
	if err != nil {
		t.Fatal(err)
	}
	if len(wfFolders) != 2 {
		t.Fatalf("workflow folders = %+v", wfFolders)
	}
	if got := strings.Join(folderPathMap(wfFolders)[wf.FolderID], "/"); got != "Ops/Daily" {
		t.Fatalf("workflow folder path = %q", got)
	}

	listed, err := a.executeMetaTool(ctx, "", "list_folders", map[string]any{
		"kind": "workflow",
	})
	if err != nil {
		t.Fatal(err)
	}
	folders := listed.(map[string]any)["folders"].([]map[string]any)
	if len(folders) != 2 ||
		strings.Join(folders[1]["path"].([]string), "/") != "Ops/Daily" {
		t.Fatalf("listed folders = %+v", folders)
	}
}
