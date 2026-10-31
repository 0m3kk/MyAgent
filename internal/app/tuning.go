package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pmezard/go-difflib/difflib"

	"myagent/internal/ids"
	"myagent/internal/llm"
	"myagent/internal/model"
	"myagent/internal/workflow"
)

func (a *App) SetFeedback(ctx context.Context, runID, rating, note string) (model.Feedback, error) {
	if rating != "up" && rating != "down" {
		return model.Feedback{}, errors.New("rating must be up or down")
	}
	if _, err := a.Store.GetRun(ctx, runID); err != nil {
		return model.Feedback{}, err
	}
	f, err := a.Store.UpsertFeedback(
		ctx,
		model.Feedback{TargetKind: "run", TargetID: runID, Rating: rating, Note: note},
	)
	if err != nil {
		return f, err
	}
	a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "feedback.created",
		Payload: a.mustPayload(map[string]any{"run_id": runID, "rating": rating, "note": note})})
	return f, nil
}

func (a *App) CreateEvalCaseFromRun(ctx context.Context, runID string) (model.EvalCase, error) {
	run, err := a.Store.GetRun(ctx, runID)
	if err != nil {
		return model.EvalCase{}, err
	}
	if run.TargetKind != "workflow" {
		return model.EvalCase{}, errors.New("eval cases are created from workflow runs")
	}
	w, err := a.Store.GetWorkflowByID(ctx, run.TargetID)
	if err != nil {
		return model.EvalCase{}, err
	}
	suite, err := a.Store.EvalSuiteByTarget(ctx, w.ID)
	if err != nil {
		suite, err = a.Store.CreateEvalSuite(
			ctx,
			model.EvalSuite{Name: w.Name + " evals", TargetKind: "workflow", TargetID: w.ID},
		)
		if err != nil {
			return model.EvalCase{}, err
		}
	}
	recorded := a.StepOutputs(ctx, runID)
	raw, _ := json.Marshal(recorded)
	ref := ""
	if len(raw) > 2048 && a.cfg.ArtifactDir != "" {
		dir := filepath.Join(a.cfg.ArtifactDir, "eval")
		_ = os.MkdirAll(dir, 0o700)
		id := ids.New()
		path := filepath.Join(dir, id+".json")
		if err := os.WriteFile(path, raw, 0o600); err == nil {
			sum := sha256Hex(raw)
			art, aerr := a.Store.AddArtifact(
				ctx,
				model.Artifact{
					ID:     id,
					Path:   path,
					SHA256: sum,
					Size:   int64(len(raw)),
					Mime:   "application/json",
				},
			)
			if aerr == nil {
				ref = "artifact:" + art.ID
			}
		}
	}
	if ref == "" {
		ref = string(raw)
	}
	inputs := run.Inputs
	assertions := json.RawMessage(`[]`)
	caseRec, err := a.Store.AddEvalCase(ctx, model.EvalCase{SuiteID: suite.ID, SourceRunID: run.ID,
		Inputs: inputs, Assertions: assertions, RecordedIORef: ref, Split: "train"})
	if err != nil {
		return caseRec, err
	}
	a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "eval.case.created",
		Payload: a.mustPayload(
			map[string]any{"suite_id": suite.ID, "case_id": caseRec.ID, "run_id": runID},
		)})
	return caseRec, nil
}

