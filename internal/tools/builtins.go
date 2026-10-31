package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"myagent/internal/ids"
	"myagent/internal/workflow"
)

func (e *Executor) executeBuiltin(ctx context.Context, m *Manifest, input map[string]any, opts ExecOptions) (any, error) {
	name := m.Runtime.Builtin
	if name == "" {
		name = m.Name
	}
	switch name {
	case "fs.list":
		return e.fsList(ctx, input, opts)
	case "fs.read_text":
		return e.fsReadText(ctx, input, opts)
	case "fs.write_text":
		return e.fsWriteText(ctx, input, opts)
	case "fs.stat":
		return e.fsStat(ctx, input, opts)
	case "fs.delete":
		return e.fsDelete(ctx, input, opts)
	case "http.fetch":
		return e.httpFetch(ctx, m, input, opts)
	case "shell.exec":
		return e.shellExec(ctx, m, input, opts)
	case "text.stats":
		return textStats(input)
	case "workspace.info":
		return e.workspaceInfo(ctx, input)
	}
	return nil, &workflow.ToolError{Kind: "unsupported", Message: "unknown builtin " + name}
}

func (e *Executor) fsList(ctx context.Context, input map[string]any, opts ExecOptions) (any, error) {
	path := stringArg(input, "path", "")
	resolved, err := e.ResolveFilePath(path, false)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &workflow.ToolError{Kind: "not_found", Message: "directory does not exist: " + resolved}
		}
		return nil, &workflow.ToolError{Kind: "crashed", Message: err.Error()}
	}
	limit := intArg(input, "limit", 500)
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	out := []map[string]any{}
	files := []map[string]any{}
	dirs := []map[string]any{}
	for _, de := range entries {
		if len(out) >= limit {
			break
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		item := map[string]any{
			"name": de.Name(), "path": filepath.Join(resolved, de.Name()),
			"relative": relativeTo(e.WorkspaceRoot, filepath.Join(resolved, de.Name())),
			"is_dir":   de.IsDir(), "size": info.Size(), "modified": info.ModTime().UnixMilli(),
		}
		out = append(out, item)
		if de.IsDir() {
			dirs = append(dirs, item)
		} else {
			files = append(files, item)
		}
	}
	return map[string]any{"path": resolved, "entries": out, "files": files, "dirs": dirs, "count": len(out)}, nil
}

func (e *Executor) fsReadText(ctx context.Context, input map[string]any, opts ExecOptions) (any, error) {
	path := stringArg(input, "path", "")
	if path == "" {
		return nil, &workflow.ToolError{Kind: "invalid_input", Message: "path is required"}
	}
	resolved, err := e.ResolveFilePath(path, false)
	if err != nil {
		return nil, err
	}
	max := intArg(input, "max_bytes", 1<<20)
	if max <= 0 || max > 10<<20 {
		max = 1 << 20
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, &workflow.ToolError{Kind: "not_found", Message: err.Error()}
	}
	defer f.Close()
	info, _ := f.Stat()
	raw, err := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if err != nil {
		return nil, &workflow.ToolError{Kind: "crashed", Message: err.Error()}
	}
	truncated := len(raw) > max
	if truncated {
		raw = raw[:max]
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		return nil, &workflow.ToolError{Kind: "binary_file", Message: "file appears to be binary"}
	}
	size := int64(0)
	if info != nil {
		size = info.Size()
	}
	return map[string]any{"path": resolved, "content": string(raw), "size": size,
		"truncated": truncated, "relative": relativeTo(e.WorkspaceRoot, resolved)}, nil
}

func (e *Executor) fsWriteText(ctx context.Context, input map[string]any, opts ExecOptions) (any, error) {
	path := stringArg(input, "path", "")
	if path == "" {
		return nil, &workflow.ToolError{Kind: "invalid_input", Message: "path is required"}
	}
	content, ok := input["content"].(string)
	if !ok {
		return nil, &workflow.ToolError{Kind: "invalid_input", Message: "content must be a string"}
	}
	resolved, err := e.ResolveFilePath(path, true)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return nil, &workflow.ToolError{Kind: "crashed", Message: err.Error()}
	}
	flag := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if b, _ := input["append"].(bool); b {
		flag = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	f, err := os.OpenFile(resolved, flag, 0o644)
	if err != nil {
		return nil, &workflow.ToolError{Kind: "crashed", Message: err.Error()}
	}
	n, err := f.WriteString(content)
	if cerr := f.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		return nil, &workflow.ToolError{Kind: "crashed", Message: err.Error()}
	}
	return map[string]any{"path": resolved, "relative": relativeTo(e.WorkspaceRoot, resolved), "bytes_written": n}, nil
}

