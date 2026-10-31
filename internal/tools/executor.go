package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"myagent/internal/schema"
	"myagent/internal/workflow"
)

type ExecOptions struct {
	ToolDir  string
	ToolName string
	Manifest *Manifest
	Secrets  map[string]string
	Mocks    map[string]MockResponse
	RunID    string
	IdemKey  string
	OnLog    func(stream, text string)
	DryRun   bool
	Source   string
}

type Executor struct {
	WorkspaceRoot string
	DataDir       string
	HTTP          *http.Client
}

func NewExecutor(workspaceRoot, dataDir string) *Executor {
	return &Executor{WorkspaceRoot: workspaceRoot, DataDir: dataDir, HTTP: SafeHTTPClient()}
}

func (e *Executor) Execute(
	ctx context.Context,
	m *Manifest,
	input map[string]any,
	opts ExecOptions,
) (any, error) {
	if m == nil {
		return nil, &workflow.ToolError{
			Kind:    "invalid_manifest",
			Message: "tool manifest is missing",
		}
	}
	if opts.Manifest == nil {
		opts.Manifest = m
	}
	if input == nil {
		input = map[string]any{}
	}
	// Normalize values decoded from YAML (int, int64) to JSON types so schema
	// validation and tool runtimes see one representation.
	if raw, err := json.Marshal(input); err == nil {
		var norm map[string]any
		if json.Unmarshal(raw, &norm) == nil {
			input = norm
		}
	}
	if iss := ValidateInput(m, input); len(iss) > 0 {
		return nil, &workflow.ToolError{Kind: "invalid_input", Message: formatIssues(iss)}
	}
	timeout := time.Duration(m.Limits.TimeoutS) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var out any
	var err error
	switch m.Kind {
	case "builtin":
		out, err = e.executeBuiltin(ctx, m, input, opts)
	case "starlark":
		out, err = e.executeStarlark(ctx, m, input, opts)
	case "script":
		out, err = e.executeScript(ctx, m, input, opts)
	case "mcp":
		err = &workflow.ToolError{
			Kind:    "unsupported",
			Message: "MCP tools are managed by the MCP client, not the local executor",
		}
	default:
		err = &workflow.ToolError{Kind: "invalid_manifest", Message: "unknown tool kind " + m.Kind}
	}
	if err != nil {
		return nil, err
	}
	// Normalize runtime values (Starlark ints, Go maps) to JSON numbers.
	if raw, err := json.Marshal(out); err == nil {
		var norm any
		if json.Unmarshal(raw, &norm) == nil {
			out = norm
		}
	}
	// Enforce the output cap after conversion.
	raw, _ := json.Marshal(out)
	limit := m.Limits.OutputKB * 1024
	if limit > 0 && len(raw) > limit {
		return nil, &workflow.ToolError{
			Kind:    "output_too_large",
			Message: fmt.Sprintf("tool output is %d bytes, limit is %d", len(raw), limit),
		}
	}
	if iss := ValidateOutput(m, out); len(iss) > 0 {
		return nil, &workflow.ToolError{
			Kind: "invalid_output",
			Message: "tool output does not match output_schema: " + formatIssues(
				iss,
			) + " (got " + truncate(
				string(raw),
				300,
			) + ")",
		}
	}
	return out, nil
}

func formatIssues(iss []schema.Issue) string {
	parts := make([]string, 0, len(iss))
	for _, i := range iss {
		if i.Path != "" {
			parts = append(parts, i.Path+": "+i.Message)
		} else {
			parts = append(parts, i.Message)
		}
	}
	return strings.Join(parts, "; ")
}

func (e *Executor) ResolveFilePath(inputPath string, forWrite bool) (string, error) {
	if strings.TrimSpace(inputPath) == "" {
		return e.WorkspaceRoot, nil
	}
	p := inputPath
	if !filepath.IsAbs(p) {
		p = filepath.Join(e.WorkspaceRoot, p)
	}
	p = filepath.Clean(p)
	root := filepath.Clean(e.WorkspaceRoot)
	// Resolve symlinks on the longest existing prefix.
	real, err := realPath(p)
	if err != nil {
		return "", err
	}
	realRoot, err := realPath(root)
	if err != nil {
		realRoot = root
	}
	if real != realRoot && !strings.HasPrefix(real, realRoot+string(os.PathSeparator)) {
		return "", &workflow.ToolError{
			Kind:    "permission_denied",
			Message: "path is outside the workspace: " + p,
		}
	}
	_ = forWrite
	return real, nil
}

func realPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	// Walk up until an existing path is found.
	cur := abs
	suffix := []string{}
	for {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(append([]string{resolved}, reverse(suffix)...)...), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs, nil
		}
		suffix = append(suffix, filepath.Base(cur))
		cur = parent
	}
}

func reverse(xs []string) []string {
	out := make([]string, len(xs))
	for i := range xs {
		out[len(xs)-1-i] = xs[i]
	}
	return out
}

func (e *Executor) log(opts ExecOptions, stream, text string) {
	if opts.OnLog != nil && text != "" {
		opts.OnLog(stream, text)
	}
}

func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func mockFor(mocks map[string]MockResponse, method, url string) (MockResponse, bool) {
	if len(mocks) == 0 {
		return MockResponse{}, false
	}
	key := strings.ToUpper(method) + " " + url
	if r, ok := mocks[key]; ok {
		return r, true
	}
	if r, ok := mocks[url]; ok {
		return r, true
	}
	return MockResponse{}, false
}

func checkDomain(permissions []string, rawURL string) error {
	if len(permissions) == 0 {
		return nil
	}
	host := rawURL
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	if i := strings.LastIndex(host, ":"); i >= 0 && !strings.Contains(host, "]") {
		host = host[:i]
	}
	for _, d := range permissions {
		if strings.EqualFold(host, d) ||
			strings.HasSuffix(strings.ToLower(host), "."+strings.ToLower(d)) {
			return nil
		}
	}
	return &workflow.ToolError{
		Kind:    "permission_denied",
		Message: "domain is not allowed by the manifest: " + host,
	}
}

func fileSecretEnv(decl SecretDecl, value, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, decl.Name)
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func classifyExecError(stage string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &workflow.ToolError{Kind: "timeout", Message: stage + " timed out", Retryable: true}
	}
	return &workflow.ToolError{Kind: "crashed", Message: stage + ": " + err.Error()}
}
