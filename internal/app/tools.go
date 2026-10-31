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
	"sort"
	"strings"
	"time"

	"myagent/internal/ids"
	"myagent/internal/model"
	"myagent/internal/schema"
	"myagent/internal/tools"
	"myagent/internal/workflow"
)

type SaveToolResult struct {
	Tool    model.Tool        `json:"tool"`
	Version model.ToolVersion `json:"version"`
	Issues  []tools.Issue     `json:"issues"`
}

func (a *App) CreateToolDraft(
	ctx context.Context,
	manifestYAML, createdBy string,
	baseVersion int,
) (SaveToolResult, error) {
	if createdBy == "" {
		createdBy = "user"
	}
	m, err := tools.ParseManifest(manifestYAML)
	if err != nil {
		return SaveToolResult{Issues: []tools.Issue{{Path: "", Message: err.Error()}}}, err
	}
	issues := tools.ValidateManifest(m)
	if len(issues) > 0 {
		return SaveToolResult{Issues: issues}, errors.New("tool manifest has validation errors")
	}
	t, err := a.Store.GetOrCreateTool(ctx, a.Workspace.ID, m.Name, m.Kind)
	if err != nil {
		return SaveToolResult{}, err
	}
	if t.CurrentVersionID != "" && baseVersion == 0 && createdBy == "agent" {
		return SaveToolResult{
				Issues: []tools.Issue{
					{
						Path:    "",
						Message: "base_version is required when the agent edits an existing tool",
					},
				},
			},
			errors.New(
				"base_version is required",
			)
	}
	if baseVersion > 0 {
		if t.CurrentVersionID != "" {
			cur, err := a.Store.GetToolVersion(ctx, t.CurrentVersionID)
			if err == nil && cur.Version != baseVersion {
				return SaveToolResult{
						Issues: []tools.Issue{
							{
								Path: "",
								Message: fmt.Sprintf(
									"base version %d is stale; current is %d",
									baseVersion,
									cur.Version,
								),
							},
						},
					},
					errors.New(
						"stale base version",
					)
			}
		}
	}
	if t.DraftVersionID != "" {
		draft, err := a.Store.GetToolVersion(ctx, t.DraftVersionID)
		if err == nil && (draft.Status == model.ToolDraft || draft.Status == model.ToolTested) {
			if draft.CodePath != "" {
				if err := os.WriteFile(
					filepath.Join(draft.CodePath, "manifest.yaml"),
					[]byte(manifestYAML),
					0o644,
				); err != nil {
					return SaveToolResult{}, err
				}
			}
			hash := hashToolDir(manifestYAML, draft.CodePath)
			v, err := a.Store.UpdateToolDraft(ctx, draft.ID, manifestYAML, hash)
			if err != nil {
				return SaveToolResult{}, err
			}
			a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "tool.version.updated",
				Payload: a.mustPayload(
					map[string]any{"tool_id": t.ID, "version_id": v.ID, "version": v.Version,
						"name": t.Name, "created_by": createdBy},
				)})
			return SaveToolResult{Tool: t, Version: v}, nil
		}
	}
	dir := filepath.Join(a.cfg.ToolsDir, t.ID, ids.New())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return SaveToolResult{}, err
	}
	// Carry existing files forward so a manifest-only edit does not destroy the
	// implementation. All changed files are explicit drafts.
	if t.CurrentVersionID != "" {
		prev, err := a.Store.GetToolVersion(ctx, t.CurrentVersionID)
		if err == nil && prev.CodePath != "" {
			copyTree(prev.CodePath, dir, 512*1024)
		}
	}
	if err := os.WriteFile(
		filepath.Join(dir, "manifest.yaml"),
		[]byte(manifestYAML),
		0o644,
	); err != nil {
		return SaveToolResult{}, err
	}
	hash := hashToolDir(manifestYAML, dir)
	v, err := a.Store.AddToolVersion(ctx, model.ToolVersion{
		ToolID: t.ID, Manifest: manifestYAML, CodePath: dir, ContentHash: hash,
		Status: model.ToolDraft, CreatedBy: createdBy,
	})
	if err != nil {
		return SaveToolResult{}, err
	}
	t.DraftVersionID = v.ID
	a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "tool.version.created",
		Payload: a.mustPayload(
			map[string]any{"tool_id": t.ID, "version_id": v.ID, "version": v.Version,
				"name": t.Name, "created_by": createdBy},
		)})
	return SaveToolResult{Tool: t, Version: v}, nil
}

