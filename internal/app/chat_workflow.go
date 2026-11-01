package app

import (
	"context"
	"errors"
	"strings"

	"myagent/internal/model"
)

// StartWorkflowInChat starts a workflow run that belongs to a conversation and
// records a workflow-run part in the chat. The card is persisted as a normal
// assistant message so it survives reloads; live status is read from the run.
func (a *App) StartWorkflowInChat(
	ctx context.Context,
	conversationID, workflowID, versionID string,
	inputs map[string]any,
) (model.Run, model.Message, error) {
	conv, err := a.Store.GetConversation(ctx, conversationID)
	if err != nil {
		return model.Run{}, model.Message{}, err
	}
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		return model.Run{}, model.Message{}, errors.New("workflow is required")
	}
	run, err := a.RunWorkflow(
		ctx,
		workflowID,
		versionID,
		inputs,
		"chat",
		conv.ID,
		false,
		"ask",
		"",
	)
	if err != nil {
		return model.Run{}, model.Message{}, err
	}
	part, ok := a.workflowRunPart(ctx, run.ID)
	if !ok {
		return run, model.Message{}, errors.New(
			"workflow run started but the chat card could not be built",
		)
	}
	run.TargetName = part.Name
	msg := a.saveAssistantText(conv.ID, "", []model.Part{part}, "", 0, 0, 0)
	if msg.ID == "" {
		return run, model.Message{}, errors.New(
			"workflow run started but the chat card could not be saved",
		)
	}
	return run, msg, nil
}

// workflowRunPart builds the persisted part that powers the live run card in
// chat. The card resolves status from the run itself, so the part only carries
// stable references and display metadata.
func (a *App) workflowRunPart(ctx context.Context, runID string) (model.Part, bool) {
	run, err := a.Store.GetRun(ctx, runID)
	if err != nil || run.TargetKind != "workflow" {
		return model.Part{}, false
	}
	v, err := a.Store.GetWorkflowVersion(ctx, run.TargetVer)
	if err != nil {
		return model.Part{}, false
	}
	w, err := a.Store.GetWorkflowByID(ctx, run.TargetID)
	if err != nil {
		return model.Part{}, false
	}
	return model.Part{
		Type:   "workflow-run",
		Name:   w.Name,
		Status: run.Status,
		Result: a.mustPayload(map[string]any{
			"run_id":        run.ID,
			"workflow_id":   w.ID,
			"workflow_name": w.Name,
			"version_id":    v.ID,
			"version":       v.Version,
			"status":        run.Status,
		}),
	}, true
}
