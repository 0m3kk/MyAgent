package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"myagent/internal/ids"
	"myagent/internal/model"
	"myagent/internal/workflow"
)

func (a *App) RequestApproval(ctx context.Context, req workflow.ApprovalRequest) (workflow.ApprovalDecision, error) {
	automated := req.Source != "" && req.Source != "chat" && req.Source != "manual"
	if automated && req.Unattended == "allow" {
		id := ids.New()
		payload := map[string]any{
			"id": id, "kind": req.Kind, "title": req.Title, "description": req.Description,
			"run_id": req.RunID, "step_id": req.StepID, "source": req.Source,
			"policy": "auto-approved", "expires_at": 0,
		}
		raw, _ := json.Marshal(payload)
		_, _ = a.Store.CreateApproval(ctx, model.Approval{ID: id, RunID: req.RunID, StepRunID: req.StepRunID,
			ConversationID: req.ConversationID, Kind: req.Kind, Request: raw, Status: "approved",
			Decision: "approve", Comment: "auto-approved by unattended policy", DecidedAt: ids.NowMS()})
		stream := model.StreamSys
		if req.RunID != "" {
			stream = model.RunStream(req.RunID)
		}
		a.Bus.EmitNoErr(model.Event{Stream: stream, Type: "approval.requested", RunID: req.RunID, StepID: req.StepID,
			Payload: a.mustPayload(payload)})
		a.Bus.EmitNoErr(model.Event{Stream: stream, Type: "approval.resolved", RunID: req.RunID, StepID: req.StepID,
			Payload: a.mustPayload(map[string]any{"approval_id": id, "decision": "approve", "decided_by": "policy"})})
		return workflow.ApprovalDecision{Approved: true, Comment: "auto-approved by unattended policy", DecidedBy: "policy"}, nil
	}
	if automated && req.Unattended == "deny" {
		return workflow.ApprovalDecision{}, fmt.Errorf("approval_denied: workflow does not allow unattended approvals")
	}
	expires := req.ExpiresAt
	if expires.IsZero() {
		expires = time.Now().Add(24 * time.Hour)
	}
	approvalID := ids.New()
	payload := map[string]any{
		"id": approvalID, "kind": req.Kind, "title": req.Title, "description": req.Description,
		"message": req.Message, "show": req.Show, "run_id": req.RunID, "step_id": req.StepID,
		"conversation_id": req.ConversationID, "source": req.Source, "unattended": req.Unattended,
		"expires_at": expires.UnixMilli(),
	}
	requestRaw, _ := json.Marshal(payload)
	approval, err := a.Store.CreateApproval(ctx, model.Approval{
		ID: approvalID, RunID: req.RunID, StepRunID: req.StepRunID, ConversationID: req.ConversationID,
		Kind: req.Kind, Request: requestRaw, Status: "pending", ExpiresAt: expires.UnixMilli(),
	})
	if err != nil {
		return workflow.ApprovalDecision{}, err
	}
	waiter := &ApprovalWaiter{ID: approval.ID, Ch: make(chan workflow.ApprovalDecision, 1), Kind: req.Kind}
	a.approvalMu.Lock()
	a.waiters[approval.ID] = waiter
	a.approvalMu.Unlock()
	defer func() {
		a.approvalMu.Lock()
		delete(a.waiters, approval.ID)
		a.approvalMu.Unlock()
	}()

	stream := model.StreamSys
	if req.RunID != "" {
		stream = model.RunStream(req.RunID)
	} else if req.ConversationID != "" {
		stream = model.ConvStream(req.ConversationID)
	}
	a.Bus.EmitNoErr(model.Event{Stream: stream, Type: "approval.requested", RunID: req.RunID, StepID: req.StepID,
		Payload: a.mustPayload(payload)})
	if req.RunID != "" {
		_ = a.Store.SetRunStatus(ctx, req.RunID, model.RunWaitingApproval, "")
		a.notify("MyAgent needs approval", req.Title)
	}
	timer := time.NewTimer(time.Until(expires))
	defer timer.Stop()
	select {
	case d := <-waiter.Ch:
		if req.RunID != "" {
			_ = a.Store.SetRunStatus(context.Background(), req.RunID, model.RunRunning, "")
		}
		return d, nil
	case <-timer.C:
		_ = a.setApprovalStatus(context.Background(), approval.ID, "expired", "timeout", "approval expired")
		stream2 := stream
		a.Bus.EmitNoErr(model.Event{Stream: stream2, Type: "approval.resolved", RunID: req.RunID, StepID: req.StepID,
			Payload: a.mustPayload(map[string]any{"approval_id": approval.ID, "decision": "timeout"})})
		if req.RunID != "" {
			_ = a.Store.SetRunStatus(context.Background(), req.RunID, model.RunRunning, "")
		}
		return workflow.ApprovalDecision{}, fmt.Errorf("approval_timeout: no decision before the deadline")
	case <-ctx.Done():
		_ = a.setApprovalStatus(context.Background(), approval.ID, "expired", "cancelled", "run cancelled")
		return workflow.ApprovalDecision{}, ctx.Err()
	}
}

