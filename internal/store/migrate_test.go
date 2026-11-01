package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMigrateRemovesLegacyBuiltinTools(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := s.DB().ExecContext(
		ctx,
		`INSERT INTO workspaces (id, name, root_path, created_at) VALUES (?,?,?,?)`,
		"w1", "Workspace", t.TempDir(), 1,
	); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []struct{ id, name, kind string }{
		{"t1", "fs.list", "builtin"},
		{"t2", "custom.read", "starlark"},
	} {
		if _, err := s.DB().ExecContext(
			ctx,
			`INSERT INTO tools (id, workspace_id, name, kind, created_at) VALUES (?,?,?,?,?)`,
			tool.id, "w1", tool.name, tool.kind, 1,
		); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, err := s.DB().QueryContext(ctx, `SELECT name FROM tools ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if len(names) != 1 || names[0] != "custom.read" {
		t.Fatalf("tools after migration = %v", names)
	}
}