func (a *App) RunEvalSuite(
	ctx context.Context,
	suiteRef, targetVersionID string,
) (model.EvalRun, error) {
	suite, err := a.Store.GetEvalSuite(ctx, suiteRef)
	if err != nil {
		suite, err = a.Store.EvalSuiteByName(ctx, suiteRef)
	}
	if err != nil {
		return model.EvalRun{}, err
	}
	suite, err = a.Store.GetEvalSuite(ctx, suite.ID)
	if err != nil {
		return model.EvalRun{}, err
	}
	if targetVersionID == "" {
		w, err := a.Store.GetWorkflowByID(ctx, suite.TargetID)
		if err != nil {
			return model.EvalRun{}, err
		}
		if w.CurrentVersionID == "" {
			return model.EvalRun{}, errors.New("target workflow has no current version")
		}
		targetVersionID = w.CurrentVersionID
	}
	v, err := a.Store.GetWorkflowVersion(ctx, targetVersionID)
	if err != nil {
		return model.EvalRun{}, err
	}
	if v.WorkflowID != suite.TargetID {
		return model.EvalRun{}, errors.New("target version does not belong to the suite's workflow")
	}
	spec, err := workflow.Parse([]byte(v.YAML))
	if err != nil {
		return model.EvalRun{}, err
	}
	bindings, err := a.resolveBindings(ctx, spec, 0)
	if err != nil {
		return model.EvalRun{}, err
	}
	w, err := a.Store.GetWorkflowByID(ctx, suite.TargetID)
	if err != nil {
		return model.EvalRun{}, err
	}
	evalRun, err := a.Store.CreateEvalRun(
		ctx,
		model.EvalRun{SuiteID: suite.ID, TargetVer: v.ID, Status: "running"},
	)
	if err != nil {
		return model.EvalRun{}, err
	}
	a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "eval.run.started",
		Payload: a.mustPayload(
			map[string]any{"eval_run_id": evalRun.ID, "suite_id": suite.ID, "target_ver": v.ID},
		)})
	for _, c := range suite.Cases {
		if ctx.Err() != nil {
			break
		}
		inputs := map[string]any{}
		_ = json.Unmarshal(c.Inputs, &inputs)
		replay := map[string]any{}
		if c.RecordedIORef != "" {
			if v, err := a.loadOutputRef(ctx, c.RecordedIORef); err == nil {
				if m, ok := v.(map[string]any); ok {
					replay = m
				}
			}
		}
		start := time.Now()
		run, rerr := a.startWorkflowRun(
			ctx,
			w,
			v,
			spec,
			inputs,
			bindings,
			"replay",
			"",
			"",
			"",
			true,
			"allow",
			replay,
		)
		var runErr string
		if rerr == nil {
			run, rerr = a.waitForRun(ctx, run.ID, 5*time.Minute)
		}
		if rerr != nil {
			runErr = rerr.Error()
		} else if run.Status != model.RunSucceeded {
			runErr = run.Error
		}
		outputs, _ := a.RunOutputs(ctx, run.ID)
		passed := runErr == "" && run.Status == model.RunSucceeded
		var assertions []string
		_ = json.Unmarshal(c.Assertions, &assertions)
		for _, as := range assertions {
			env := &workflow.Env{Inputs: map[string]any{}, Steps: map[string]workflow.StepView{},
				Vars: map[string]any{"output": outputs, "inputs": inputs}}
			ok, aerr := workflow.EvalExpression(as, env)
			if aerr != nil || ok != true {
				passed = false
				if runErr == "" {
					runErr = "assertion failed: " + as
				}
				break
			}
		}
		score := 0.0
		if passed {
			score = 1
		}
		rawOut, _ := json.Marshal(outputs)
		res, err := a.Store.AddEvalResult(ctx, model.EvalResult{
			EvalRunID: evalRun.ID,
			CaseID:    c.ID,
			TargetVer: v.ID,
			Passed:    passed,
			Score:     score,
			Judge:     "assertions",
			CostMicro: run.CostMicro,
			LatencyMS: time.Since(start).Milliseconds(),
			Output:    rawOut,
			Error:     runErr,
		})
		if err != nil {
			return evalRun, err
		}
		a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "eval.case.finished",
			Payload: a.mustPayload(
				map[string]any{"eval_run_id": evalRun.ID, "case_id": c.ID, "passed": passed,
					"score": score, "cost_micro": res.CostMicro, "latency_ms": res.LatencyMS},
			)})
	}
	_ = a.Store.FinishEvalRun(ctx, evalRun.ID, "finished")
	evalRun, _ = a.Store.GetEvalRun(ctx, evalRun.ID)
	a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "eval.run.finished",
		Payload: a.mustPayload(map[string]any{"eval_run_id": evalRun.ID, "suite_id": suite.ID})})
	return evalRun, nil
}

func (a *App) RunOutputs(ctx context.Context, runID string) (map[string]any, error) {
	events, err := a.Store.RunEvents(ctx, runID, 0, 1000)
	if err != nil {
		return nil, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != "run.finished" {
			continue
		}
		var p struct {
			Outputs map[string]any `json:"outputs"`
		}
		if json.Unmarshal(events[i].Payload, &p) == nil {
			return p.Outputs, nil
		}
	}
	return map[string]any{}, nil
}

