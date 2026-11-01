package store

import (
	"context"
	"database/sql"
	"myagent/internal/model"
	"path/filepath"
	"testing"
)

// A pre-folders database must gain the new columns and table in place: users
// already have tools, workflows and runs that cannot be recreated.
func TestFoldersMigrateLegacyDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`
		CREATE TABLE workspaces (
		  id TEXT PRIMARY KEY, name TEXT NOT NULL, root_path TEXT NOT NULL, created_at INTEGER NOT NULL
		) STRICT;
		CREATE TABLE tools (
		  id TEXT PRIMARY KEY,
		  workspace_id TEXT NOT NULL,
		  name TEXT NOT NULL,
		  kind TEXT NOT NULL,
		  current_version_id TEXT,
		  draft_version_id TEXT,
		  created_at INTEGER NOT NULL,
		  UNIQUE (workspace_id, name)
		) STRICT;
		CREATE TABLE workflows (
		  id TEXT PRIMARY KEY,
		  workspace_id TEXT NOT NULL,
		  name TEXT NOT NULL,
		  current_version_id TEXT,
		  created_at INTEGER NOT NULL,
		  UNIQUE (workspace_id, name)
		) STRICT;
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	defer st.Close()

	ws, err := st.EnsureWorkspace(ctx, "Personal", filepath.Join(dir, "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := st.CreateFolder(ctx, folderForTest(ws.ID, "tool", "Research", ""))
	if err != nil {
		t.Fatal(err)
	}
	child, err := st.CreateFolder(ctx, folderForTest(ws.ID, "tool", "Sources", root.ID))
	if err != nil {
		t.Fatal(err)
	}
	tool, err := st.GetOrCreateTool(ctx, ws.ID, "test.org", "starlark")
	if err != nil {
		t.Fatal(err)
	}
	pinned := true
	if err := st.SetToolOrganization(ctx, tool.ID, child.ID, &pinned); err != nil {
		t.Fatal(err)
	}
	tool, err = st.GetToolByID(ctx, tool.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tool.FolderID != child.ID || !tool.Pinned {
		t.Fatalf("tool placement = %q pinned=%v", tool.FolderID, tool.Pinned)
	}

	workflow, err := st.GetOrCreateWorkflow(ctx, ws.ID, "test.flow")
	if err != nil {
		t.Fatal(err)
	}
	wfFolder, err := st.CreateFolder(ctx, folderForTest(ws.ID, "workflow", "Ops", ""))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetWorkflowOrganization(ctx, workflow.ID, wfFolder.ID, &pinned); err != nil {
		t.Fatal(err)
	}
	workflow, err = st.GetWorkflowByID(ctx, workflow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if workflow.FolderID != wfFolder.ID || !workflow.Pinned {
		t.Fatalf("workflow placement = %q pinned=%v", workflow.FolderID, workflow.Pinned)
	}

	// Deleting the top-level folder removes its nested folder and moves the
	// tool to the root rather than deleting it.
	if err := st.DeleteFolder(ctx, root.ID); err != nil {
		t.Fatal(err)
	}
	tool, err = st.GetToolByID(ctx, tool.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tool.FolderID != "" {
		t.Fatalf("tool folder after delete = %q", tool.FolderID)
	}
	folders, err := st.ListFolders(ctx, "tool")
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 0 {
		t.Fatalf("tool folders after delete = %+v", folders)
	}
}

func folderForTest(workspaceID, kind, name, parentID string) model.Folder {
	return model.Folder{WorkspaceID: workspaceID, Kind: kind, Name: name, ParentID: parentID}
}
