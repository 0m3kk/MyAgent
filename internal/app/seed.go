package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"myagent/internal/ids"
	"myagent/internal/model"
	"myagent/internal/tools"
	"myagent/internal/workflow"
)

const builtinFSList = `schema: 1
name: fs.list
description: List files and folders inside the workspace.
kind: builtin
runtime: {builtin: fs.list}
side_effect: read
input_schema:
  type: object
  additionalProperties: false
  properties:
    path: {type: string, description: Folder path, default: "."}
    limit: {type: integer, description: Maximum entries to return, default: 200}
permissions:
  network: {domains: []}
  fs: {read: ["."], write: []}
  exec: []
limits: {timeout_s: 30, memory_mb: 256, output_kb: 1024}
`

const builtinReadText = `schema: 1
name: fs.read_text
description: Read a UTF-8 text file inside the workspace.
kind: builtin
runtime: {builtin: fs.read_text}
side_effect: read
input_schema:
  type: object
  additionalProperties: false
  required: [path]
  properties:
    path: {type: string, description: File path}
    max_bytes: {type: integer, description: Maximum bytes to read, default: 1048576}
permissions:
  network: {domains: []}
  fs: {read: ["."], write: []}
  exec: []
limits: {timeout_s: 30, memory_mb: 256, output_kb: 2048}
`

const builtinWriteText = `schema: 1
name: fs.write_text
description: Write a UTF-8 text file inside the workspace. Asks for approval in chat.
kind: builtin
runtime: {builtin: fs.write_text}
side_effect: write
input_schema:
  type: object
  additionalProperties: false
  required: [path, content]
  properties:
    path: {type: string, description: File path}
    content: {type: string, description: File content}
    append: {type: boolean, description: Append instead of overwrite, default: false}
permissions:
  network: {domains: []}
  fs: {read: ["."], write: ["."]}
  exec: []
limits: {timeout_s: 30, memory_mb: 256, output_kb: 64}
`

const builtinStat = `schema: 1
name: fs.stat
description: Show metadata for a path in the workspace.
kind: builtin
runtime: {builtin: fs.stat}
side_effect: read
input_schema:
  type: object
  additionalProperties: false
  required: [path]
  properties:
    path: {type: string, description: Path to inspect}
permissions:
  network: {domains: []}
  fs: {read: ["."], write: []}
  exec: []
limits: {timeout_s: 15, memory_mb: 128, output_kb: 32}
`

const builtinHTTPFetch = `schema: 1
name: http.fetch
description: Fetch a public HTTP(S) URL. Private and loopback addresses are blocked.
kind: builtin
runtime: {builtin: http.fetch}
side_effect: read
input_schema:
  type: object
  additionalProperties: false
  required: [url]
  properties:
    url: {type: string, description: Public URL, format: uri}
    method: {type: string, description: HTTP method, enum: [GET, HEAD], default: GET}
    headers: {type: object, description: Request headers, additionalProperties: {type: string}}
    body: {type: string, description: Request body}
permissions:
  network: {domains: []}
  fs: {read: [], write: []}
  exec: []
limits: {timeout_s: 45, memory_mb: 256, output_kb: 2048}
`

const builtinShell = `schema: 1
name: shell.exec
description: Run an allowlisted command in a sandboxed subprocess. Always asks for approval.
kind: builtin
runtime: {builtin: shell.exec}
side_effect: external
input_schema:
  type: object
  additionalProperties: false
  required: [command]
  properties:
    command: {type: string, description: Executable name}
    args: {type: array, description: Command arguments, items: {type: string}}
    cwd: {type: string, description: Working directory, default: "."}
    timeout_s: {type: integer, description: Timeout in seconds, default: 30}
permissions:
  network: {domains: []}
  fs: {read: ["."], write: ["."]}
  exec: ["/bin/bash", "/bin/sh", "/bin/zsh", "/bin/echo", "/bin/ls", "/bin/cat", "/bin/pwd", "/usr/bin/env", "/usr/bin/python3", "/opt/homebrew/bin/python3", "/usr/local/bin/node", "/opt/homebrew/bin/node", "/usr/local/bin/git", "/usr/bin/git"]
limits: {timeout_s: 60, memory_mb: 512, output_kb: 1024}
`

const builtinWorkspaceInfo = `schema: 1
name: workspace.info
description: Show the workspace root, OS, date and available local runtimes.
kind: builtin
runtime: {builtin: workspace.info}
side_effect: read
input_schema: {type: object, additionalProperties: false, properties: {}}
permissions:
  network: {domains: []}
  fs: {read: [], write: []}
  exec: []
limits: {timeout_s: 10, memory_mb: 64, output_kb: 32}
`

