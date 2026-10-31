package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"myagent/internal/ids"
	"myagent/internal/model"
)

var errBudgetExceeded = errors.New("budget_exceeded")

type executor struct {
	e    *Engine
	spec *Spec
	req  RunRequest

	runCtx context.Context
	cancel context.CancelFunc

	mu             sync.Mutex
	tokensIn       int
	tokensOut      int
	costMicro      int64
	budgetExceeded bool
	fatalErr       error
}

// Run executes a workflow against a run row that already exists. The engine
// owns run/step status projection and event emission.
func (e *Engine) Run(ctx context.Context, spec *Spec, req RunRequest) RunResult {
	ctx2 := ctx
	var cancel context.CancelFunc
	if d, err := parseDuration(spec.Settings.Timeout); err == nil && d > 0 {
		ctx2, cancel = context.WithTimeout(ctx, d)
	} else {
		ctx2, cancel = context.WithCancel(ctx)
	}
	ex := &executor{e: e, spec: spec, req: req, runCtx: ctx2, cancel: cancel}
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			ex.emit(context.Background(), "run.finished", "", map[string]any{
				"status": model.RunFailed,
				"error":  fmt.Sprintf("internal error: %v", r),
			}, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(
					context.Background(),
					`UPDATE runs SET status=?, finished_at=?, error=? WHERE id=?`,
					model.RunFailed,
					ids.NowMS(),
					fmt.Sprintf("internal error: %v", r),
					req.RunID,
				)
				return err
			})
			panic(r)
		}
	}()

	if r, err := e.Store.GetRun(ctx2, req.RunID); err == nil {
		ex.tokensIn, ex.tokensOut, ex.costMicro = r.TokensIn, r.TokensOut, r.CostMicro
	}

	startPayload := map[string]any{
		"name":       spec.Name,
		"target":     "workflow",
		"source":     req.Source,
		"inputs":     req.Inputs,
		"dry_run":    req.DryRun,
		"resume":     req.Resume,
		"hash":       spec.Hash(),
		"unattended": req.Unattended,
	}
	start := ex.emit(ctx, "run.started", "", startPayload, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(
			context.Background(),
			`UPDATE runs SET status='running', started_at=?, finished_at=NULL WHERE id=?`,
			ids.NowMS(),
			req.RunID,
		)
		return err
	})
	_ = start

	env := &Env{
		Inputs: req.Inputs,
		Steps:  map[string]StepView{},
		Run: map[string]any{
			"id":     req.RunID,
			"source": req.Source,
		},
	}
	if spec.Defaults.Role != "" {
		env.Vars = map[string]any{"default_role": spec.Defaults.Role}
	}

	stepOutputs, statuses, execErr := ex.executeList(ctx2, spec.Steps, env, "", 0)
	finalStatus := model.RunSucceeded
	finalErr := ""
	budgetExceeded := false
	ex.mu.Lock()
	budgetExceeded = ex.budgetExceeded
	ex.mu.Unlock()
	switch {
	case budgetExceeded:
		finalStatus = model.RunFailed
		finalErr = "budget_exceeded: workflow exceeded its token or cost budget"
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx2.Err(), context.DeadlineExceeded):
		finalStatus = model.RunFailed
		finalErr = "run_timeout: workflow exceeded its total timeout"
	case ctx.Err() != nil:
		finalStatus = model.RunCancelled
		finalErr = "cancelled by user"
	case execErr != nil:
		finalStatus = model.RunFailed
		finalErr = execErr.Error()
	}

	// Evaluate outputs only for successful or partially successful runs.
	outputEnvSteps := map[string]StepView{}
	for _, st := range spec.Steps {
		outputEnvSteps[st.ID] = StepView{Output: stepOutputs[st.ID], Status: statuses[st.ID]}
	}
	outEnv := &Env{Inputs: req.Inputs, Steps: outputEnvSteps, Run: env.Run}
	values := map[string]any{}
	for name, expr := range spec.Outputs {
		v, err := evalWorkflowString(expr, outEnv)
		if err != nil {
			values[name] = nil
			if finalStatus == model.RunSucceeded {
				finalStatus = model.RunFailed
				finalErr = fmt.Sprintf("output %s: %v", name, err)
			}
			continue
		}
		values[name] = v
	}
	tokensIn, tokensOut, cost := ex.usage()
	if finalStatus == "" {
		finalStatus = model.RunSucceeded
	}
	endPayload := map[string]any{
		"status":     finalStatus,
		"tokens":     map[string]any{"in": tokensIn, "out": tokensOut},
		"cost_micro": cost,
		"error":      finalErr,
		"outputs":    values,
		"warnings":   ex.continuedFailures(statuses),
	}
	ex.emit(context.Background(), "run.finished", "", endPayload, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(
			context.Background(),
			`UPDATE runs SET status=?, finished_at=?, error=?, tokens_in=?, tokens_out=?, cost_micro=? WHERE id=?`,
			finalStatus,
			ids.NowMS(),
			nullIfEmpty(finalErr),
			tokensIn,
			tokensOut,
			cost,
			req.RunID,
		)
		return err
	})
	return RunResult{Status: finalStatus, Outputs: values, TokensIn: tokensIn, TokensOut: tokensOut,
		CostMicro: cost, Error: finalErr, StepStatuses: normalizeStatuses(statuses)}
}