func (a *App) AnalyzeRun(ctx context.Context, runID string) (model.TuningProposal, error) {
	run, err := a.Store.GetRun(ctx, runID)
	if err != nil {
		return model.TuningProposal{}, err
	}
	if run.TargetKind != "workflow" {
		return model.TuningProposal{}, errors.New("only workflow runs can be analyzed")
	}
	v, err := a.Store.GetWorkflowVersion(ctx, run.TargetVer)
	if err != nil {
		return model.TuningProposal{}, err
	}
	events, _ := a.Store.RunEvents(ctx, runID, 0, 500)
	trimmed := []map[string]any{}
	for _, e := range events {
		if e.Type == "llm.delta" || e.Type == "step.output.delta" {
			continue
		}
		trimmed = append(
			trimmed,
			map[string]any{
				"type":    e.Type,
				"step":    e.StepID,
				"payload": previewJSON(e.Payload, 1500),
			},
		)
	}
	trace, _ := json.Marshal(trimmed)
	prompt := fmt.Sprintf(
		`A workflow run failed or was rated poorly. Diagnose the root cause and propose the smallest backward-compatible patch.

Workflow YAML:
%s

Run:
- status: %s
- error: %s
- source: %s
- inputs: %s
- steps: %s

Events:
%s

Return JSON:
{"diagnosis":"...", "patch_summary":"...", "expected_effect":"...", "proposed_yaml":"<full corrected workflow YAML>"}
The diagnosis must classify the cause: bad input, wrong tool choice, weak prompt, tool bug, model limit, or flaky external service. Do not change steps that passed.`,
		v.YAML,
		run.Status,
		run.Error,
		run.Source,
		string(run.Inputs),
		mustJSON(run.Steps),
		string(trace),
	)
	res, err := a.ChatWithTools(ctx, workflow.LLMChatRequest{
		SessionID: runID,
		Role:      "planner",
		System:    "You are the tuner inside MyAgent. Diagnose from evidence, not guesses. Propose the smallest backward-compatible patch for one root cause. Do not touch steps that passed.",
		Messages:  []llm.Message{{Role: "user", Content: prompt}},
		OutputSchema: map[string]any{
			"type":     "object",
			"required": []any{"diagnosis", "patch_summary", "expected_effect", "proposed_yaml"},
			"properties": map[string]any{
				"diagnosis":     map[string]any{"type": "string"},
				"patch_summary": map[string]any{"type": "string"},
				"expected_effect": map[string]any{
					"type": "string",
				},
				"proposed_yaml": map[string]any{"type": "string"},
			},
		},
	})
	if err != nil {
		return model.TuningProposal{}, err
	}
	data, _ := res.Data.(map[string]any)
	if data == nil {
		return model.TuningProposal{}, errors.New(
			"the model did not return a structured diagnosis; use a stronger planner model",
		)
	}
	proposed, _ := data["proposed_yaml"].(string)
	if proposed == "" {
		return model.TuningProposal{}, errors.New("the model returned no proposed YAML")
	}
	return a.createProposal(
		ctx,
		run.TargetID,
		v.ID,
		proposed,
		fmt.Sprint(
			data["diagnosis"],
		)+"\n\nExpected effect: "+fmt.Sprint(
			data["expected_effect"],
		),
		"agent",
	)
}

func validationError(res workflow.ValidationResult) error {
	if len(res.Errors) == 0 {
		return errors.New("validation failed")
	}
	first := res.Errors[0]
	if first.Path != "" {
		return fmt.Errorf("%s at %s: %s", first.Code, first.Path, first.Message)
	}
	return fmt.Errorf("%s: %s", first.Code, first.Message)
}

func unifiedDiff(base, proposed string) string {
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(base),
		B:        difflib.SplitLines(proposed),
		FromFile: "current",
		ToFile:   "proposed",
		Context:  3,
	})
	if err != nil {
		return ""
	}
	return diff
}

func (a *App) fillProposalPatch(ctx context.Context, p *model.TuningProposal) {
	if p.BaseVersionID == "" || p.ProposedYAML == "" {
		return
	}
	base, err := a.Store.GetWorkflowVersion(ctx, p.BaseVersionID)
	if err != nil {
		return
	}
	p.Patch = unifiedDiff(base.YAML, p.ProposedYAML)
}

// TuningProposals returns proposals with a readable unified diff. Older
// proposals stored diff-match-patch text (percent-encoded), so regenerate the
// patch from the base and proposed YAML when possible.
func (a *App) TuningProposals(ctx context.Context) ([]model.TuningProposal, error) {
	proposals, err := a.Store.ListTuningProposals(ctx)
	if err != nil {
		return nil, err
	}
	for i := range proposals {
		a.fillProposalPatch(ctx, &proposals[i])
	}
	return proposals, nil
}

func (a *App) createProposal(
	ctx context.Context,
	workflowID, baseVersionID, proposedYAML, diagnosis, createdBy string,
) (model.TuningProposal, error) {
	w, err := a.Store.GetWorkflowByID(ctx, workflowID)
	if err != nil {
		return model.TuningProposal{}, err
	}
	base, err := a.Store.GetWorkflowVersion(ctx, baseVersionID)
	if err != nil {
		return model.TuningProposal{}, err
	}
	spec, res, perr := a.ValidateWorkflowYAML(ctx, proposedYAML, false)
	if perr != nil {
		return model.TuningProposal{}, perr
	}
	if !res.OK() {
		return model.TuningProposal{}, fmt.Errorf(
			"proposed workflow is invalid: %s %s",
			res.Errors[0].Code,
			res.Errors[0].Message,
		)
	}
	_ = spec
	diff := unifiedDiff(base.YAML, proposedYAML)
	p, err := a.Store.CreateTuningProposal(
		ctx,
		model.TuningProposal{
			TargetKind:    "workflow",
			TargetID:      w.ID,
			BaseVersionID: base.ID,
			Diagnosis:     diagnosis,
			Patch:         diff,
			ProposedYAML:  proposedYAML,
			Status:        "proposed",
		},
	)
	if err != nil {
		return p, err
	}
	a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "tuning.proposal.created",
		Payload: a.mustPayload(
			map[string]any{
				"proposal_id":  p.ID,
				"workflow":     w.Name,
				"base_version": base.Version,
				"diagnosis":    diagnosis,
			},
		)})
	return p, nil
}