func (a *App) WriteToolFile(
	ctx context.Context,
	toolID, versionID, relPath, content string,
) (model.ToolVersion, error) {
	tv, err := a.Store.GetToolVersion(ctx, versionID)
	if err != nil {
		return tv, err
	}
	if tv.ToolID != toolID {
		return tv, errors.New("version does not belong to this tool")
	}
	if tv.Status != model.ToolDraft && tv.Status != model.ToolTested {
		return tv, errors.New("only draft or tested tool versions can be edited")
	}
	if len(content) > 256*1024 {
		return tv, errors.New("tool file is larger than 256KB")
	}
	clean := filepath.Clean(relPath)
	if clean == "." || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return tv, errors.New("invalid tool file path")
	}
	full := filepath.Join(tv.CodePath, clean)
	if !strings.HasPrefix(full, filepath.Clean(tv.CodePath)+string(os.PathSeparator)) {
		return tv, errors.New("path is outside the tool directory")
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return tv, err
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return tv, err
	}
	manifestYAML := tv.Manifest
	if clean == "manifest.yaml" {
		m, err := tools.ParseManifest(content)
		if err != nil {
			return tv, err
		}
		if issues := tools.ValidateManifest(m); len(issues) > 0 {
			return tv, fmt.Errorf("manifest invalid: %s %s", issues[0].Path, issues[0].Message)
		}
		manifestYAML = content
		tv.Manifest = content
	}
	hash := hashToolDir(manifestYAML, tv.CodePath)
	if clean == "manifest.yaml" {
		err = a.Store.UpdateToolVersionManifest(ctx, tv.ID, manifestYAML, hash)
	} else {
		err = a.Store.UpdateToolVersionHash(ctx, tv.ID, hash)
	}
	if err != nil {
		return tv, err
	}
	// Any content change invalidates a previous test result.
	if tv.Status == model.ToolTested {
		_ = a.Store.UpdateToolVersionStatus(ctx, tv.ID, model.ToolDraft, 0)
		tv.Status = model.ToolDraft
	}
	tv.ContentHash = hash
	return tv, nil
}

func (a *App) ToolFiles(ctx context.Context, versionID string) ([]ToolFile, error) {
	tv, err := a.Store.GetToolVersion(ctx, versionID)
	if err != nil {
		return nil, err
	}
	return listCodeFiles(tv.CodePath), nil
}

func (a *App) ToolTest(ctx context.Context, toolID, versionID string) ([]tools.TestResult, error) {
	tv, err := a.Store.GetToolVersion(ctx, versionID)
	if err != nil {
		return nil, err
	}
	m, err := tools.ManifestFromModel(tv.Manifest)
	if err != nil {
		return nil, err
	}
	results := a.Executor.RunTests(ctx, m, tv.CodePath, nil)
	allOK := len(results) > 0
	for _, r := range results {
		if !r.Passed {
			allOK = false
		}
	}
	next := model.ToolDraft
	if allOK {
		next = model.ToolTested
	}
	if tv.Status == model.ToolDraft || tv.Status == model.ToolTested {
		_ = a.Store.UpdateToolVersionStatus(ctx, tv.ID, next, 0)
	}
	a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "tool.test.finished",
		Payload: a.mustPayload(
			map[string]any{
				"tool_id":    toolID,
				"version_id": versionID,
				"passed":     allOK,
				"tests":      results,
			},
		)})
	return results, nil
}

func (a *App) ApproveToolVersion(ctx context.Context, toolID, versionID string) error {
	tv, err := a.Store.GetToolVersion(ctx, versionID)
	if err != nil {
		return err
	}
	if tv.ToolID != toolID {
		return errors.New("version does not belong to this tool")
	}
	if _, err := tools.ManifestFromModel(tv.Manifest); err != nil {
		return err
	}
	if tv.Status == model.ToolDraft {
		return errors.New("tool version is still a draft; run the tests before approval")
	}
	if err := a.Store.UpdateToolVersionStatus(
		ctx,
		tv.ID,
		model.ToolActive,
		ids.NowMS(),
	); err != nil {
		return err
	}
	if err := a.Store.SetToolCurrentVersion(ctx, toolID, tv.ID); err != nil {
		return err
	}
	a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "tool.version.approved",
		Payload: a.mustPayload(
			map[string]any{"tool_id": toolID, "version_id": versionID, "version": tv.Version},
		)})
	return nil
}

func (a *App) DeprecateTool(ctx context.Context, toolID string) error {
	t, err := a.Store.GetToolByID(ctx, toolID)
	if err != nil {
		return err
	}
	if t.CurrentVersionID == "" {
		return nil
	}
	return a.Store.UpdateToolVersionStatus(ctx, t.CurrentVersionID, model.ToolDeprecated, 0)
}

