package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testFSManifest(read, write, sideEffect string) string {
	return `schema: 1
name: test.fs
description: Workspace filesystem test tool.
kind: starlark
runtime: {entry: main.star}
side_effect: ` + sideEffect + `
input_schema:
  type: object
  additionalProperties: false
  required: [path]
  properties:
    path: {type: string, description: Path to read}
output_schema: {type: object}
permissions:
  network: {domains: []}
  fs: {read: [` + read + `], write: [` + write + `]}
  exec: []
limits: {timeout_s: 10, memory_mb: 64, output_kb: 64}
`
}

func runStarlarkFS(t *testing.T, root, manifestYAML, code, path string) (any, error) {
	t.Helper()
	m, err := ParseManifest(manifestYAML)
	if err != nil {
		t.Fatal(err)
	}
	if issues := ValidateManifest(m); len(issues) > 0 {
		t.Fatalf("manifest invalid: %+v", issues)
	}
	toolDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(toolDir, "main.star"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	e := NewExecutor(root, t.TempDir())
	return e.Execute(context.Background(), m, map[string]any{"path": path}, ExecOptions{
		ToolDir: toolDir, ToolName: m.Name, Manifest: m,
	})
}

func TestStarlarkFSReadsStayInsideWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	code := `def run(input, ctx):
    return fs.read_text(input["path"])
`
	out, err := runStarlarkFS(t, root, testFSManifest(`"."`, ``, `read`), code, "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := out.(map[string]any)
	if body["content"] != "hello" {
		t.Fatalf("content = %#v", body["content"])
	}
	if _, err := runStarlarkFS(t, root, testFSManifest(`"."`, ``, `read`), code, "/etc/passwd"); err == nil ||
		!strings.Contains(err.Error(), "outside the workspace") {
		t.Fatalf("expected workspace escape denial, got %v", err)
	}
}

func TestStarlarkFSRequiresDeclaredPermission(t *testing.T) {
	root := t.TempDir()
	code := `def run(input, ctx):
    return fs.read_text(input["path"])
`
	_, err := runStarlarkFS(t, root, testFSManifest(``, ``, `read`), code, "hello.txt")
	if err == nil || !strings.Contains(err.Error(), "does not declare fs read permission") {
		t.Fatalf("expected permission denial, got %v", err)
	}
}

func TestStarlarkFSWritesStayInsideWorkspace(t *testing.T) {
	root := t.TempDir()
	code := `def run(input, ctx):
    return fs.write_text(input["path"], "written")
`
	out, err := runStarlarkFS(t, root, testFSManifest(`"."`, `"."`, `write`), code, "notes/out.txt")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := out.(map[string]any)
	if body["bytes_written"] != float64(7) {
		t.Fatalf("bytes_written = %#v", body["bytes_written"])
	}
	raw, err := os.ReadFile(filepath.Join(root, "notes", "out.txt"))
	if err != nil || string(raw) != "written" {
		t.Fatalf("written file = %q, err = %v", raw, err)
	}
	if _, err := runStarlarkFS(t, root, testFSManifest(`"."`, `"."`, `write`), code, "../escape.txt"); err == nil ||
		!strings.Contains(err.Error(), "outside the workspace") {
		t.Fatalf("expected workspace escape denial, got %v", err)
	}
}

func TestStarlarkFSRejectsWritesFromReadOnlyTool(t *testing.T) {
	root := t.TempDir()
	code := `def run(input, ctx):
    return fs.write_text(input["path"], "written")
`
	_, err := runStarlarkFS(t, root, testFSManifest(`"."`, `"."`, `read`), code, "out.txt")
	if err == nil || !strings.Contains(err.Error(), "read-only tool cannot write files") {
		t.Fatalf("expected read-only denial, got %v", err)
	}
}