func (a *App) DecideProposal(
	ctx context.Context,
	proposalID, action, comment string,
) (model.TuningProposal, error) {
	p, err := a.Store.GetTuningProposal(ctx, proposalID)
	if err != nil {
		return p, err
	}
	switch action {
	case "accept":
		if p.Status != "proposed" && p.Status != "testing" {
			return p, errors.New("proposal is no longer pending")
		}
		if p.ProposedYAML == "" {
			return p, errors.New("proposal has no proposed YAML")
		}
		base, err := a.Store.GetWorkflowVersion(ctx, p.BaseVersionID)
		if err != nil {
			return p, fmt.Errorf("load proposal base version: %w", err)
		}
		w, err := a.Store.GetWorkflowByID(ctx, p.TargetID)
		if err != nil {
			return p, err
		}
		v, err := a.SaveWorkflowDraft(
			ctx,
			p.ProposedYAML,
			"Accepted tuning proposal "+p.ID+". "+comment,
			"tuner",
			base.Version,
		)
		if err != nil {
			return p, fmt.Errorf("save tuning patch: %w", err)
		}
		if !v.Validation.OK() {
			return p, fmt.Errorf("proposed workflow is no longer valid: %w", validationError(v.Validation))
		}
		if v.Workflow.ID != w.ID {
			return p, errors.New("proposed workflow changed the workflow name")
		}
		if err := a.ApproveWorkflowVersion(ctx, w.ID, v.Version.ID); err != nil {
			return p, err
		}
		_ = a.Store.SetTuningStatus(ctx, p.ID, "accepted", p.EvalAfter)
	case "reject":
		_ = a.Store.SetTuningStatus(ctx, p.ID, "rejected", p.EvalAfter)
	case "rollback":
		base, err := a.Store.GetWorkflowVersion(ctx, p.BaseVersionID)
		if err != nil {
			return p, err
		}
		if err := a.Store.SetWorkflowCurrentVersion(ctx, base.WorkflowID, base.ID); err != nil {
			return p, err
		}
		_ = a.Store.SetTuningStatus(ctx, p.ID, "rolled_back", p.EvalAfter)
	default:
		return p, errors.New("action must be accept, reject or rollback")
	}
	a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "tuning.proposal.decided",
		Payload: a.mustPayload(map[string]any{"proposal_id": p.ID, "decision": action})})
	return a.Store.GetTuningProposal(ctx, p.ID)
}

func (a *App) metaProposePatch(ctx context.Context, args map[string]any) (any, error) {
	w, err := a.Store.GetWorkflowByName(ctx, str(args, "target"))
	if err != nil {
		return nil, err
	}
	ver := intg(args, "base_version", 0)
	base, err := a.Store.GetWorkflowVersionByNumber(ctx, w.ID, ver)
	if err != nil {
		return nil, err
	}
	p, err := a.createProposal(
		ctx,
		w.ID,
		base.ID,
		str(args, "content"),
		str(args, "rationale"),
		"tuner",
	)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":          true,
		"proposal_id": p.ID,
		"next_hint":   "Ask the user to review the diff and run the eval suite before accepting.",
	}, nil
}

func (a *App) metaRunEval(ctx context.Context, suite, targetVersion string) (any, error) {
	r, err := a.RunEvalSuite(ctx, suite, targetVersion)
	if err != nil {
		return nil, err
	}
	passed := 0
	for _, res := range r.Results {
		if res.Passed {
			passed++
		}
	}
	return map[string]any{
		"ok":          true,
		"eval_run_id": r.ID,
		"passed":      passed,
		"total":       len(r.Results),
		"results":     r.Results,
		"next_hint":   "Compare pass rate, cost and latency with the baseline before accepting a patch.",
	}, nil
}

func (a *App) EvalSuiteList(ctx context.Context) ([]model.EvalSuite, error) {
	suites, err := a.Store.ListEvalSuites(ctx)
	if err != nil {
		return nil, err
	}
	for i := range suites {
		suites[i], _ = a.Store.GetEvalSuite(ctx, suites[i].ID)
	}
	return suites, nil
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