func (ex *executor) continuedFailures(statuses map[string]string) []string {
	out := []string{}
	for _, st := range ex.spec.Steps {
		if statuses[st.ID] == "failed" && st.OnError == "continue" {
			out = append(out, st.ID)
		}
	}
	return out
}

func (ex *executor) usage() (int, int, int64) {
	ex.mu.Lock()
	defer ex.mu.Unlock()
	return ex.tokensIn, ex.tokensOut, ex.costMicro
}

func (ex *executor) addUsage(tokensIn, tokensOut int, cost int64) error {
	ex.mu.Lock()
	ex.tokensIn += tokensIn
	ex.tokensOut += tokensOut
	ex.costMicro += cost
	over := false
	if b := ex.spec.Settings.Budget; b != nil {
		if b.MaxTokens > 0 && ex.tokensIn+ex.tokensOut > b.MaxTokens {
			ex.budgetExceeded = true
			over = true
		}
		if b.MaxCostUSD > 0 && float64(ex.costMicro)/1_000_000 > b.MaxCostUSD {
			ex.budgetExceeded = true
			over = true
		}
	}
	ex.mu.Unlock()
	if over {
		ex.cancel()
		return errBudgetExceeded
	}
	return nil
}

func (ex *executor) emit(
	ctx context.Context,
	typ, stepID string,
	payload any,
	projection func(*sql.Tx) error,
) model.Event {
	pc := context.WithoutCancel(ctx)
	pc, cancel := context.WithTimeout(pc, 5*time.Second)
	defer cancel()
	evt := model.Event{
		Stream:    model.RunStream(ex.req.RunID),
		Type:      typ,
		TS:        model.Now(),
		RunID:     ex.req.RunID,
		StepID:    stepID,
		SchemaVer: 1,
		Payload:   ex.e.redactPayload(payload),
	}
	out, err := ex.e.Bus.Emit(pc, evt, projection)
	if err != nil {
		println("workflow event:", typ, err.Error())
	}
	return out
}

func (ex *executor) emitDelta(typ, stepID string, payload any) {
	// Deltas are ephemeral but still cross the same trust boundary as persisted
	// events, so redact them too.
	ex.e.Bus.Delta(
		model.RunStream(ex.req.RunID),
		typ,
		ex.req.RunID,
		stepID,
		ex.e.redactPayload(payload),
	)
}

type stepOutcome struct {
	id     string
	output any
	status string
	err    error
}

