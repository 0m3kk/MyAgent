package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"myagent/internal/eventbus"
	"myagent/internal/model"
	"myagent/internal/store"
)

type fakeTools struct {
	mu     sync.Mutex
	calls  int
	failN  int
	inputs []map[string]any
}

func (f *fakeTools) Resolve(ctx context.Context, ref string) (ToolBinding, error) {
	return ToolBinding{Name: "test.echo", VersionID: "tv1", SideEffect: "read", InputSchema: map[string]any{
		"type": "object", "properties": map[string]any{"value": map[string]any{"type": "number"}},
	}}, nil
}

func (f *fakeTools) Execute(ctx context.Context, b ToolBinding, input map[string]any, opts ToolExecOptions) (any, error) {
	f.mu.Lock()
	f.calls++
	call := f.calls
	f.inputs = append(f.inputs, input)
	f.mu.Unlock()
	if call <= f.failN {
		return nil, &ToolError{Kind: "crashed", Message: "temporary failure", Retryable: true}
	}
	return map[string]any{"value": input["value"], "call": call}, nil
}

type fakeApprovals struct{ approve bool }

func (f *fakeApprovals) RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalDecision, error) {
	return ApprovalDecision{Approved: f.approve, Comment: "test"}, nil
}

type fakeLLM struct{}

func (f *fakeLLM) ChatWithTools(ctx context.Context, req LLMChatRequest) (LLMChatResult, error) {
	return LLMChatResult{}, errors.New("not used")
}

func newTestEngine(t *testing.T) (*Engine, *store.Store, *fakeTools) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	tools := &fakeTools{}
	bus := eventbus.New(st)
	eng := &Engine{Store: st, Bus: bus, Tools: tools, LLM: &fakeLLM{}, Approvals: &fakeApprovals{approve: true}, ArtifactDir: dir}
	return eng, st, tools
}

func makeRun(t *testing.T, st *store.Store) model.Run {
	t.Helper()
	run, err := st.CreateRun(context.Background(), model.Run{
		TargetKind: "workflow", TargetVer: "wv1", Source: "manual", Inputs: json.RawMessage(`{}`), Status: model.RunQueued,
	})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

const engineYAML = `schema: 1
name: test
inputs: {}
outputs:
  value: ${steps.echo.output.value}
  approved: ${steps.review.output.approved}
steps:
  - id: seed
    set: {n: 2}
  - id: echo
    needs: [seed]
    tool: test.echo
    with: {value: "${steps.seed.output.n}"}
  - id: review
    needs: [echo]
    approval: {message: "ok?"}
  - id: after
    needs: [review]
    when: ${steps.review.output.approved}
    set: {done: true}
`

func TestEngineSuccess(t *testing.T) {
	eng, st, _ := newTestEngine(t)
	spec, err := Parse([]byte(engineYAML))
	if err != nil {
		t.Fatal(err)
	}
	run := makeRun(t, st)
	res := eng.Run(context.Background(), spec, RunRequest{RunID: run.ID, Source: "manual", Inputs: map[string]any{}})
	if res.Status != model.RunSucceeded {
		t.Fatalf("status %s err %s", res.Status, res.Error)
	}
	if res.Outputs["approved"] != true {
		t.Fatalf("outputs: %+v", res.Outputs)
	}
	if fmt.Sprint(res.Outputs["value"]) != "2" {
		t.Fatalf("value: %#v", res.Outputs["value"])
	}
	dbRun, _ := st.GetRun(context.Background(), run.ID)
	if dbRun.Status != model.RunSucceeded {
		t.Fatalf("db status %s", dbRun.Status)
	}
	events, _ := st.RunEvents(context.Background(), run.ID, 0, 100)
	foundFinish := false
	for _, e := range events {
		if e.Type == "run.finished" {
			foundFinish = true
		}
	}
	if !foundFinish {
		t.Fatal("missing run.finished")
	}
}

func TestEngineApprovalRejectedSkipsDependents(t *testing.T) {
	eng, st, _ := newTestEngine(t)
	eng.Approvals = &fakeApprovals{approve: false}
	spec, _ := Parse([]byte(engineYAML))
	run := makeRun(t, st)
	res := eng.Run(context.Background(), spec, RunRequest{RunID: run.ID, Source: "manual", Inputs: map[string]any{}})
	if res.Status != model.RunSucceeded {
		t.Fatalf("status %s err %s", res.Status, res.Error)
	}
	if res.Outputs["approved"] != false {
		t.Fatalf("outputs: %+v", res.Outputs)
	}
	steps, _ := st.ListStepRuns(context.Background(), run.ID)
	for _, s := range steps {
		if s.StepID == "after" && s.Status != "skipped" {
			t.Fatalf("after should be skipped, got %s", s.Status)
		}
	}
}

func TestEngineRetry(t *testing.T) {
	eng, st, tools := newTestEngine(t)
	tools.failN = 1
	spec, _ := Parse([]byte(`schema: 1
name: retry
inputs: {}
steps:
  - id: echo
    tool: test.echo
    retry: {max: 1, backoff: fixed, delay: 1ms}
    with: {value: 7}
`))
	run := makeRun(t, st)
	res := eng.Run(context.Background(), spec, RunRequest{RunID: run.ID, Source: "manual", Inputs: map[string]any{}})
	if res.Status != model.RunSucceeded {
		t.Fatalf("status %s err %s", res.Status, res.Error)
	}
	if tools.calls != 2 {
		t.Fatalf("expected 2 calls, got %d", tools.calls)
	}
}

func TestEngineResumeSkipsSucceededSteps(t *testing.T) {
	eng, st, tools := newTestEngine(t)
	spec, _ := Parse([]byte(`schema: 1
name: resume
inputs: {}
steps:
  - id: seed
    set: {n: 9}
  - id: echo
    needs: [seed]
    tool: test.echo
    with: {value: "${steps.seed.output.n}"}
`))
	run := makeRun(t, st)
	// Pretend seed succeeded before a crash.
	sr, err := st.StartStepRun(context.Background(), run.ID, "seed", 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishStepRun(context.Background(), sr.ID, "succeeded", `{"n":9}`, ""); err != nil {
		t.Fatal(err)
	}
	res := eng.Run(context.Background(), spec, RunRequest{RunID: run.ID, Source: "manual", Inputs: map[string]any{}, Resume: true})
	if res.Status != model.RunSucceeded {
		t.Fatalf("status %s err %s", res.Status, res.Error)
	}
	if len(tools.inputs) != 1 || tools.inputs[0]["value"] != float64(9) {
		t.Fatalf("tool inputs: %+v", tools.inputs)
	}
}
