package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"myagent/internal/workflow"
)

// starlarkFSModule exposes workspace-scoped filesystem primitives to
// agent-authored Starlark tools. Tools are ordinary tools; this module is part
// of their runtime, not a built-in tool registry. Every path is resolved
// through ResolveFilePath, so it can never escape the workspace.
func (e *Executor) starlarkFSModule(m *Manifest) *starlarkstruct.Struct {
	readAllowed := len(m.Permissions.FS.Read) > 0
	writeAllowed := len(m.Permissions.FS.Write) > 0
	requireRead := func() error {
		if !readAllowed {
			return &workflow.ToolError{
				Kind:    "permission_denied",
				Message: "tool manifest does not declare fs read permission",
			}
		}
		return nil
	}
	requireWrite := func() error {
		if !writeAllowed {
			return &workflow.ToolError{
				Kind:    "permission_denied",
				Message: "tool manifest does not declare fs write permission",
			}
		}
		if m.SideEffect == "read" {
			return &workflow.ToolError{
				Kind:    "permission_denied",
				Message: "read-only tool cannot write files",
			}
		}
		return nil
	}
	list := starlark.NewBuiltin(
		"list",
		func(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			if err := requireRead(); err != nil {
				return nil, err
			}
			path := "."
			limit := 200
			if err := starlark.UnpackArgs(
				"list",
				args,
				kwargs,
				"path?",
				&path,
				"limit?",
				&limit,
			); err != nil {
				return nil, err
			}
			out, err := e.fsList(map[string]any{"path": path, "limit": limit})
			if err != nil {
				return nil, err
			}
			return goToStarlark(out), nil
		},
	)
	readText := starlark.NewBuiltin(
		"read_text",
		func(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			if err := requireRead(); err != nil {
				return nil, err
			}
			var path string
			maxBytes := 1 << 20
			if err := starlark.UnpackArgs(
				"read_text", args, kwargs, "path", &path, "max_bytes?", &maxBytes,
			); err != nil {
				return nil, err
			}
			out, err := e.fsReadText(map[string]any{"path": path, "max_bytes": maxBytes})
			if err != nil {
				return nil, err
			}
			return goToStarlark(out), nil
		},
	)
	writeText := starlark.NewBuiltin(
		"write_text",
		func(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			if err := requireWrite(); err != nil {
				return nil, err
			}
			var path, content string
			appendMode := false
			if err := starlark.UnpackArgs(
				"write_text",
				args,
				kwargs,
				"path",
				&path,
				"content",
				&content,
				"append?",
				&appendMode,
			); err != nil {
				return nil, err
			}
			out, err := e.fsWriteText(map[string]any{
				"path": path, "content": content, "append": appendMode,
			})
			if err != nil {
				return nil, err
			}
			return goToStarlark(out), nil
		},
	)
	stat := starlark.NewBuiltin(
		"stat",
		func(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			if err := requireRead(); err != nil {
				return nil, err
			}
			var path string
			if err := starlark.UnpackArgs("stat", args, kwargs, "path", &path); err != nil {
				return nil, err
			}
			out, err := e.fsStat(map[string]any{"path": path})
			if err != nil {
				return nil, err
			}
			return goToStarlark(out), nil
		},
	)
	del := starlark.NewBuiltin(
		"delete",
		func(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			if err := requireWrite(); err != nil {
				return nil, err
			}
			var path string
			recursive := false
			if err := starlark.UnpackArgs(
				"delete",
				args,
				kwargs,
				"path",
				&path,
				"recursive?",
				&recursive,
			); err != nil {
				return nil, err
			}
			out, err := e.fsDelete(map[string]any{"path": path, "recursive": recursive})
			if err != nil {
				return nil, err
			}
			return goToStarlark(out), nil
		},
	)
	return starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"list": list, "read_text": readText, "write_text": writeText,
		"stat": stat, "delete": del,
	})
}

func (e *Executor) fsList(input map[string]any) (any, error) {
	path := stringArg(input, "path", "")
	resolved, err := e.ResolveFilePath(path, false)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &workflow.ToolError{
				Kind:    "not_found",
				Message: "directory does not exist: " + resolved,
			}
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
	return map[string]any{
		"path":    resolved,
		"entries": out,
		"files":   files,
		"dirs":    dirs,
		"count":   len(out),
	}, nil
}

func (e *Executor) fsReadText(input map[string]any) (any, error) {
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

func (e *Executor) fsWriteText(input map[string]any) (any, error) {
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
	return map[string]any{
		"path":          resolved,
		"relative":      relativeTo(e.WorkspaceRoot, resolved),
		"bytes_written": n,
	}, nil
}

func (e *Executor) fsStat(input map[string]any) (any, error) {
	path := stringArg(input, "path", "")
	resolved, err := e.ResolveFilePath(path, false)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, &workflow.ToolError{Kind: "not_found", Message: err.Error()}
	}
	return map[string]any{
		"path":     resolved,
		"name":     info.Name(),
		"size":     info.Size(),
		"is_dir":   info.IsDir(),
		"modified": info.ModTime().UnixMilli(),
		"mode":     info.Mode().String(),
	}, nil
}

func (e *Executor) fsDelete(input map[string]any) (any, error) {
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
		return nil, &workflow.ToolError{
			Kind:    "permission_denied",
			Message: "refusing to delete the workspace root",
		}
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

func (e *Executor) mockResponse(resp MockResponse, rawURL string) (any, error) {
	body := resp.Body
	if resp.File != "" {
		path := resp.File
		if !filepath.IsAbs(path) {
			path = filepath.Join(e.WorkspaceRoot, path)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, &workflow.ToolError{
				Kind:    "mock_error",
				Message: "mock file: " + err.Error(),
			}
		}
		body = string(raw)
	}
	if resp.JSON != nil {
		raw, _ := json.Marshal(resp.JSON)
		body = string(raw)
	}
	out := map[string]any{"url": rawURL, "status": resp.Status, "ok": resp.Status < 400,
		"headers": map[string]string{
			"Content-Type": "application/json",
		}, "body": body, "mocked": true}
	var parsed any
	if json.Unmarshal([]byte(body), &parsed) == nil {
		out["json"] = parsed
	}
	if resp.Status >= 400 {
		return out, &workflow.ToolError{
			Kind:    "http_error",
			Message: fmt.Sprintf("HTTP %d: %s", resp.Status, truncate(body, 300)),
		}
	}
	return out, nil
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
