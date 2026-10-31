package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const testManifest = `schema: 1
name: text.test
description: Test tool.
kind: starlark
runtime: {entry: main.star}
side_effect: none
input_schema:
  type: object
  additionalProperties: false
  required: [name]
  properties:
    name: {type: string, description: Person}
output_schema:
  type: object
  required: [message]
  properties:
    message: {type: string}
permissions:
  network: {domains: []}
  fs: {read: [], write: []}
  exec: []
secrets: []
limits: {timeout_s: 10, memory_mb: 64, output_kb: 64}
tests:
  - name: works
    input: {name: Ada}
    expect: ['output.message == "Hello Ada"']
  - name: invalid
    input: {}
    expect_error: invalid_input
`

const testCode = `def run(input, ctx):
    return {"message": "Hello " + input["name"]}
`

func TestManifestValidation(t *testing.T) {
	m, err := ParseManifest(testManifest)
	if err != nil {
		t.Fatal(err)
	}
	if issues := ValidateManifest(m); len(issues) != 0 {
		t.Fatalf("issues: %+v", issues)
	}
}

func TestStarlarkExecutionAndTests(t *testing.T) {
	m, err := ParseManifest(testManifest)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := writeFile(filepath.Join(dir, "main.star"), testCode); err != nil {
		t.Fatal(err)
	}
	ex := NewExecutor(t.TempDir(), t.TempDir())
	out, err := ex.Execute(context.Background(), m, map[string]any{"name": "Ada"}, ExecOptions{ToolDir: dir, ToolName: m.Name, Manifest: m})
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["message"] != "Hello Ada" {
		t.Fatalf("unexpected output: %+v", out)
	}
	results := ex.RunTests(context.Background(), m, dir, nil)
	if len(results) != 2 || !results[0].Passed || !results[1].Passed {
		t.Fatalf("test results: %+v", results)
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