func (ex *executor) executeList(
	ctx context.Context,
	steps []Step,
	parent *Env,
	prefix string,
	depth int,
) (map[string]any, map[string]string, error) {
	if depth > 8 {
		return nil, nil, errors.New("workflow nesting is too deep")
	}
	env := copyEnv(parent)
	statuses := map[string]string{}
	outputs := map[string]any{}
	pending := map[string]bool{}
	for _, st := range steps {
		pending[st.ID] = true
	}

	// Resume: a successful step is never executed twice.
	if ex.req.Resume {
		for _, st := range steps {
			path := stepPath(prefix, st.ID)
			sr, ok, err := ex.e.Store.SucceededStepAttempt(ctx, ex.req.RunID, path)
			if err == nil && ok {
				val, lerr := ex.loadOutput(ctx, sr.OutputRef)
				if lerr == nil {
					statuses[st.ID] = "succeeded"
					outputs[st.ID] = val
					env.Steps[st.ID] = StepView{Output: val, Status: "succeeded"}
					pending[st.ID] = false
				}
			}
		}
	}

	running := 0
	finished := make(chan stepOutcome, len(steps))
	maxParallel := 8
	var fatal error
	for {
		if ctx.Err() != nil {
			for id := range pending {
				if pending[id] {
					statuses[id] = "cancelled"
					pending[id] = false
				}
			}
		}
		// Propagate skips caused by failed dependencies.
		for {
			changed := false
			for _, st := range steps {
				if !pending[st.ID] {
					continue
				}
				ready, skip, fail := ex.dependencies(st, statuses, steps)
				if !ready {
					continue
				}
				if skip || fail {
					path := stepPath(prefix, st.ID)
					statuses[st.ID] = "skipped_dependency"
					pending[st.ID] = false
					outputs[st.ID] = nil
					env.Steps[st.ID] = StepView{Output: nil, Status: "skipped"}
					reason := "a dependency failed or was skipped"
					ex.recordSkipped(ctx, path, reason)
					changed = true
				}
			}
			if !changed {
				break
			}
		}
		startedNow := 0
		for _, st := range steps {
			if !pending[st.ID] || running >= maxParallel {
				continue
			}
			ready, skip, fail := ex.dependencies(st, statuses, steps)
			if !ready || skip || fail {
				continue
			}
			pending[st.ID] = false
			running++
			startedNow++
			snapshot := copyEnv(env)
			stCopy := st
			go func() {
				output, status, err := ex.executeStep(
					ctx,
					&stCopy,
					stepPath(prefix, stCopy.ID),
					snapshot,
					depth,
					steps,
				)
				finished <- stepOutcome{id: stCopy.ID, output: output, status: status, err: err}
			}()
		}
		_ = startedNow
		if running == 0 {
			remaining := 0
			for _, p := range pending {
				if p {
					remaining++
				}
			}
			if remaining == 0 {
				break
			}
			// No runnable step means a cycle or an invalid dependency; validation
			// should have caught this, so fail defensively.
			fatal = errors.New("workflow is stuck: no runnable step")
			for id := range pending {
				if pending[id] {
					statuses[id] = "failed"
					pending[id] = false
				}
			}
			break
		}
		oc := <-finished
		running--
		outputs[oc.id] = oc.output
		statuses[oc.id] = oc.status
		env.Steps[oc.id] = StepView{Output: oc.output, Status: oc.status}
		if oc.err != nil && fatal == nil {
			st := findStep(steps, oc.id)
			if st == nil || st.OnError != "continue" {
				fatal = oc.err
			}
		}
		if oc.err != nil && errors.Is(oc.err, context.Canceled) && ctx.Err() != nil {
			// Drain cleanly; remaining steps will be marked cancelled.
			continue
		}
	}
	for _, st := range steps {
		if _, ok := statuses[st.ID]; !ok {
			statuses[st.ID] = "skipped"
		}
	}
	if fatal != nil {
		return outputs, statuses, fatal
	}
	if ctx.Err() != nil {
		return outputs, statuses, ctx.Err()
	}
	return outputs, statuses, nil
}