func (e *Executor) fsStat(ctx context.Context, input map[string]any, opts ExecOptions) (any, error) {
	path := stringArg(input, "path", "")
	resolved, err := e.ResolveFilePath(path, false)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, &workflow.ToolError{Kind: "not_found", Message: err.Error()}
	}
	return map[string]any{"path": resolved, "name": info.Name(), "size": info.Size(), "is_dir": info.IsDir(),
		"modified": info.ModTime().UnixMilli(), "mode": info.Mode().String()}, nil
}

func (e *Executor) fsDelete(ctx context.Context, input map[string]any, opts ExecOptions) (any, error) {
	path := stringArg(input, "path", "")
	if path == "" {
		return nil, &workflow.ToolError{Kind: "invalid_input", Message: "path is required"}
	}
	resolved, err := e.ResolveFilePath(path, true)
	if err != nil {
		return nil, err
	}
	root, _ := e.ResolveFilePath("", true)
	if resolved == root {
		return nil, &workflow.ToolError{Kind: "permission_denied", Message: "refusing to delete the workspace root"}
	}
	recursive, _ := input["recursive"].(bool)
	if recursive {
		err = os.RemoveAll(resolved)
	} else {
		err = os.Remove(resolved)
	}
	if err != nil {
		return nil, &workflow.ToolError{Kind: "crashed", Message: err.Error()}
	}
	return map[string]any{"path": resolved, "deleted": true}, nil
}

func (e *Executor) httpFetch(ctx context.Context, m *Manifest, input map[string]any, opts ExecOptions) (any, error) {
	rawURL := stringArg(input, "url", "")
	if rawURL == "" {
		return nil, &workflow.ToolError{Kind: "invalid_input", Message: "url is required"}
	}
	if _, err := validateFetchURL(rawURL); err != nil {
		return nil, &workflow.ToolError{Kind: "permission_denied", Message: err.Error()}
	}
	if err := checkDomain(m.Permissions.Network.Domains, rawURL); err != nil {
		return nil, err
	}
	method := strings.ToUpper(stringArg(input, "method", "GET"))
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD":
	default:
		return nil, &workflow.ToolError{Kind: "invalid_input", Message: "unsupported HTTP method " + method}
	}
	if m.SideEffect == "read" && method != "GET" && method != "HEAD" {
		return nil, &workflow.ToolError{Kind: "permission_denied", Message: "read-only tool cannot issue " + method}
	}
	if resp, ok := mockFor(opts.Mocks, method, rawURL); ok {
		return e.mockResponse(resp, rawURL)
	}
	body := ""
	if v, ok := input["body"]; ok {
		switch b := v.(type) {
		case string:
			body = b
		default:
			raw, _ := json.Marshal(b)
			body = string(raw)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, strings.NewReader(body))
	if err != nil {
		return nil, &workflow.ToolError{Kind: "invalid_input", Message: err.Error()}
	}
	req.Header.Set("User-Agent", "MyAgent/1.0")
	if headers, ok := input["headers"].(map[string]any); ok {
		for k, v := range headers {
			req.Header.Set(k, fmt.Sprint(v))
		}
	}
	client := e.HTTP
	if client == nil {
		client = SafeHTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, &workflow.ToolError{Kind: "crashed", Message: err.Error(), Retryable: true}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, &workflow.ToolError{Kind: "crashed", Message: err.Error(), Retryable: true}
	}
	headers := map[string]string{}
	for k, vs := range resp.Header {
		headers[k] = strings.Join(vs, ", ")
	}
	out := map[string]any{"url": rawURL, "status": resp.StatusCode, "ok": resp.StatusCode < 400,
		"headers": headers, "body": string(raw)}
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "json") {
		var parsed any
		if json.Unmarshal(raw, &parsed) == nil {
			out["json"] = parsed
		}
	}
	if resp.StatusCode >= 400 {
		return out, &workflow.ToolError{Kind: "http_error", Message: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300)), Retryable: resp.StatusCode == 429 || resp.StatusCode >= 500}
	}
	return out, nil
}

func (e *Executor) mockResponse(resp MockResponse, rawURL string) (any, error) {
	body := resp.Body
	if resp.File != "" {
		path := resp.File
		if !filepath.IsAbs(path) {
			path = filepath.Join(e.WorkspaceRoot, path)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, &workflow.ToolError{Kind: "mock_error", Message: "mock file: " + err.Error()}
		}
		body = string(raw)
	}
	if resp.JSON != nil {
		raw, _ := json.Marshal(resp.JSON)
		body = string(raw)
	}
	out := map[string]any{"url": rawURL, "status": resp.Status, "ok": resp.Status < 400,
		"headers": map[string]string{"Content-Type": "application/json"}, "body": body, "mocked": true}
	var parsed any
	if json.Unmarshal([]byte(body), &parsed) == nil {
		out["json"] = parsed
	}
	if resp.Status >= 400 {
		return out, &workflow.ToolError{Kind: "http_error", Message: fmt.Sprintf("HTTP %d: %s", resp.Status, truncate(body, 300))}
	}
	return out, nil
}

