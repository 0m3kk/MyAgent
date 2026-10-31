package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"myagent/internal/workflow"
)

func (e *Executor) executeScript(
	ctx context.Context,
	m *Manifest,
	input map[string]any,
	opts ExecOptions,
) (any, error) {
	entry := m.Runtime.Entry
	if entry == "" {
		return nil, &workflow.ToolError{Kind: "invalid_tool", Message: "runtime.entry is required"}
	}
	entryPath := entry
	if !filepath.IsAbs(entryPath) {
		entryPath = filepath.Join(opts.ToolDir, entry)
	}
	if _, err := os.Stat(entryPath); err != nil {
		return nil, &workflow.ToolError{
			Kind:    "invalid_tool",
			Message: "entry file is missing: " + entry,
		}
	}
	outDir := filepath.Join(
		e.DataDir,
		"tmp",
		fmt.Sprintf("%s-%d", strings.ReplaceAll(m.Name, ".", "_"), time.Now().UnixNano()),
	)
	secretsDir := filepath.Join(outDir, "secrets")
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		return nil, &workflow.ToolError{Kind: "crashed", Message: err.Error()}
	}
	defer os.RemoveAll(outDir)

	env := []string{
		"PATH=/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin",
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		"HOME=" + outDir,
		"TMPDIR=" + outDir,
		"OUT_DIR=" + outDir,
		"PYTHONDONTWRITEBYTECODE=1",
	}
	headerSecrets := []HeaderSecret{}
	for _, decl := range m.Secrets {
		val, ok := opts.Secrets[decl.Name]
		if !ok || val == "" {
			continue
		}
		switch decl.Inject {
		case "env":
			env = append(env, decl.Name+"="+val)
		case "file":
			path, err := fileSecretEnv(decl, val, secretsDir)
			if err != nil {
				return nil, &workflow.ToolError{Kind: "crashed", Message: err.Error()}
			}
			env = append(env, decl.Name+"="+path)
		case "header":
			headerSecrets = append(
				headerSecrets,
				HeaderSecret{
					Name:    decl.Name,
					Value:   val,
					Header:  decl.Header,
					Domains: decl.Scope.Domains,
				},
			)
		}
	}
	if len(m.Permissions.Network.Domains) > 0 {
		proxy, perr := StartProxy(m.Permissions.Network.Domains, headerSecrets, opts.Mocks)
		if perr != nil {
			return nil, &workflow.ToolError{
				Kind:    "crashed",
				Message: "cannot start network proxy: " + perr.Error(),
			}
		}
		defer proxy.Close()
		env = append(env, proxy.ProxyEnv()...)
	}

	ctxPayload := map[string]any{
		"run_id":    opts.RunID,
		"idem_key":  opts.IdemKey,
		"workspace": e.WorkspaceRoot,
		"out_dir":   outDir,
	}
	reqBody, _ := json.Marshal(map[string]any{"input": input, "ctx": ctxPayload})
	var cmd string
	var args []string
	switch m.Runtime.Language {
	case "python":
		cmd = "python3"
		args = []string{entryPath}
	case "node":
		cmd = "node"
		args = []string{entryPath}
	case "shell":
		cmd = "/bin/sh"
		args = []string{entryPath}
	case "binary":
		cmd = entryPath
	default:
		return nil, &workflow.ToolError{
			Kind:    "invalid_tool",
			Message: "unsupported script language " + m.Runtime.Language,
		}
	}
	allowWrite := len(m.Permissions.FS.Write) > 0 || m.SideEffect == "write"
	onLog := func(stream, text string) {
		if stream == "stderr" {
			e.log(opts, "stderr", text)
		}
	}
	res := RunSandboxed(ctx, SandboxSpec{
		Command:    cmd,
		Args:       args,
		Dir:        opts.ToolDir,
		Workspace:  e.WorkspaceRoot,
		ToolDir:    opts.ToolDir,
		OutDir:     outDir,
		AllowNet:   len(m.Permissions.Network.Domains) > 0,
		AllowWrite: allowWrite,
		AllowExec:  m.Permissions.Exec,
		DenyRead:   sandboxDenyPaths(e),
		Env:        env,
		Stdin:      string(reqBody),
		OnLog:      onLog,
	})
	if res.TimedOut {
		return nil, &workflow.ToolError{
			Kind:      "timeout",
			Message:   "script timed out",
			Retryable: true,
		}
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		if len(msg) > 1200 {
			msg = msg[:1200]
		}
		return nil, &workflow.ToolError{
			Kind:    "crashed",
			Message: fmt.Sprintf("script exited with code %d: %s", res.ExitCode, msg),
		}
	}
	raw := strings.TrimSpace(res.Stdout)
	if raw == "" {
		return nil, &workflow.ToolError{Kind: "crashed", Message: "script produced no JSON output"}
	}
	// Tolerate a final log line before the JSON object.
	if !json.Valid([]byte(raw)) {
		lines := strings.Split(raw, "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			candidate := strings.TrimSpace(lines[i])
			if json.Valid([]byte(candidate)) {
				raw = candidate
				break
			}
		}
	}
	var out any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, &workflow.ToolError{
			Kind:    "crashed",
			Message: "script stdout is not valid JSON: " + truncate(raw, 300),
		}
	}
	if m, ok := out.(map[string]any); ok {
		if e, ok := m["error"]; ok {
			kind, msg := "tool_error", fmt.Sprint(e)
			if em, ok := e.(map[string]any); ok {
				if k, _ := em["kind"].(string); k != "" {
					kind = k
				}
				if mm, _ := em["message"].(string); mm != "" {
					msg = mm
				}
			}
			return nil, &workflow.ToolError{Kind: kind, Message: msg}
		}
	}
	return out, nil
}
