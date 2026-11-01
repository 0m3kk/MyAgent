package workflow

import (
	"strings"
	"testing"
)

const validYAML = `schema: 1
name: demo
description: demo workflow
inputs:
  folder: {type: string, description: Folder, default: "."}
outputs:
  result: ${steps.list.output}
settings:
  unattended: ask
steps:
  - id: list
    tool: fs.list
    with: {path: "${inputs.folder}"}
  - id: review
    needs: [list]
    approval: {message: "Continue?"}
  - id: write
    needs: [review]
    tool: fs.write_text
    when: ${steps.review.output.approved}
    with: {path: "out.txt", content: "${steps.list.output}"}
`

func toolMap() map[string]ToolInfo {
	return map[string]ToolInfo{
		"fs.list": {Name: "fs.list", VersionID: "v1", Status: "active", SideEffect: "read",
			InputSchema: map[string]any{"type": "object", "additionalProperties": false,
				"properties": map[string]any{"path": map[string]any{"type": "string"}}}},
		"fs.write_text": {
			Name:       "fs.write_text",
			VersionID:  "v2",
			Status:     "active",
			SideEffect: "write",
			InputSchema: map[string]any{"type": "object", "additionalProperties": false,
				"required": []any{"path", "content"},
				"properties": map[string]any{
					"path":    map[string]any{"type": "string"},
					"content": map[string]any{"type": "string"},
				}},
		},
	}
}

func TestValidWorkflow(t *testing.T) {
	spec, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	res := Validate(spec, ValidateOptions{Tools: toolMap()})
	if !res.OK() {
		t.Fatalf("unexpected errors: %+v", res.Errors)
	}
}

func TestUnknownToolAndMissingAncestor(t *testing.T) {
	yaml := strings.Replace(validYAML, "tool: fs.list", "tool: nope.missing", 1)
	spec, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	res := Validate(spec, ValidateOptions{Tools: toolMap()})
	found := false
	for _, e := range res.Errors {
		if e.Code == "E007" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected E007, got %+v", res.Errors)
	}
}

func TestCycleDetected(t *testing.T) {
	yaml := `schema: 1
name: cycle
inputs: {}
steps:
  - id: a
    needs: [b]
    set: {x: 1}
  - id: b
    needs: [a]
    set: {x: 2}
`
	spec, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	res := Validate(spec, ValidateOptions{Tools: toolMap()})
	found := false
	for _, e := range res.Errors {
		if e.Code == "E004" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected E004, got %+v", res.Errors)
	}
}

func TestUndeclaredInput(t *testing.T) {
	yaml := strings.Replace(validYAML, "${inputs.folder}", "${inputs.nope}", 1)
	spec, _ := Parse([]byte(yaml))
	res := Validate(spec, ValidateOptions{Tools: toolMap()})
	found := false
	for _, e := range res.Errors {
		if e.Code == "E006" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected E006, got %+v", res.Errors)
	}
}

func TestUnknownFieldRejected(t *testing.T) {
	_, err := Parse([]byte(`schema: 1
name: x
steps:
  - id: a
    set: {x: 1}
    surprise: true
`))
	if err == nil {
		t.Fatal("unknown field should be rejected")
	}
}

func TestMapAliasAndNestedReference(t *testing.T) {
	yaml := `schema: 1
name: mapped
inputs: {}
steps:
  - id: list
    tool: fs.list
    with: {path: "."}
  - id: each
    needs: [list]
    map:
      over: ${steps.list.output.files}
      as: file
      do:
        - id: read
          tool: fs.list
          with: {path: "${file.path}"}
`
	spec, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	res := Validate(spec, ValidateOptions{Tools: toolMap()})
	if !res.OK() {
		t.Fatalf("unexpected errors: %+v", res.Errors)
	}
}

func TestModelNameValidation(t *testing.T) {
	yaml := `schema: 1
name: model-test
defaults:
  model: fast-model
steps:
  - id: summarize
    llm:
      model: strong-model
      prompt: Summarize this
`
	spec, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	res := Validate(spec, ValidateOptions{Models: map[string]bool{"fast-model": true}})
	if res.OK() {
		t.Fatal("expected an unknown model error")
	}
	found := false
	for _, e := range res.Errors {
		if e.Path == "steps[0].llm.model" && strings.Contains(e.Message, "strong-model") {
			found = true
		}
	}
	if !found {
		t.Fatalf("errors = %+v", res.Errors)
	}

	res = Validate(spec, ValidateOptions{
		Models: map[string]bool{"fast-model": true, "strong-model": true},
	})
	if !res.OK() {
		t.Fatalf("unexpected errors: %+v", res.Errors)
	}
}

func TestLegacyRoleStillParsesAndWarns(t *testing.T) {
	yaml := `schema: 1
name: legacy-role
defaults:
  role: executor
steps:
  - id: summarize
    llm:
      role: planner
      prompt: Summarize this
`
	spec, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	res := Validate(spec, ValidateOptions{})
	if !res.OK() {
		t.Fatalf("legacy role should not block validation: %+v", res.Errors)
	}
	count := 0
	for _, w := range res.Warnings {
		if w.Code == "W005" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected two deprecation warnings, got %+v", res.Warnings)
	}
}
