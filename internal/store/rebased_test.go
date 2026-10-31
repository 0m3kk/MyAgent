package store

import (
	"context"
	"path/filepath"
	"testing"

	"myagent/internal/model"
)

func TestRebaseDataPaths(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ws, err := st.EnsureWorkspace(ctx, "Personal", filepath.Join(dir, "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	tool, err := st.GetOrCreateTool(ctx, ws.ID, "test.tool", "starlark")
	if err != nil {
		t.Fatal(err)
	}
	oldTools := filepath.Join(dir, "old", "tools")
	oldArtifacts := filepath.Join(dir, "old", "artifacts")
	v, err := st.AddToolVersion(ctx, model.ToolVersion{
		ToolID: tool.ID, Manifest: "name: test.tool", CodePath: filepath.Join(oldTools, "t1", "v1"),
		ContentHash: "hash", Status: model.ToolDraft, CreatedBy: "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(
		`INSERT INTO artifacts (id,path,sha256,size,created_at) VALUES (?,?,?,?,?)`,
		"a1", filepath.Join(oldArtifacts, "run", "out.json"), "sum", 1, 1,
	); err != nil {
		t.Fatal(err)
	}
	newTools := filepath.Join(dir, "profile", "tools")
	newArtifacts := filepath.Join(dir, "profile", "artifacts")
	if err := st.RebaseDataPaths(ctx, newTools, newArtifacts); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetToolVersion(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(newTools, "t1", "v1")
	if got.CodePath != want {
		t.Fatalf("code path = %q, want %q", got.CodePath, want)
	}
	var artifact string
	if err := st.DB().QueryRow(`SELECT path FROM artifacts WHERE id='a1'`).Scan(&artifact); err != nil {
		t.Fatal(err)
	}
	wantArtifact := filepath.Join(newArtifacts, "run", "out.json")
	if artifact != wantArtifact {
		t.Fatalf("artifact path = %q, want %q", artifact, wantArtifact)
	}
}