func (ex *executor) dependencies(
	st Step,
	statuses map[string]string,
	siblings []Step,
) (ready, skip, fail bool) {
	for _, need := range st.Needs {
		status, ok := statuses[need]
		if !ok {
			return false, false, false
		}
		if status == "failed" {
			dep := findStep(siblings, need)
			if dep == nil || dep.OnError != "continue" {
				fail = true
			}
		}
		if status == "skipped_dependency" {
			skip = true
		}
		if status == "skipped_condition" {
			// A false `when` is success with a null output by design.
		}
	}
	return true, skip, fail
}

func (ex *executor) recordSkipped(ctx context.Context, path, reason string) {
	attempt, _ := ex.e.Store.LatestStepAttempt(ctx, ex.req.RunID, path)
	attempt++
	sr, err := ex.e.Store.StartStepRun(ctx, ex.req.RunID, path, attempt, "", "")
	if err == nil {
		_ = ex.e.Store.FinishStepRun(ctx, sr.ID, "skipped", "", reason)
	}
	ex.emit(ctx, "step.skipped", path, map[string]any{"step_id": path, "reason": reason}, nil)
}

func (ex *executor) executeStep(
	ctx context.Context,
	st *Step,
	path string,
	env *Env,
	depth int,
	siblings []Step,
) (any, string, error) {
	if ex.e.Controls != nil {
		if err := ex.e.Controls.WaitIfPaused(ctx, ex.req.RunID); err != nil {
			return nil, "cancelled", err
		}
	}
	// Resume check (top-level list already does this, but nested lists reach here
	// directly through branch/map).
	if ex.req.Resume {
		if sr, ok, err := ex.e.Store.SucceededStepAttempt(ctx, ex.req.RunID, path); err == nil &&
			ok {
			val, lerr := ex.loadOutput(ctx, sr.OutputRef)
			if lerr == nil {
				return val, "succeeded", nil
			}
		}
	}
	if st.When != "" {
		ok, err := ex.evalCondition(st.When, env)
		if err != nil {
			return nil, "failed", fmt.Errorf("when: %w", err)
		}
		if !ok {
			sr, serr := ex.e.Store.StartStepRun(
				ctx,
				ex.req.RunID,
				path,
				nextAttempt(ctx, ex, path),
				"",
				"",
			)
			if serr == nil {
				_ = ex.e.Store.FinishStepRun(ctx, sr.ID, "skipped", "", "condition was false")
			}
			ex.emit(
				ctx,
				"step.skipped",
				path,
				map[string]any{"step_id": path, "reason": "condition was false"},
				nil,
			)
			return nil, "skipped_condition", nil
		}
	}

	kind := st.StepKind()
	retry := ex.spec.EffectiveRetry(st)
	maxAttempts := 1
	if retry != nil {
		maxAttempts += retry.Max
	}
	if maxAttempts > 6 {
		maxAttempts = 6
	}
	attempt := nextAttempt(ctx, ex, path)
	attemptNum := attempt
	for i := 0; i < maxAttempts; i++ {
		if ctx.Err() != nil {
			return nil, "cancelled", ctx.Err()
		}
		sr, err := ex.e.Store.StartStepRun(
			ctx,
			ex.req.RunID,
			path,
			attemptNum,
			"",
			ex.req.RunID+":"+path,
		)
		if err != nil {
			return nil, "failed", err
		}
		ex.emit(ctx, "step.started", path, map[string]any{
			"step_id": path, "attempt": attemptNum, "kind": kind, "name": st.Name,
		}, nil)
		start := time.Now()
		stepCtx := ctx
		var stepCancel context.CancelFunc
		if d, derr := parseDuration(ex.spec.EffectiveTimeout(st)); derr == nil && d > 0 {
			stepCtx, stepCancel = context.WithTimeout(ctx, d)
		}
		output, execErr := ex.runKind(stepCtx, st, path, env, depth, sr, siblings)
		if stepCancel != nil {
			stepCancel()
		}
		if execErr == nil {
			ref, serr := ex.saveOutput(ctx, sr.ID, path, output)
			if serr != nil {
				execErr = serr
			} else {
				_ = ex.e.Store.FinishStepRun(ctx, sr.ID, "succeeded", ref, "")
				ex.emit(ctx, "step.finished", path, map[string]any{
					"step_id":     path,
					"status":      "succeeded",
					"duration_ms": time.Since(start).Milliseconds(),
					"output_ref":  ref,
					"output":      previewValue(output),
				}, nil)
				return output, "succeeded", nil
			}
		}
		if execErr != nil {
			_ = ex.e.Store.FinishStepRun(ctx, sr.ID, "failed", "", execErr.Error())
			ex.emit(ctx, "step.finished", path, map[string]any{
				"step_id":     path,
				"status":      "failed",
				"duration_ms": time.Since(start).Milliseconds(),
				"error":       execErr.Error(),
				"attempt":     attemptNum,
			}, nil)
			if ctx.Err() != nil {
				return nil, "cancelled", ctx.Err()
			}
			if !retryableError(execErr) || i == maxAttempts-1 {
				return nil, "failed", execErr
			}
			delay := retryDelay(retry, i)
			ex.emit(ctx, "step.retrying", path, map[string]any{
				"attempt": attemptNum, "reason": execErr.Error(), "delay_ms": delay.Milliseconds(),
			}, nil)
			if err := ex.e.delay(ctx, delay); err != nil {
				return nil, "cancelled", err
			}
			attemptNum++
		}
	}
	return nil, "failed", errors.New("step failed")
}