func (a *App) RunToolDirect(
	ctx context.Context,
	toolID, versionID string,
	inputs map[string]any,
) (model.Run, error) {
	t, err := a.Store.GetToolByID(ctx, toolID)
	if err != nil {
		return model.Run{}, err
	}
	if versionID == "" {
		versionID = t.CurrentVersionID
	}
	if versionID == "" {
		return model.Run{}, errors.New("tool has no version")
	}
	tv, err := a.Store.GetToolVersion(ctx, versionID)
	if err != nil {
		return model.Run{}, err
	}
	if tv.ToolID != t.ID {
		return model.Run{}, errors.New("version does not belong to this tool")
	}
	m, err := tools.ManifestFromModel(tv.Manifest)
	if err != nil {
		return model.Run{}, err
	}
	if iss := tools.ValidateInput(m, inputs); len(iss) > 0 {
		return model.Run{}, errors.New("invalid inputs: " + formatSchemaIssues(iss))
	}
	raw, _ := json.Marshal(inputs)
	run, err := a.Store.CreateRun(
		ctx,
		model.Run{TargetKind: "tool", TargetID: t.ID, TargetVer: tv.ID,
			Source: "manual", Inputs: raw, Status: model.RunQueued},
	)
	if err != nil {
		return model.Run{}, err
	}
	a.Bus.EmitNoErr(model.Event{Stream: model.RunStream(run.ID), Type: "run.queued", RunID: run.ID,
		Payload: a.mustPayload(
			map[string]any{"target": "tool", "name": t.Name, "source": "manual", "inputs": inputs},
		)})
	go a.executeToolRun(run.ID, t, tv, m, inputs)
	return run, nil
}

func (a *App) executeToolRun(
	runID string,
	t model.Tool,
	tv model.ToolVersion,
	m *tools.Manifest,
	inputs map[string]any,
) {
	ctx := context.Background()
	start := ids.NowMS()
	_ = a.Store.StartRun(ctx, runID)
	a.Bus.EmitNoErr(model.Event{Stream: model.RunStream(runID), Type: "run.started", RunID: runID,
		Payload: a.mustPayload(
			map[string]any{
				"target":     "tool",
				"name":       t.Name,
				"source":     "manual",
				"inputs":     inputs,
				"started_at": start,
			},
		)})
	step, err := a.Store.StartStepRun(ctx, runID, "tool", 1, "", runID+":tool")
	if err != nil {
		_ = a.Store.FinishRun(ctx, runID, model.RunFailed, err.Error())
		a.Bus.EmitNoErr(
			model.Event{Stream: model.RunStream(runID), Type: "run.finished", RunID: runID,
				Payload: a.mustPayload(
					map[string]any{"status": model.RunFailed, "error": err.Error()},
				)},
		)
		return
	}
	a.Bus.EmitNoErr(
		model.Event{
			Stream: model.RunStream(runID),
			Type:   "step.started",
			RunID:  runID,
			StepID: "tool",
			Payload: a.mustPayload(
				map[string]any{"step_id": "tool", "attempt": 1, "kind": "tool"},
			),
		},
	)
	a.Bus.EmitNoErr(
		model.Event{
			Stream: model.RunStream(runID),
			Type:   "tool.call.started",
			RunID:  runID,
			StepID: "tool",
			Payload: a.mustPayload(
				map[string]any{"tool": t.Name, "tool_version_id": tv.ID, "args": inputs},
			),
		},
	)
	onLog := func(stream, text string) {
		a.Bus.EmitNoErr(
			model.Event{
				Stream:  model.RunStream(runID),
				Type:    "step.log",
				RunID:   runID,
				StepID:  "tool",
				Payload: a.mustPayload(map[string]any{"stream": stream, "text": text}),
			},
		)
	}
	secVals, err := a.resolveToolSecretsForVersion(
		ctx,
		t.ID,
		tv.ID,
		m,
		runID,
		"tool",
		step.ID,
		"manual",
	)
	if err == nil {
		var out any
		out, err = a.Executor.Execute(ctx, m, inputs, tools.ExecOptions{
			ToolDir: tv.CodePath, ToolName: m.Name, Manifest: m, Secrets: secVals,
			RunID: runID, IdemKey: runID + ":tool", OnLog: onLog,
		})
		if err == nil {
			raw, _ := json.Marshal(out)
			_ = a.Store.FinishStepRun(ctx, step.ID, "succeeded", string(raw), "")
			a.Bus.EmitNoErr(
				model.Event{
					Stream: model.RunStream(runID),
					Type:   "step.finished",
					RunID:  runID,
					StepID: "tool",
					Payload: a.mustPayload(
						map[string]any{
							"step_id":    "tool",
							"status":     "succeeded",
							"output":     out,
							"output_ref": string(raw),
						},
					),
				},
			)
			a.Bus.EmitNoErr(
				model.Event{
					Stream: model.RunStream(runID),
					Type:   "tool.call.finished",
					RunID:  runID,
					StepID: "tool",
					Payload: a.mustPayload(
						map[string]any{"tool": t.Name, "status": "succeeded", "result": out},
					),
				},
			)
			_ = a.Store.FinishRun(ctx, runID, model.RunSucceeded, "")
			a.Bus.EmitNoErr(
				model.Event{Stream: model.RunStream(runID), Type: "run.finished", RunID: runID,
					Payload: a.mustPayload(
						map[string]any{
							"status":  model.RunSucceeded,
							"outputs": map[string]any{"result": out},
						},
					)},
			)
			return
		}
	}
	_ = a.Store.FinishStepRun(ctx, step.ID, "failed", "", err.Error())
	a.Bus.EmitNoErr(
		model.Event{
			Stream: model.RunStream(runID),
			Type:   "step.finished",
			RunID:  runID,
			StepID: "tool",
			Payload: a.mustPayload(
				map[string]any{"step_id": "tool", "status": "failed", "error": err.Error()},
			),
		},
	)
	a.Bus.EmitNoErr(
		model.Event{
			Stream: model.RunStream(runID),
			Type:   "tool.call.finished",
			RunID:  runID,
			StepID: "tool",
			Payload: a.mustPayload(
				map[string]any{"tool": t.Name, "status": "failed", "error": err.Error()},
			),
		},
	)
	_ = a.Store.FinishRun(ctx, runID, model.RunFailed, err.Error())
	a.Bus.EmitNoErr(model.Event{Stream: model.RunStream(runID), Type: "run.finished", RunID: runID,
		Payload: a.mustPayload(map[string]any{"status": model.RunFailed, "error": err.Error()})})
}

