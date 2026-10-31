package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"myagent/internal/model"
	"myagent/internal/schema"
	"myagent/internal/workflow"
)

var ErrConcurrency = errors.New("workflow is already running and its concurrency policy is skip")

type SaveWorkflowResult struct {
	Workflow   model.Workflow            `json:"workflow"`
	Version    model.WorkflowVersion     `json:"version"`
	Validation workflow.ValidationResult `json:"validation"`
}

type WorkflowDetail struct {
	Workflow    model.WorkflowWithVersion `json:"workflow"`
	Versions    []model.WorkflowVersion   `json:"versions"`
	Triggers    []model.Trigger           `json:"triggers"`
	Runs        []model.Run               `json:"runs"`
	Validation  workflow.ValidationResult `json:"validation"`
	Spec        *workflow.Spec            `json:"spec,omitempty"`
	InputSchema map[string]any            `json:"input_schema,omitempty"`
}

func (a *App) ValidateWorkflowYAML(
	ctx context.Context,
	yamlText string,
	forRun bool,
) (*workflow.Spec, workflow.ValidationResult, error) {
	spec, err := workflow.Parse([]byte(yamlText))
	if err != nil {
		res := workflow.ValidationResult{
			Errors: []workflow.Diagnostic{{Code: "E001", Path: "", Message: err.Error(),
				Hint: "Fix the YAML syntax and field names; unknown fields are rejected."}},
		}
		return nil, res, err
	}
	res := workflow.Validate(spec, a.validateOptions(ctx, forRun))
	workflow.SortDiagnostics(res.Errors)
	workflow.SortDiagnostics(res.Warnings)
	return spec, res, nil
}

func (a *App) SaveWorkflowDraft(
	ctx context.Context,
	yamlText, changeNote, createdBy string,
	baseVersion int,
) (SaveWorkflowResult, error) {
	if createdBy == "" {
		createdBy = "user"
	}
	spec, res, parseErr := a.ValidateWorkflowYAML(ctx, yamlText, false)
	if parseErr != nil || !res.OK() {
		if spec != nil {
			a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "workflow.validation.failed",
				Payload: a.mustPayload(map[string]any{"name": spec.Name, "errors": res.Errors})})
		}
		return SaveWorkflowResult{Validation: res}, parseErr
	}
	w, err := a.Store.GetOrCreateWorkflow(ctx, a.Workspace.ID, spec.Name)
	if err != nil {
		return SaveWorkflowResult{Validation: res}, err
	}
	if w.CurrentVersionID != "" {
		cur, err := a.Store.GetWorkflowVersion(ctx, w.CurrentVersionID)
		if err == nil && baseVersion == 0 {
			res.Errors = append(res.Errors, workflow.Diagnostic{Code: "E011", Path: "",
				Message: fmt.Sprintf(
					"base_version is required when editing an existing workflow (current v%d)",
					cur.Version,
				),
				Hint: "Pass the version you started from so concurrent edits are not overwritten."})
			return SaveWorkflowResult{
				Workflow:   w,
				Validation: res,
			}, errors.New(
				"base_version is required",
			)
		}
		if err == nil && cur.Version != baseVersion {
			res.Errors = append(res.Errors, workflow.Diagnostic{Code: "E011", Path: "",
				Message: fmt.Sprintf(
					"base version %d is stale; current version is %d",
					baseVersion,
					cur.Version,
				),
				Hint: "Reload the workflow and reapply the change."})
			return SaveWorkflowResult{
				Workflow:   w,
				Validation: res,
			}, errors.New(
				"stale base version",
			)
		}
	}
	inputSchema, _ := spec.InputSchema()
	schemaJSON, _ := json.Marshal(inputSchema)
	v, err := a.Store.AddWorkflowVersion(ctx, model.WorkflowVersion{
		WorkflowID: w.ID, YAML: yamlText, InputSchema: schemaJSON, ContentHash: spec.Hash(),
		Status: "draft", ChangeNote: changeNote, CreatedBy: createdBy,
	})
	if err != nil {
		return SaveWorkflowResult{Validation: res}, err
	}
	a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "workflow.version.created",
		Payload: a.mustPayload(
			map[string]any{"workflow_id": w.ID, "version_id": v.ID, "version": v.Version,
				"name": spec.Name, "created_by": createdBy, "warnings": res.Warnings},
		)})
	return SaveWorkflowResult{Workflow: w, Version: v, Validation: res}, nil
}