func nextAttempt(ctx context.Context, ex *executor, path string) int {
	n, _ := ex.e.Store.LatestStepAttempt(ctx, ex.req.RunID, path)
	return n + 1
}

func retryDelay(r *RetrySpec, attempt int) time.Duration {
	if r == nil {
		return time.Second
	}
	d, err := parseDuration(r.Delay)
	if err != nil || d <= 0 {
		d = time.Second
	}
	if r.Backoff == "exp" {
		for i := 0; i < attempt; i++ {
			d *= 2
			if d > 30*time.Second {
				return 30 * time.Second
			}
		}
	}
	return d
}

func retryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var te *ToolError
	if errors.As(err, &te) {
		return te.Retryable
	}
	var rt interface{ RetryableError() bool }
	if errors.As(err, &rt) {
		return rt.RetryableError()
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "invalid_input") || strings.Contains(msg, "approval") ||
		strings.Contains(msg, "401") ||
		strings.Contains(msg, "403") {
		return false
	}
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "rate") ||
		strings.Contains(msg, "temporar") ||
		strings.Contains(msg, "connection") {
		return true
	}
	return false
}

func stepPath(prefix, id string) string {
	if prefix == "" {
		return id
	}
	return prefix + id
}

func copyEnv(in *Env) *Env {
	out := &Env{
		Inputs:  in.Inputs,
		Steps:   make(map[string]StepView, len(in.Steps)),
		Item:    in.Item,
		Index:   in.Index,
		HasItem: in.HasItem,
		Run:     in.Run,
		Vars:    map[string]any{},
	}
	for k, v := range in.Steps {
		out.Steps[k] = v
	}
	for k, v := range in.Vars {
		out.Vars[k] = v
	}
	return out
}

func (ex *executor) evalCondition(expr string, env *Env) (bool, error) {
	v, err := evalWorkflowString(expr, env)
	if err != nil {
		return false, err
	}
	if b, ok := v.(bool); ok {
		return b, nil
	}
	if s, ok := v.(string); ok {
		return s == "true" || s == "1", nil
	}
	if f, ok := v.(float64); ok {
		return f != 0, nil
	}
	return v != nil, nil
}

// evalWorkflowString accepts both bare expressions (when/outputs) and ${...}
// templates.
func evalWorkflowString(s string, env *Env) (any, error) {
	if strings.Contains(s, "${") {
		return EvalTemplate(s, env)
	}
	return EvalExpression(s, env)
}

func previewValue(v any) any {
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > 16*1024 {
		return nil
	}
	var out any
	_ = json.Unmarshal(raw, &out)
	return out
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func normalizeStatuses(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		switch v {
		case "skipped_condition", "skipped_dependency":
			out[k] = "skipped"
		default:
			out[k] = v
		}
	}
	return out
}