func (a *App) resolveToolSecretsForVersion(
	ctx context.Context,
	toolID, versionID string,
	m *tools.Manifest,
	runID, stepID, stepRunID, source string,
) (map[string]string, error) {
	out := map[string]string{}
	binds, _ := a.Store.SecretBindings(ctx, toolID)
	byName := map[string]model.SecretBinding{}
	for _, b := range binds {
		byName[b.LogicalName] = b
	}
	for _, d := range m.Secrets {
		b, ok := byName[d.Name]
		if !ok {
			if d.Required {
				return nil, &workflow.ToolError{
					Kind:    "missing_secret",
					Message: "secret " + d.Name + " is not bound to this tool",
				}
			}
			continue
		}
		val, rec, err := a.Secrets.Get(ctx, b.SecretID)
		if err != nil {
			if d.Required {
				return nil, &workflow.ToolError{
					Kind:    "missing_secret",
					Message: "secret " + d.Name + " is unavailable",
				}
			}
			continue
		}
		if rec.AskEachRun && runID != "" {
			dec, derr := a.RequestApproval(ctx, workflow.ApprovalRequest{
				RunID:       runID,
				StepID:      stepID,
				StepRunID:   stepRunID,
				Kind:        "secret",
				Title:       "Use secret " + rec.Name + "?",
				Description: "Tool " + m.Name + " wants to access this secret.",
				Source:      source,
				ExpiresAt:   time.Now().Add(30 * time.Minute),
			})
			if derr != nil || !dec.Approved {
				return nil, &workflow.ToolError{
					Kind:    "approval_denied",
					Message: "secret access was denied",
				}
			}
		}
		out[d.Name] = val
		_ = a.Store.LogSecretAccess(ctx, b.SecretID, runID, versionID)
		a.Bus.EmitNoErr(
			model.Event{
				Stream:  model.RunStream(runID),
				Type:    "secret.accessed",
				RunID:   runID,
				StepID:  stepID,
				Payload: a.mustPayload(map[string]any{"secret_id": b.SecretID, "tool": m.Name}),
			},
		)
	}
	return out, nil
}

func hashToolDir(manifestYAML, dir string) string {
	h := sha256.New()
	h.Write([]byte(manifestYAML))
	files := listCodeFiles(dir)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	for _, f := range files {
		h.Write([]byte(f.Path))
		if raw, err := os.ReadFile(filepath.Join(dir, f.Path)); err == nil {
			h.Write(raw)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func copyTree(src, dst string, limit int64) {
	_ = filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, _ := d.Info()
		if info != nil && info.Size() > limit {
			return nil
		}
		rel, _ := filepath.Rel(src, path)
		full := filepath.Join(dst, rel)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		raw, e := os.ReadFile(path)
		if e == nil {
			_ = os.WriteFile(full, raw, 0o644)
		}
		return nil
	})
}

func formatSchemaIssues(iss []schema.Issue) string {
	parts := []string{}
	for _, i := range iss {
		parts = append(parts, i.Path+": "+i.Message)
	}
	return strings.Join(parts, "; ")
}