func (a *App) ApproveWorkflowVersion(ctx context.Context, workflowID, versionID string) error {
	w, err := a.Store.GetWorkflowByID(ctx, workflowID)
	if err != nil {
		return err
	}
	v, err := a.Store.GetWorkflowVersion(ctx, versionID)
	if err != nil {
		return err
	}
	if v.WorkflowID != w.ID {
		return errors.New("version does not belong to this workflow")
	}
	_, res, err := a.ValidateWorkflowYAML(ctx, v.YAML, true)
	if err != nil || !res.OK() {
		return fmt.Errorf("workflow has validation errors and cannot be approved")
	}
	if err := a.Store.ApproveWorkflowVersion(ctx, w.ID, v.ID); err != nil {
		return err
	}
	a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "workflow.version.approved",
		Payload: a.mustPayload(
			map[string]any{"workflow_id": w.ID, "version_id": v.ID, "version": v.Version},
		)})
	return nil
}

func (a *App) WorkflowDetail(ctx context.Context, id string) (WorkflowDetail, error) {
	w, err := a.Store.GetWorkflowByID(ctx, id)
	if err != nil {
		return WorkflowDetail{}, err
	}
	versions, err := a.Store.WorkflowVersions(ctx, id)
	if err != nil {
		return WorkflowDetail{}, err
	}
	ww := model.WorkflowWithVersion{Workflow: w, Versions: versions}
	if w.CurrentVersionID != "" {
		for i := range versions {
			if versions[i].ID == w.CurrentVersionID {
				v := versions[i]
				ww.Version = &v
			}
		}
	}
	if ww.Version == nil && len(versions) > 0 {
		v := versions[0]
		ww.Version = &v
	}
	detail := WorkflowDetail{Workflow: ww, Versions: versions}
	if ww.Version != nil {
		if spec, verr := workflow.Parse([]byte(ww.Version.YAML)); verr == nil {
			detail.Spec = spec
			ww.Steps = stepViews(spec)
			detail.Workflow = ww
			is, _ := spec.InputSchema()
			detail.InputSchema = is
			detail.Validation = workflow.Validate(spec, a.validateOptions(ctx, true))
		}
	}
	detail.Triggers, _ = a.Store.ListTriggers(ctx, id)
	detail.Runs, _ = a.Store.ListRuns(ctx, model.RunFilter{TargetID: id, Limit: 30})
	return detail, nil
}

func (a *App) ResolveWorkflowVersion(
	ctx context.Context,
	workflowID, versionID, source string,
) (model.Workflow, model.WorkflowVersion, *workflow.Spec, error) {
	w, err := a.Store.GetWorkflowByID(ctx, workflowID)
	if err != nil {
		w, err = a.Store.GetWorkflowByName(ctx, workflowID)
		if err != nil {
			return w, model.WorkflowVersion{}, nil, err
		}
	}
	var v model.WorkflowVersion
	if versionID != "" {
		v, err = a.Store.GetWorkflowVersion(ctx, versionID)
		if err != nil {
			return w, v, nil, err
		}
		if v.WorkflowID != w.ID {
			return w, v, nil, errors.New("version does not belong to this workflow")
		}
	} else {
		if w.CurrentVersionID != "" {
			v, err = a.Store.GetWorkflowVersion(ctx, w.CurrentVersionID)
			if err != nil {
				return w, v, nil, err
			}
		}
		if v.ID == "" ||
			(v.Status != "approved" && source != "manual" && source != "chat" && source != "replay") {
			var approvedID string
			err = a.Store.DB().
				QueryRowContext(ctx, `SELECT id FROM workflow_versions WHERE workflow_id=? AND status='approved' ORDER BY version DESC LIMIT 1`, w.ID).
				Scan(&approvedID)
			if err != nil {
				if v.ID == "" {
					return w, v, nil, errors.New("workflow has no approved version")
				}
			} else {
				v, err = a.Store.GetWorkflowVersion(ctx, approvedID)
				if err != nil {
					return w, v, nil, err
				}
			}
		}
	}
	spec, err := workflow.Parse([]byte(v.YAML))
	if err != nil {
		return w, v, nil, err
	}
	return w, v, spec, nil
}