func (e *Executor) shellExec(ctx context.Context, m *Manifest, input map[string]any, opts ExecOptions) (any, error) {
	command := stringArg(input, "command", "")
	if command == "" {
		return nil, &workflow.ToolError{Kind: "invalid_input", Message: "command is required"}
	}
	if len(m.Permissions.Exec) > 0 {
		allowed := false
		for _, x := range m.Permissions.Exec {
			if filepath.Base(x) == filepath.Base(command) || x == command {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, &workflow.ToolError{Kind: "permission_denied", Message: "executable is not allowed by the manifest: " + command}
		}
	}
	args := []string{}
	if raw, ok := input["args"].([]any); ok {
		for _, a := range raw {
			args = append(args, fmt.Sprint(a))
		}
	}
	cwd := stringArg(input, "cwd", e.WorkspaceRoot)
	if !filepath.IsAbs(cwd) {
		cwd = filepath.Join(e.WorkspaceRoot, cwd)
	}
	resolved, err := e.ResolveFilePath(cwd, false)
	if err != nil {
		return nil, err
	}
	timeout := intArg(input, "timeout_s", 30)
	if timeout <= 0 || timeout > 600 {
		timeout = 30
	}
	tctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	outDir := filepath.Join(e.DataDir, "tmp", ids.New())
	_ = os.MkdirAll(outDir, 0o700)
	defer os.RemoveAll(outDir)
	env := sanitizedEnv()
	if len(m.Permissions.Network.Domains) > 0 {
		proxy, perr := StartProxy(m.Permissions.Network.Domains, nil, opts.Mocks)
		if perr != nil {
			return nil, &workflow.ToolError{Kind: "crashed", Message: "cannot start network proxy: " + perr.Error()}
		}
		defer proxy.Close()
		env = append(env, proxy.ProxyEnv()...)
	}
	res := RunSandboxed(tctx, SandboxSpec{
		Command:    command,
		Args:       args,
		Dir:        resolved,
		Workspace:  e.WorkspaceRoot,
		ToolDir:    opts.ToolDir,
		OutDir:     outDir,
		AllowNet:   len(m.Permissions.Network.Domains) > 0,
		AllowWrite: len(m.Permissions.FS.Write) > 0 || m.SideEffect == "write",
		AllowExec:  m.Permissions.Exec,
		DenyRead:   sandboxDenyPaths(e),
		Env:        env,
	})
	if res.TimedOut {
		return nil, &workflow.ToolError{Kind: "timeout", Message: "command timed out", Retryable: true}
	}
	output := map[string]any{
		"command": command, "args": args, "cwd": resolved, "exit_code": res.ExitCode,
		"stdout": truncate(res.Stdout, 512*1024), "stderr": truncate(res.Stderr, 512*1024),
	}
	return output, nil
}

func (e *Executor) workspaceInfo(ctx context.Context, input map[string]any) (any, error) {
	info := map[string]any{
		"root": e.WorkspaceRoot, "os": runtime.GOOS, "arch": runtime.GOARCH,
		"date": time.Now().Format(time.RFC3339), "app": "MyAgent 1.0",
	}
	for _, name := range []string{"python3", "node", "git", "rg", "jq"} {
		if p, err := exec.LookPath(name); err == nil {
			info[name] = p
		}
	}
	return info, nil
}

func textStats(input map[string]any) (any, error) {
	text := stringArg(input, "text", "")
	words := len(strings.Fields(text))
	lines := 0
	if text != "" {
		lines = strings.Count(text, "\n") + 1
	}
	return map[string]any{"chars": len([]rune(text)), "words": words, "lines": lines}, nil
}

func stringArg(input map[string]any, key, def string) string {
	if v, ok := input[key]; ok {
		switch x := v.(type) {
		case string:
			return x
		case nil:
			return def
		default:
			return fmt.Sprint(x)
		}
	}
	return def
}

func intArg(input map[string]any, key string, def int) int {
	v, ok := input[key]
	if !ok {
		return def
	}
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	case string:
		if n, err := strconv.Atoi(x); err == nil {
			return n
		}
	}
	return def
}

func relativeTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func sanitizedEnv() []string {
	out := []string{
		"PATH=/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin",
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		"HOME=" + os.Getenv("HOME"),
		"TMPDIR=" + os.Getenv("TMPDIR"),
	}
	return out
}