const builtinTextStats = `schema: 1
name: text.stats
description: Count characters, words and lines in a piece of text.
kind: builtin
runtime: {builtin: text.stats}
side_effect: none
input_schema:
  type: object
  additionalProperties: false
  required: [text]
  properties:
    text: {type: string, description: Text to inspect}
permissions:
  network: {domains: []}
  fs: {read: [], write: []}
  exec: []
limits: {timeout_s: 10, memory_mb: 64, output_kb: 16}
`

func (a *App) seed(ctx context.Context) error {
	// Built-in tools are part of the product, not demo data. Workflows, model
	// profiles, conversations and workspace files are created by the user.
	builtins := []string{builtinFSList, builtinReadText, builtinWriteText, builtinStat,
		builtinHTTPFetch, builtinShell, builtinWorkspaceInfo, builtinTextStats}
	for _, manifest := range builtins {
		if err := a.seedTool(ctx, manifest, ""); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) seedTool(ctx context.Context, manifestYAML, code string) error {
	m, err := tools.ParseManifest(manifestYAML)
	if err != nil {
		return fmt.Errorf("seed tool: %w", err)
	}
	if issues := tools.ValidateManifest(m); len(issues) > 0 {
		return fmt.Errorf("seed %s: %s", m.Name, issues[0].Path+" "+issues[0].Message)
	}
	t, err := a.Store.GetOrCreateTool(ctx, a.Workspace.ID, m.Name, m.Kind)
	if err != nil {
		return err
	}
	if t.CurrentVersionID != "" {
		return nil
	}
	codePath := ""
	hashInput := manifestYAML + code
	if code != "" {
		dir := filepath.Join(a.cfg.ToolsDir, t.ID, ids.New())
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		entry := m.Runtime.Entry
		if entry == "" {
			entry = "main.star"
		}
		if err := os.WriteFile(filepath.Join(dir, entry), []byte(code), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifestYAML), 0o644); err != nil {
			return err
		}
		codePath = dir
	}
	sum := sha256.Sum256([]byte(hashInput))
	_, err = a.Store.AddToolVersion(ctx, model.ToolVersion{
		ToolID: t.ID, Manifest: manifestYAML, CodePath: codePath, ContentHash: hex.EncodeToString(sum[:]),
		Status: model.ToolActive, CreatedBy: "user", ApprovedAt: ids.NowMS(),
	})
	return err
}

func (a *App) validateOptions(ctx context.Context, forRun bool) workflow.ValidateOptions {
	toolList, _ := a.Store.ListTools(ctx, model.ToolFilter{})
	toolsMap := map[string]workflow.ToolInfo{}
	for _, t := range toolList {
		if t.Version == nil {
			continue
		}
		m, err := tools.ParseManifest(t.Version.Manifest)
		if err != nil {
			continue
		}
		info := workflow.ToolInfo{Name: t.Name, VersionID: t.Version.ID, Status: t.Version.Status,
			InputSchema: m.InputSchema, SideEffect: m.SideEffect, Network: len(m.Permissions.Network.Domains) > 0,
			SecretNames: secretNames(m.Secrets)}
		// Missing secret bindings.
		binds, _ := a.Store.SecretBindings(ctx, t.ID)
		byName := map[string]bool{}
		for _, b := range binds {
			byName[b.LogicalName] = true
		}
		for _, d := range m.Secrets {
			if d.Required && !byName[d.Name] {
				info.MissingSecrets = append(info.MissingSecrets, d.Name)
			}
		}
		toolsMap[t.Name] = info
		versions, _ := a.Store.ToolVersions(ctx, t.ID)
		for _, tv := range versions {
			if tv.Status == model.ToolActive || tv.Status == model.ToolApproved {
				toolsMap[fmt.Sprintf("%s@%d", t.Name, tv.Version)] = info
			}
		}
	}
	wfMap := map[string]bool{}
	wfs, _ := a.Store.ListWorkflows(ctx)
	for _, w := range wfs {
		if w.Version != nil && w.Version.Status == "approved" {
			wfMap[w.Name] = true
			wfMap[fmt.Sprintf("%s@%d", w.Name, w.Version.Version)] = true
		}
	}
	return workflow.ValidateOptions{Tools: toolsMap, Workflows: wfMap, ForRun: forRun}
}