func (a *App) validateRunInputs(
	v model.WorkflowVersion,
	inputs map[string]any,
) (map[string]any, []schema.Issue, error) {
	s, err := schema.Compile(v.InputSchema)
	if err != nil {
		return inputs, nil, err
	}
	normalized := s.ApplyDefaults(inputs)
	m, ok := normalized.(map[string]any)
	if !ok {
		return inputs, []schema.Issue{{Path: "", Message: "inputs must be an object"}}, nil
	}
	return m, s.Validate(normalized), nil
}

func (a *App) RunWorkflow(
	ctx context.Context,
	workflowID, versionID string,
	inputs map[string]any,
	source, conversationID string,
	dryRun bool,
	unattended string,
	parentRunID string,
) (model.Run, error) {
	w, v, spec, err := a.ResolveWorkflowVersion(ctx, workflowID, versionID, source)
	if err != nil {
		return model.Run{}, err
	}
	res := workflow.Validate(spec, a.validateOptions(ctx, true))
	if !res.OK() {
		return model.Run{}, fmt.Errorf(
			"workflow validation failed: %s %s",
			res.Errors[0].Code,
			res.Errors[0].Message,
		)
	}
	clean, iss, err := a.validateRunInputs(v, inputs)
	if err != nil {
		return model.Run{}, err
	}
	if len(iss) > 0 {
		msgs := []string{}
		for _, i := range iss {
			msgs = append(msgs, i.Path+": "+i.Message)
		}
		return model.Run{}, errors.New("invalid inputs: " + strings.Join(msgs, "; "))
	}
	policy := "queue"
	if spec.Settings.Concurrency != nil && spec.Settings.Concurrency.Policy != "" {
		policy = spec.Settings.Concurrency.Policy
	}
	active, _ := a.Store.CountActiveRunsForTarget(ctx, w.ID)
	if active > 0 {
		switch policy {
		case "skip":
			return model.Run{}, ErrConcurrency
		case "cancel-previous":
			runs, _ := a.Store.ActiveRunsForTarget(ctx, w.ID)
			for _, r := range runs {
				if c := a.runControl(r.ID); c != nil {
					c.Cancel()
				}
			}
		}
	}
	if unattended == "" {
		unattended = spec.Settings.Unattended
	}
	if unattended == "" {
		unattended = "ask"
	}
	bindings, err := a.resolveBindings(ctx, spec, 0)
	if err != nil {
		return model.Run{}, err
	}
	return a.startWorkflowRun(
		ctx,
		w,
		v,
		spec,
		clean,
		bindings,
		source,
		conversationID,
		"",
		parentRunID,
		dryRun,
		unattended,
		nil,
	)
}

func (a *App) startWorkflowRun(
	ctx context.Context,
	w model.Workflow,
	v model.WorkflowVersion,
	spec *workflow.Spec,
	inputs map[string]any,
	bindings map[string]workflow.ToolBinding,
	source, conversationID, triggerID, parentRunID string,
	dryRun bool,
	unattended string,
	replay map[string]any,
) (model.Run, error) {
	rawInputs, _ := json.Marshal(inputs)
	run, err := a.Store.CreateRun(ctx, model.Run{
		TargetKind: "workflow", TargetID: w.ID, TargetVer: v.ID, Source: source,
		ConversationID: conversationID, TriggerID: triggerID, ParentRunID: parentRunID,
		Inputs: rawInputs, Status: model.RunQueued,
	})
	if err != nil {
		return run, err
	}
	a.Bus.EmitNoErr(model.Event{Stream: model.RunStream(run.ID), Type: "run.queued", RunID: run.ID,
		Payload: a.mustPayload(
			map[string]any{
				"target":     "workflow",
				"name":       w.Name,
				"source":     source,
				"inputs":     inputs,
				"trigger_id": triggerID,
			},
		)})
	runCtx, cancel := context.WithCancel(context.Background())
	control := a.registerRun(run.ID, cancel)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				_ = a.Store.FinishRun(
					context.Background(),
					run.ID,
					model.RunFailed,
					fmt.Sprintf("internal error: %v", r),
				)
				a.Bus.EmitNoErr(
					model.Event{
						Stream: model.RunStream(run.ID),
						Type:   "run.finished",
						RunID:  run.ID,
						Payload: a.mustPayload(
							map[string]any{
								"status": model.RunFailed,
								"error":  fmt.Sprintf("internal error: %v", r),
							},
						),
					},
				)
			}
			a.unregisterRun(run.ID)
			cancel()
		}()
		select {
		case a.sem <- struct{}{}:
			defer func() { <-a.sem }()
		case <-runCtx.Done():
			_ = a.Store.FinishRun(
				context.Background(),
				run.ID,
				model.RunCancelled,
				"cancelled before start",
			)
			a.Bus.EmitNoErr(
				model.Event{Stream: model.RunStream(run.ID), Type: "run.finished", RunID: run.ID,
					Payload: a.mustPayload(
						map[string]any{
							"status": model.RunCancelled,
							"error":  "cancelled before start",
						},
					)},
			)
			return
		case <-a.closeCh:
			_ = a.Store.FinishRun(
				context.Background(),
				run.ID,
				model.RunCancelled,
				"app shutting down",
			)
			return
		}
		a.Engine.Run(runCtx, spec, workflow.RunRequest{
			RunID: run.ID, Source: source, ConversationID: conversationID, Inputs: inputs,
			Unattended: unattended, DryRun: dryRun, Bindings: bindings, Replay: replay,
		})
		_ = control
	}()
	return run, nil
}

