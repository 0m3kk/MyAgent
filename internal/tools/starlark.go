package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"myagent/internal/workflow"
)

func (e *Executor) executeStarlark(
	ctx context.Context,
	m *Manifest,
	input map[string]any,
	opts ExecOptions,
) (any, error) {
	entry := m.Runtime.Entry
	if entry == "" {
		entry = "main.star"
	}
	path := entry
	if !filepath.IsAbs(path) {
		path = filepath.Join(opts.ToolDir, entry)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, &workflow.ToolError{
			Kind:    "invalid_tool",
			Message: "cannot read " + entry + ": " + err.Error(),
		}
	}
	thread := &starlark.Thread{Name: "tool:" + m.Name}
	thread.Print = func(th *starlark.Thread, msg string) {
		e.log(opts, "stdout", msg)
	}
	thread.SetMaxExecutionSteps(5_000_000)
	httpModule := e.starlarkHTTPModule(ctx, m, opts)
	jsonModule := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"encode": starlark.NewBuiltin(
			"encode",
			func(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
				var v starlark.Value
				if err := starlark.UnpackArgs("encode", args, kwargs, "value", &v); err != nil {
					return nil, err
				}
				raw, err := json.Marshal(starlarkToGo(v))
				if err != nil {
					return nil, err
				}
				return starlark.String(raw), nil
			},
		),
		"decode": starlark.NewBuiltin(
			"decode",
			func(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
				var s string
				if err := starlark.UnpackArgs("decode", args, kwargs, "text", &s); err != nil {
					return nil, err
				}
				var v any
				if err := json.Unmarshal([]byte(s), &v); err != nil {
					return nil, err
				}
				return goToStarlark(v), nil
			},
		),
	})
	secretFn := starlark.NewBuiltin(
		"secret",
		func(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var name string
			if err := starlark.UnpackArgs("secret", args, kwargs, "name", &name); err != nil {
				return nil, err
			}
			if v, ok := opts.Secrets[name]; ok {
				return starlark.String(v), nil
			}
			return starlark.None, nil
		},
	)
	logFn := starlark.NewBuiltin(
		"log",
		func(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			parts := []string{}
			for _, a := range args {
				if s, ok := starlark.AsString(a); ok {
					parts = append(parts, s)
				} else {
					parts = append(parts, a.String())
				}
			}
			e.log(opts, "stdout", strings.Join(parts, " "))
			return starlark.None, nil
		},
	)
	predeclared := starlark.StringDict{
		"json":   jsonModule,
		"http":   httpModule,
		"fs":     e.starlarkFSModule(m),
		"secret": secretFn,
		"log":    logFn,
		"ctx": goToStarlark(
			map[string]any{
				"run_id":    opts.RunID,
				"idem_key":  opts.IdemKey,
				"workspace": e.WorkspaceRoot,
			},
		),
	}
	globals, err := starlark.ExecFile(thread, path, src, predeclared)
	if err != nil {
		return nil, starlarkError(err)
	}
	fn, ok := globals["run"]
	if !ok {
		return nil, &workflow.ToolError{
			Kind:    "invalid_tool",
			Message: "tool must define def run(input, ctx)",
		}
	}
	callable, ok := fn.(starlark.Callable)
	if !ok {
		return nil, &workflow.ToolError{Kind: "invalid_tool", Message: "run must be callable"}
	}
	args := starlark.Tuple{
		goToStarlark(input),
		goToStarlark(
			map[string]any{
				"run_id":    opts.RunID,
				"idem_key":  opts.IdemKey,
				"workspace": e.WorkspaceRoot,
			},
		),
	}
	if len(m.Secrets) > 0 {
		_ = m
	}
	res, err := starlark.Call(thread, callable, args, nil)
	if err != nil {
		return nil, starlarkError(err)
	}
	return starlarkToGo(res), nil
}

func starlarkError(err error) error {
	var ee *starlark.EvalError
	if ok := asEval(err, &ee); ok {
		return &workflow.ToolError{Kind: "crashed", Message: ee.Backtrace()}
	}
	return &workflow.ToolError{Kind: "crashed", Message: err.Error()}
}

func asEval(err error, target **starlark.EvalError) bool {
	if e, ok := err.(*starlark.EvalError); ok {
		*target = e
		return true
	}
	return false
}

