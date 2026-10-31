package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateLegacyWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	legacy := filepath.Join(home, "MyAgent Workspace")
	target := filepath.Join(home, "MyAgent")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(legacy, "note.txt")
	if err := os.WriteFile(note, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	migrateLegacyWorkspace(target)

	if _, err := os.Stat(filepath.Join(target, "note.txt")); err != nil {
		t.Fatalf("workspace was not migrated: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy workspace still exists: %v", err)
	}
}

func TestMigrateLegacyWorkspaceKeepsExistingTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	legacy := filepath.Join(home, "MyAgent Workspace")
	target := filepath.Join(home, "MyAgent")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "keep.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	migrateLegacyWorkspace(target)

	if _, err := os.Stat(filepath.Join(target, "keep.txt")); err != nil {
		t.Fatalf("existing target was modified: %v", err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy workspace should be left in place: %v", err)
	}
}