func (a *App) resolveBindings(
	ctx context.Context,
	spec *workflow.Spec,
	depth int,
) (map[string]workflow.ToolBinding, error) {
	if depth > 4 {
		return nil, errors.New("workflow nesting is too deep")
	}
	out := map[string]workflow.ToolBinding{}
	for _, st := range spec.FlattenSteps() {
		refs := []string{}
		if st.Step.Tool != "" {
			refs = append(refs, st.Step.Tool)
		}
		if st.Step.Agent != nil {
			refs = append(refs, st.Step.Agent.Tools...)
		}
		for _, ref := range refs {
			if _, ok := out[ref]; ok {
				continue
			}
			b, err := a.Resolve(ctx, ref)
			if err != nil {
				return nil, fmt.Errorf("tool %s: %w", ref, err)
			}
			out[ref] = b
		}
		if st.Step.Workflow != "" {
			child, _, err := a.LoadWorkflow(ctx, st.Step.Workflow)
			if err != nil {
				return nil, err
			}
			childBindings, err := a.resolveBindings(ctx, child, depth+1)
			if err != nil {
				return nil, err
			}
			for k, v := range childBindings {
				out[k] = v
			}
		}
	}
	return out, nil
}

func (a *App) PauseRun(ctx context.Context, runID string) error {
	r, err := a.Store.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if model.RunTerminal(r.Status) {
		return errors.New("run has already finished")
	}
	c := a.runControl(runID)
	if c == nil {
		return errors.New("run is not active in this app session")
	}
	c.Pause()
	_ = a.Store.SetRunStatus(ctx, runID, model.RunPaused, "")
	a.Bus.EmitNoErr(model.Event{Stream: model.RunStream(runID), Type: "run.paused", RunID: runID,
		Payload: a.mustPayload(map[string]any{})})
	return nil
}

func (a *App) ResumeRun(ctx context.Context, runID string) error {
	r, err := a.Store.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if model.RunTerminal(r.Status) {
		return errors.New("run has already finished")
	}
	if c := a.runControl(runID); c != nil {
		c.Resume()
		_ = a.Store.SetRunStatus(ctx, runID, model.RunRunning, "")
		a.Bus.EmitNoErr(
			model.Event{Stream: model.RunStream(runID), Type: "run.resumed", RunID: runID,
				Payload: a.mustPayload(map[string]any{})},
		)
		return nil
	}
	// Resume after restart: reload the pinned version and continue from the
	// last successful step.
	if r.TargetKind != "workflow" {
		return errors.New("only workflow runs can be resumed")
	}
	w, err := a.Store.GetWorkflowByID(ctx, r.TargetID)
	if err != nil {
		return err
	}
	v, err := a.Store.GetWorkflowVersion(ctx, r.TargetVer)
	if err != nil {
		return err
	}
	spec, err := workflow.Parse([]byte(v.YAML))
	if err != nil {
		return err
	}
	inputs := map[string]any{}
	_ = json.Unmarshal(r.Inputs, &inputs)
	bindings, err := a.resolveBindings(ctx, spec, 0)
	if err != nil {
		return err
	}
	_, _ = a.Store.ExpireApprovalsForRun(ctx, runID, "superseded by resume")
	runCtx, cancel := context.WithCancel(context.Background())
	a.registerRun(runID, cancel)
	go func() {
		defer func() {
			a.unregisterRun(runID)
			cancel()
		}()
		a.sem <- struct{}{}
		defer func() { <-a.sem }()
		a.Engine.Run(runCtx, spec, workflow.RunRequest{
			RunID: runID, Source: r.Source, ConversationID: r.ConversationID, Inputs: inputs,
			Unattended: spec.Settings.Unattended, Resume: true, Bindings: bindings,
		})
		_ = w
	}()
	a.Bus.EmitNoErr(model.Event{Stream: model.RunStream(runID), Type: "run.resumed", RunID: runID,
		Payload: a.mustPayload(map[string]any{"resume": true})})
	return nil
}