func (e *Executor) starlarkHTTPModule(
	ctx context.Context,
	m *Manifest,
	opts ExecOptions,
) *starlarkstruct.Struct {
	headerSecrets := map[string][]SecretDecl{}
	for _, d := range m.Secrets {
		if d.Inject == "header" {
			for _, domain := range d.Scope.Domains {
				headerSecrets[strings.ToLower(domain)] = append(
					headerSecrets[strings.ToLower(domain)],
					d,
				)
			}
		}
	}
	request := starlark.NewBuiltin(
		"request",
		func(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var method, rawURL string
			var body string
			var headersVal starlark.Value = starlark.None
			if err := starlark.UnpackArgs(
				"request",
				args,
				kwargs,
				"method",
				&method,
				"url",
				&rawURL,
				"body?",
				&body,
				"headers?",
				&headersVal,
			); err != nil {
				return nil, err
			}
			method = strings.ToUpper(method)
			if _, err := validateFetchURL(rawURL); err != nil {
				return nil, fmt.Errorf("permission denied: %v", err)
			}
			if err := checkDomain(m.Permissions.Network.Domains, rawURL); err != nil {
				return nil, err
			}
			if m.SideEffect == "read" && method != "GET" && method != "HEAD" {
				return nil, fmt.Errorf("permission denied: read-only tool cannot issue %s", method)
			}
			if resp, ok := mockFor(opts.Mocks, method, rawURL); ok {
				out, rerr := e.mockResponse(resp, rawURL)
				if rerr != nil {
					return nil, rerr
				}
				return goToStarlark(out), nil
			}
			headers := map[string]string{}
			if headersVal != starlark.None && headersVal != nil {
				if d, ok := headersVal.(*starlark.Dict); ok {
					for _, item := range d.Items() {
						k, _ := starlark.AsString(item[0])
						v, _ := starlark.AsString(item[1])
						headers[k] = v
					}
				}
			}
			for domain, decls := range headerSecrets {
				if strings.EqualFold(hostOf(rawURL), domain) ||
					strings.HasSuffix(strings.ToLower(hostOf(rawURL)), "."+domain) {
					for _, d := range decls {
						if val, ok := opts.Secrets[d.Name]; ok {
							headers = applyHeaderTemplate(headers, d.Header, val)
						}
					}
				}
			}
			req, err := http.NewRequestWithContext(ctx, method, rawURL, strings.NewReader(body))
			if err != nil {
				return nil, err
			}
			for k, v := range headers {
				req.Header.Set(k, v)
			}
			client := e.HTTP
			if client == nil {
				client = SafeHTTPClient()
			}
			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			out := map[string]any{
				"status":  resp.StatusCode,
				"body":    string(raw),
				"headers": flattenHeaders(resp.Header),
			}
			var parsed any
			if json.Unmarshal(raw, &parsed) == nil {
				out["json"] = parsed
			}
			if resp.StatusCode >= 400 {
				return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
			}
			return goToStarlark(out), nil
		},
	)
	get := starlark.NewBuiltin(
		"get",
		func(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var rawURL string
			var headers starlark.Value = starlark.None
			if err := starlark.UnpackArgs(
				"get",
				args,
				kwargs,
				"url",
				&rawURL,
				"headers?",
				&headers,
			); err != nil {
				return nil, err
			}
			return request.CallInternal(
				th,
				starlark.Tuple{starlark.String("GET"), starlark.String(rawURL)},
				[]starlark.Tuple{{starlark.String("headers"), headers}},
			)
		},
	)
	post := starlark.NewBuiltin(
		"post",
		func(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var rawURL, body string
			var headers starlark.Value = starlark.None
			if err := starlark.UnpackArgs(
				"post",
				args,
				kwargs,
				"url",
				&rawURL,
				"body?",
				&body,
				"headers?",
				&headers,
			); err != nil {
				return nil, err
			}
			return request.CallInternal(
				th,
				starlark.Tuple{starlark.String("POST"), starlark.String(rawURL)},
				[]starlark.Tuple{
					{starlark.String("body"), starlark.String(body)},
					{starlark.String("headers"), headers},
				},
			)
		},
	)
	return starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"request": request, "get": get, "post": post,
	})
}

func applyHeaderTemplate(headers map[string]string, template, value string) map[string]string {
	line := strings.ReplaceAll(template, "${value}", value)
	parts := strings.SplitN(line, ":", 2)
	if len(parts) == 2 {
		headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return headers
}

func hostOf(raw string) string {
	s := raw
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, ":"); i >= 0 && !strings.Contains(s, "]") {
		s = s[:i]
	}
	return s
}

func flattenHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for k, vs := range h {
		out[k] = strings.Join(vs, ", ")
	}
	return out
}

func goToStarlark(v any) starlark.Value {
	switch x := v.(type) {
	case nil:
		return starlark.None
	case bool:
		return starlark.Bool(x)
	case string:
		return starlark.String(x)
	case int:
		return starlark.MakeInt(x)
	case int64:
		return starlark.MakeInt64(x)
	case float64:
		if x == float64(int64(x)) && x < 1e15 && x > -1e15 {
			return starlark.MakeInt64(int64(x))
		}
		return starlark.Float(x)
	case []any:
		vals := make([]starlark.Value, len(x))
		for i, item := range x {
			vals[i] = goToStarlark(item)
		}
		return starlark.NewList(vals)
	case map[string]any:
		d := starlark.NewDict(len(x))
		for k, val := range x {
			_ = d.SetKey(starlark.String(k), goToStarlark(val))
		}
		return d
	case map[string]string:
		d := starlark.NewDict(len(x))
		for k, val := range x {
			_ = d.SetKey(starlark.String(k), starlark.String(val))
		}
		return d
	case starlark.Value:
		return x
	default:
		raw, _ := json.Marshal(x)
		var conv any
		_ = json.Unmarshal(raw, &conv)
		if raw == nil {
			return starlark.None
		}
		return goToStarlark(conv)
	}
}

func starlarkToGo(v starlark.Value) any {
	switch x := v.(type) {
	case starlark.NoneType:
		return nil
	case starlark.Bool:
		return bool(x)
	case starlark.String:
		return string(x)
	case starlark.Int:
		if i, ok := x.Int64(); ok {
			return i
		}
		return x.Float()
	case starlark.Float:
		return float64(x)
	case *starlark.List:
		out := make([]any, x.Len())
		for i := 0; i < x.Len(); i++ {
			out[i] = starlarkToGo(x.Index(i))
		}
		return out
	case starlark.Tuple:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = starlarkToGo(item)
		}
		return out
	case *starlark.Dict:
		out := map[string]any{}
		for _, item := range x.Items() {
			key := item[0].String()
			if s, ok := starlark.AsString(item[0]); ok {
				key = s
			}
			out[key] = starlarkToGo(item[1])
		}
		return out
	case starlark.IterableMapping:
		out := map[string]any{}
		for _, item := range x.Items() {
			key := item[0].String()
			if s, ok := starlark.AsString(item[0]); ok {
				key = s
			}
			out[key] = starlarkToGo(item[1])
		}
		return out
	default:
		return v.String()
	}
}
