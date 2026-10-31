package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"myagent/internal/workflow"
)

type TestResult struct {
	Name       string   `json:"name"`
	Passed     bool     `json:"passed"`
	Error      string   `json:"error,omitempty"`
	Output     any      `json:"output,omitempty"`
	Logs       []string `json:"logs,omitempty"`
	DurationMS int64    `json:"duration_ms"`
}

func (e *Executor) RunTests(ctx context.Context, m *Manifest, toolDir string, secrets map[string]string) []TestResult {
	results := make([]TestResult, 0, len(m.Tests))
	for i := range m.Tests {
		tc := &m.Tests[i]
		start := time.Now()
		logs := []string{}
		mocks := m.MockMap(tc)
		for key, resp := range mocks {
			if resp.File != "" && !filepath.IsAbs(resp.File) {
				resp.File = filepath.Join(toolDir, resp.File)
				mocks[key] = resp
			}
		}
		sec := map[string]string{}
		for k, v := range secrets {
			sec[k] = v
		}
		for _, d := range m.Secrets {
			if _, ok := sec[d.Name]; !ok {
				sec[d.Name] = "test-value-" + strings.ToLower(d.Name)
			}
		}
		tctx, cancel := context.WithTimeout(ctx, time.Duration(m.Limits.TimeoutS)*time.Second)
		out, err := e.Execute(tctx, m, tc.Input, ExecOptions{
			ToolDir: toolDir, ToolName: m.Name, Manifest: m, Secrets: sec, Mocks: mocks,
			RunID: "test", IdemKey: "test:" + tc.Name,
			OnLog: func(stream, text string) {
				logs = append(logs, stream+": "+text)
			},
		})
		cancel()
		res := TestResult{Name: tc.Name, DurationMS: time.Since(start).Milliseconds(), Logs: logs, Output: out}
		if tc.ExpectError != "" {
			if err == nil {
				res.Passed = false
				res.Error = fmt.Sprintf("expected error %q, but the tool succeeded", tc.ExpectError)
			} else if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.ExpectError)) {
				res.Passed = false
				res.Error = fmt.Sprintf("expected error %q, got %q", tc.ExpectError, err.Error())
			} else {
				res.Passed = true
				res.Error = err.Error()
			}
		} else if err != nil {
			res.Passed = false
			res.Error = err.Error()
		} else {
			res.Passed = true
			for _, assertion := range tc.Expect {
				env := &workflow.Env{
					Inputs: map[string]any{},
					Steps:  map[string]workflow.StepView{},
					Vars:   map[string]any{"output": normalizeAny(out), "input": normalizeAny(tc.Input)},
				}
				ok, aerr := workflow.EvalExpression(assertion, env)
				if aerr != nil {
					res.Passed = false
					res.Error = "assertion " + assertion + ": " + aerr.Error()
					break
				}
				if b, isBool := ok.(bool); !isBool || !b {
					res.Passed = false
					res.Error = "assertion failed: " + assertion
					break
				}
			}
		}
		results = append(results, res)
	}
	return results
}

func normalizeAny(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	_ = json.Unmarshal(raw, &out)
	return out
}
