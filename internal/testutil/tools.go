// Package testutil provides fixtures shared by integration tests.
package testutil

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"myagent/internal/ids"
	"myagent/internal/model"
	"myagent/internal/store"
	"myagent/internal/tools"
)

// FSListManifest and FSListCode implement a workspace-scoped fs.list tool.
const FSListManifest = `schema: 1
name: fs.list
description: List files and folders inside the workspace.
kind: starlark
runtime: {entry: main.star}
side_effect: read
input_schema:
  type: object
  additionalProperties: false
  properties:
    path: {type: string, description: Folder path, default: "."}
    limit: {type: integer, description: Maximum entries to return, default: 200}
output_schema: {type: object}
permissions:
  network: {domains: []}
  fs: {read: ["."], write: []}
  exec: []
limits: {timeout_s: 30, memory_mb: 256, output_kb: 1024}
`

const FSListCode = `def run(input, ctx):
    return fs.list(input.get("path", "."), input.get("limit", 200))
`

// FSWriteTextManifest and FSWriteTextCode implement a workspace-scoped
// fs.write_text tool.
const FSWriteTextManifest = `schema: 1
name: fs.write_text
description: Write a UTF-8 text file inside the workspace. Asks for approval in chat.
kind: starlark
runtime: {entry: main.star}
side_effect: write
input_schema:
  type: object
  additionalProperties: false
  required: [path, content]
  properties:
    path: {type: string, description: File path}
    content: {type: string, description: File content}
    append: {type: boolean, description: Append instead of overwrite, default: false}
output_schema: {type: object}
permissions:
  network: {domains: []}
  fs: {read: ["."], write: ["."]}
  exec: []
limits: {timeout_s: 30, memory_mb: 256, output_kb: 64}
`

const FSWriteTextCode = `def run(input, ctx):
    return fs.write_text(
        input["path"],
        input["content"],
        input.get("append", False),
    )
`

// StarlarkManifest returns a minimal valid Starlark tool manifest. Tests use
// it when only the tool's presence matters.
func StarlarkManifest(name, description string) string {
	return fmt.Sprintf(`schema: 1
name: %s
description: %s
kind: starlark
runtime: {entry: main.star}
side_effect: none
input_schema: {type: object, additionalProperties: false, properties: {}}
output_schema: {type: object}
permissions:
  network: {domains: []}
  fs: {read: [], write: []}
  exec: []
limits: {timeout_s: 10, memory_mb: 64, output_kb: 64}
`, name, description)
}

// InstallTool creates an active agent-authored tool directly in the store. It
// bypasses the draft/approval flow so integration tests can start from a known
// tool set.
func InstallTool(
	ctx context.Context,
	st *store.Store,
	workspaceID, toolsDir, manifestYAML, code string,
) (model.Tool, error) {
	m, err := tools.ParseManifest(manifestYAML)
	if err != nil {
		return model.Tool{}, err
	}
	if issues := tools.ValidateManifest(m); len(issues) > 0 {
		return model.Tool{}, fmt.Errorf(
			"install %s: %s %s",
			m.Name,
			issues[0].Path,
			issues[0].Message,
		)
	}
	t, err := st.GetOrCreateTool(ctx, workspaceID, m.Name, m.Kind)
	if err != nil {
		return model.Tool{}, err
	}
	if t.CurrentVersionID != "" {
		return t, nil
	}
	entry := m.Runtime.Entry
	if entry == "" {
		entry = "main.star"
	}
	dir := filepath.Join(toolsDir, t.ID, ids.New())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return model.Tool{}, err
	}
	if err := os.WriteFile(
		filepath.Join(dir, "manifest.yaml"),
		[]byte(manifestYAML),
		0o644,
	); err != nil {
		return model.Tool{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, entry), []byte(code), 0o644); err != nil {
		return model.Tool{}, err
	}
	sum := sha256.Sum256([]byte(manifestYAML + "\n" + code))
	if _, err := st.AddToolVersion(ctx, model.ToolVersion{
		ToolID:      t.ID,
		Manifest:    manifestYAML,
		CodePath:    dir,
		ContentHash: hex.EncodeToString(sum[:]),
		Status:      model.ToolActive,
		CreatedBy:   "user",
		ApprovedAt:  ids.NowMS(),
	}); err != nil {
		return model.Tool{}, err
	}
	return t, nil
}