func (a *App) CancelRun(ctx context.Context, runID string) error {
	r, err := a.Store.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if model.RunTerminal(r.Status) {
		return nil
	}
	if c := a.runControl(runID); c != nil {
		c.Cancel()
		return nil
	}
	_ = a.Store.FinishRun(ctx, runID, model.RunCancelled, "cancelled")
	a.Bus.EmitNoErr(model.Event{Stream: model.RunStream(runID), Type: "run.finished", RunID: runID,
		Payload: a.mustPayload(map[string]any{"status": model.RunCancelled, "error": "cancelled"})})
	return nil
}

func (a *App) Rerun(ctx context.Context, runID string) (model.Run, error) {
	r, err := a.Store.GetRun(ctx, runID)
	if err != nil {
		return model.Run{}, err
	}
	if r.TargetKind != "workflow" {
		return model.Run{}, errors.New("only workflow runs can be rerun")
	}
	w, v, spec, err := a.ResolveWorkflowVersion(ctx, r.TargetID, r.TargetVer, "manual")
	if err != nil {
		return model.Run{}, err
	}
	inputs := map[string]any{}
	_ = json.Unmarshal(r.Inputs, &inputs)
	clean, iss, err := a.validateRunInputs(v, inputs)
	if err != nil {
		return model.Run{}, err
	}
	if len(iss) > 0 {
		return model.Run{}, errors.New("stored inputs are no longer valid for this version")
	}
	bindings, err := a.resolveBindings(ctx, spec, 0)
	if err != nil {
		return model.Run{}, err
	}
	return a.startWorkflowRun(
		ctx,
		w,
		v,
		spec,
		clean,
		bindings,
		"manual",
		"",
		"",
		r.ID,
		false,
		spec.Settings.Unattended,
		nil,
	)
}

func (a *App) recoverInterrupted(ctx context.Context) {
	// In-memory approval waiters do not survive a restart. Expire the old cards
	// so a resumed run creates a fresh, actionable approval.
	if pending, err := a.Store.PendingApprovals(ctx); err == nil {
		for _, ap := range pending {
			_ = a.setApprovalStatus(
				ctx,
				ap.ID,
				"expired",
				"interrupted",
				"approval interrupted by app restart",
			)
		}
	}
	runs, err := a.Store.InterruptedRuns(ctx)
	if err != nil {
		return
	}
	for _, r := range runs {
		_ = a.Store.SetRunStatus(
			ctx,
			r.ID,
			model.RunPaused,
			"interrupted by app restart; press Resume to continue",
		)
	}
}

func (a *App) StepOutputs(ctx context.Context, runID string) map[string]any {
	steps, _ := a.Store.ListStepRuns(ctx, runID)
	out := map[string]any{}
	for _, sr := range steps {
		if sr.Status != "succeeded" || sr.OutputRef == "" {
			continue
		}
		if v, err := a.loadOutputRef(ctx, sr.OutputRef); err == nil {
			out[sr.StepID] = v
		}
	}
	return out
}

func (a *App) loadOutputRef(ctx context.Context, ref string) (any, error) {
	if strings.HasPrefix(ref, "artifact:") {
		art, err := a.Store.GetArtifact(ctx, strings.TrimPrefix(ref, "artifact:"))
		if err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(art.Path)
		if err != nil {
			return nil, err
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		return v, nil
	}
	var v any
	if err := json.Unmarshal([]byte(ref), &v); err != nil {
		return nil, err
	}
	return v, nil
}

func (a *App) mustPayload(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return a.Secrets.RedactJSON(raw)
}