func (a *App) setApprovalStatus(ctx context.Context, id, status, decision, comment string) error {
	_, err := a.Store.DB().ExecContext(ctx, `UPDATE approvals SET status=?, decision=?, comment=?, decided_at=? WHERE id=? AND status='pending'`,
		status, decision, comment, ids.NowMS(), id)
	return err
}

func (a *App) ResolveApproval(ctx context.Context, id, decision, comment string) (model.Approval, error) {
	ap, err := a.Store.GetApproval(ctx, id)
	if err != nil {
		return ap, err
	}
	if ap.Status != "pending" {
		return ap, fmt.Errorf("approval is already %s", ap.Status)
	}
	decision = strings.ToLower(strings.TrimSpace(decision))
	if decision != "approve" && decision != "reject" {
		return ap, fmt.Errorf("decision must be approve or reject")
	}
	if err := a.Store.ResolveApproval(ctx, id, decision, comment); err != nil {
		return ap, err
	}
	ap, _ = a.Store.GetApproval(ctx, id)
	stream := model.StreamSys
	if ap.RunID != "" {
		stream = model.RunStream(ap.RunID)
	} else if ap.ConversationID != "" {
		stream = model.ConvStream(ap.ConversationID)
	}
	a.Bus.EmitNoErr(model.Event{Stream: stream, Type: "approval.resolved", RunID: ap.RunID,
		Payload: a.mustPayload(map[string]any{"approval_id": id, "decision": decision, "comment": comment})})
	a.approvalMu.Lock()
	w := a.waiters[id]
	a.approvalMu.Unlock()
	if w != nil {
		select {
		case w.Ch <- workflow.ApprovalDecision{Approved: decision == "approve", Comment: comment, DecidedBy: "user"}:
		default:
		}
	}
	return ap, nil
}

func (a *App) PendingApprovals(ctx context.Context) ([]model.Approval, error) {
	_ = a.expirePendingApprovals(ctx)
	return a.Store.PendingApprovals(ctx)
}

func (a *App) expirePendingApprovals(ctx context.Context) int {
	n, _ := a.Store.ExpireApprovals(ctx, ids.NowMS())
	return n
}

func (a *App) AskUser(ctx context.Context, conversationID, question, kind string, options []string) (string, error) {
	if strings.TrimSpace(question) == "" {
		return "", fmt.Errorf("question is empty")
	}
	req := workflow.ApprovalRequest{
		ConversationID: conversationID, Kind: "question", Title: question,
		Description: "The agent needs your input.", Message: question, Source: "chat",
		ExpiresAt: time.Now().Add(2 * time.Hour),
	}
	payload := map[string]any{"question": question, "kind": kind, "options": options}
	d, err := a.askApproval(ctx, req, payload)
	if err != nil {
		return "", err
	}
	if d.Comment != "" {
		return d.Comment, nil
	}
	if d.Approved {
		return "yes", nil
	}
	return "", fmt.Errorf("user did not answer")
}

// askApproval is the generic blocking card used by questions and tool
// approvals. The request payload is merged into the approval event.
func (a *App) askApproval(ctx context.Context, req workflow.ApprovalRequest, payload map[string]any) (workflow.ApprovalDecision, error) {
	expires := req.ExpiresAt
	if expires.IsZero() {
		expires = time.Now().Add(2 * time.Hour)
	}
	id := ids.New()
	payload["id"] = id
	payload["kind"] = req.Kind
	payload["expires_at"] = expires.UnixMilli()
	raw, _ := json.Marshal(payload)
	_, err := a.Store.CreateApproval(ctx, model.Approval{ID: id, RunID: req.RunID, ConversationID: req.ConversationID,
		Kind: req.Kind, Request: raw, Status: "pending", ExpiresAt: expires.UnixMilli()})
	if err != nil {
		return workflow.ApprovalDecision{}, err
	}
	waiter := &ApprovalWaiter{ID: id, Ch: make(chan workflow.ApprovalDecision, 1), Kind: req.Kind}
	a.approvalMu.Lock()
	a.waiters[id] = waiter
	a.approvalMu.Unlock()
	defer func() {
		a.approvalMu.Lock()
		delete(a.waiters, id)
		a.approvalMu.Unlock()
	}()
	stream := model.StreamSys
	if req.ConversationID != "" {
		stream = model.ConvStream(req.ConversationID)
	}
	if req.RunID != "" {
		stream = model.RunStream(req.RunID)
	}
	a.Bus.EmitNoErr(model.Event{Stream: stream, Type: "approval.requested", RunID: req.RunID, StepID: req.StepID,
		Payload: a.mustPayload(payload)})
	timer := time.NewTimer(time.Until(expires))
	defer timer.Stop()
	select {
	case d := <-waiter.Ch:
		return d, nil
	case <-timer.C:
		_ = a.setApprovalStatus(context.Background(), id, "expired", "timeout", "approval expired")
		a.Bus.EmitNoErr(model.Event{Stream: stream, Type: "approval.resolved", RunID: req.RunID,
			Payload: a.mustPayload(map[string]any{"approval_id": id, "decision": "timeout"})})
		return workflow.ApprovalDecision{}, fmt.Errorf("approval_timeout")
	case <-ctx.Done():
		_ = a.setApprovalStatus(context.Background(), id, "expired", "cancelled", "run cancelled")
		return workflow.ApprovalDecision{}, ctx.Err()
	}
}
