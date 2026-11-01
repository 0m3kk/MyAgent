package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"myagent/internal/llm"
	"myagent/internal/model"
	"myagent/internal/workflow"
)

// AnalyzeRun reads a workflow run's trace and returns a read-only diagnosis.
// It never changes a workflow version or stores a proposal; the user can copy
// the suggested YAML and apply it manually.
func (a *App) AnalyzeRun(ctx context.Context, runID string) (model.RunAnalysis, error) {
	run, err := a.Store.GetRun(ctx, runID)
	if err != nil {
		return model.RunAnalysis{}, err
	}
	if run.TargetKind != "workflow" {
		return model.RunAnalysis{}, errors.New("only workflow runs can be analyzed")
	}
	v, err := a.Store.GetWorkflowVersion(ctx, run.TargetVer)
	if err != nil {
		return model.RunAnalysis{}, err
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
		`A workflow run failed or did not meet expectations. Diagnose the root cause from the trace.

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
{"diagnosis":"...", "patch_summary":"...", "expected_effect":"...", "proposed_yaml":"<optional corrected workflow YAML>"}
The diagnosis must classify the cause: bad input, wrong tool choice, weak prompt, tool bug, model limit, or flaky external service. Do not change steps that passed. Leave proposed_yaml empty when a YAML change would not fix the problem.`,
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
		System:    "You are the run analyst inside MyAgent. Diagnose from evidence, not guesses. Identify one root cause and explain the smallest fix. The response is advisory only; never claim that a change was applied.",
		Messages:  []llm.Message{{Role: "user", Content: prompt}},
		OutputSchema: map[string]any{
			"type":     "object",
			"required": []any{"diagnosis"},
			"properties": map[string]any{
				"diagnosis":       map[string]any{"type": "string"},
				"patch_summary":   map[string]any{"type": "string"},
				"expected_effect": map[string]any{"type": "string"},
				"proposed_yaml":   map[string]any{"type": "string"},
			},
		},
	})
	if err != nil {
		return model.RunAnalysis{}, err
	}
	data, _ := res.Data.(map[string]any)
	if data == nil {
		return model.RunAnalysis{}, errors.New(
			"the model did not return a structured diagnosis; use a stronger planner model",
		)
	}
	analysis := model.RunAnalysis{
		RunID:          runID,
		Diagnosis:      str(data, "diagnosis"),
		PatchSummary:   str(data, "patch_summary"),
		ExpectedEffect: str(data, "expected_effect"),
		ProposedYAML:   str(data, "proposed_yaml"),
	}
	if analysis.Diagnosis == "" {
		return model.RunAnalysis{}, errors.New("the model returned an empty diagnosis")
	}
	return analysis, nil
}
