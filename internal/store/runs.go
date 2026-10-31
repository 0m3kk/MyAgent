package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"myagent/internal/ids"
	"myagent/internal/model"
)

const runSelect = `SELECT r.id,r.target_kind,COALESCE(r.target_id,''),r.target_ver,
	COALESCE(w.name,t.name,'') AS target_name,
	r.source,COALESCE(r.trigger_id,''),COALESCE(r.conversation_id,''),COALESCE(r.parent_run_id,''),
	r.inputs,r.status,COALESCE(r.started_at,0),COALESCE(r.finished_at,0),
	r.tokens_in,r.tokens_out,r.cost_micro,COALESCE(r.error,''),r.created_at
	FROM runs r
	LEFT JOIN workflow_versions wv ON wv.id=r.target_ver
	LEFT JOIN workflows w ON w.id=wv.workflow_id
	LEFT JOIN tool_versions tv ON tv.id=r.target_ver
	LEFT JOIN tools t ON t.id=tv.tool_id`

func scanRun(scan func(dest ...any) error) (model.Run, error) {
	var r model.Run
	var inputs string
	err := scan(&r.ID, &r.TargetKind, &r.TargetID, &r.TargetVer, &r.TargetName, &r.Source, &r.TriggerID,
		&r.ConversationID, &r.ParentRunID, &inputs, &r.Status, &r.StartedAt, &r.FinishedAt,
		&r.TokensIn, &r.TokensOut, &r.CostMicro, &r.Error, &r.CreatedAt)
	r.Inputs = json.RawMessage(mustJSON(inputs))
	return r, err
}

func (s *Store) CreateRun(ctx context.Context, r model.Run) (model.Run, error) {
	if r.ID == "" {
		r.ID = ids.New()
	}
	if r.CreatedAt == 0 {
		r.CreatedAt = ids.NowMS()
	}
	if len(r.Inputs) == 0 {
		r.Inputs = json.RawMessage(`{}`)
	}
	if r.Status == "" {
		r.Status = model.RunQueued
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO runs (id,target_kind,target_id,target_ver,source,trigger_id,conversation_id,parent_run_id,inputs,status,started_at,finished_at,tokens_in,tokens_out,cost_micro,error,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.TargetKind, nullString(r.TargetID), r.TargetVer, r.Source, nullString(r.TriggerID),
		nullString(r.ConversationID), nullString(r.ParentRunID), string(r.Inputs), r.Status,
		nullInt(r.StartedAt), nullInt(r.FinishedAt), r.TokensIn, r.TokensOut, r.CostMicro, nullString(r.Error), r.CreatedAt)
	return r, err
}

func (s *Store) GetRun(ctx context.Context, id string) (model.Run, error) {
	row := s.db.QueryRowContext(ctx, runSelect+` WHERE r.id = ?`, id)
	r, err := scanRun(row.Scan)
	if err != nil {
		return r, fmtErr("run", err)
	}
	steps, err := s.ListStepRuns(ctx, id)
	if err == nil {
		r.Steps = steps
	}
	return r, nil
}

func (s *Store) ListRuns(ctx context.Context, f model.RunFilter) ([]model.Run, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	query := runSelect + ` WHERE 1=1`
	args := []any{}
	if f.TargetID != "" {
		query += ` AND (r.target_id = ? OR w.id = ?)`
		args = append(args, f.TargetID, f.TargetID)
	}
	if f.Status != "" {
		query += ` AND r.status = ?`
		args = append(args, f.Status)
	}
	if f.Source != "" {
		query += ` AND r.source = ?`
		args = append(args, f.Source)
	}
	query += ` ORDER BY r.created_at DESC LIMIT ? OFFSET ?`
	args = append(args, f.Limit, f.Offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Run{}
	for rows.Next() {
		r, err := scanRun(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) StartRun(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE runs SET status=?, started_at=? WHERE id=?`, model.RunRunning, ids.NowMS(), id)
	return err
}

func (s *Store) SetRunStatus(ctx context.Context, id, status, errText string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE runs SET status=?, error=? WHERE id=?`, status, nullString(errText), id)
	return err
}

func (s *Store) FinishRun(ctx context.Context, id, status, errText string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE runs SET status=?, finished_at=?, error=? WHERE id=?`,
		status, ids.NowMS(), nullString(errText), id)
	return err
}

func (s *Store) AddRunUsage(ctx context.Context, id string, tokensIn, tokensOut int, costMicro int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE runs SET tokens_in=tokens_in+?, tokens_out=tokens_out+?, cost_micro=cost_micro+? WHERE id=?`,
		tokensIn, tokensOut, costMicro, id)
	return err
}

func (s *Store) CountActiveRuns(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE status IN ('queued','running','paused','waiting_approval','waiting_secret')`).Scan(&n)
	return n, err
}

func (s *Store) CountActiveRunsForTarget(ctx context.Context, targetID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE target_id=? AND status IN ('queued','running','paused','waiting_approval','waiting_secret')`, targetID).Scan(&n)
	return n, err
}

func (s *Store) ActiveRunsForTarget(ctx context.Context, targetID string) ([]model.Run, error) {
	rows, err := s.db.QueryContext(ctx, runSelect+` WHERE r.target_id=? AND r.status IN ('queued','running','paused','waiting_approval','waiting_secret') ORDER BY r.created_at`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Run{}
	for rows.Next() {
		r, err := scanRun(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) InterruptedRuns(ctx context.Context) ([]model.Run, error) {
	rows, err := s.db.QueryContext(ctx, runSelect+` WHERE r.status IN ('running','waiting_approval','waiting_secret') ORDER BY r.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Run{}
	for rows.Next() {
		r, err := scanRun(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---- step runs ----

func (s *Store) StartStepRun(ctx context.Context, runID, stepID string, attempt int, inputRef, idemKey string) (model.StepRun, error) {
	if attempt <= 0 {
		attempt = 1
	}
	now := ids.NowMS()
	sr := model.StepRun{ID: ids.New(), RunID: runID, StepID: stepID, Attempt: attempt,
		Status: "running", InputRef: inputRef, IdemKey: idemKey, StartedAt: now}
	_, err := s.db.ExecContext(ctx, `INSERT INTO step_runs (id,run_id,step_id,attempt,status,input_ref,idem_key,started_at)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(run_id,step_id,attempt) DO UPDATE SET status='running', input_ref=excluded.input_ref,
			idem_key=excluded.idem_key, started_at=excluded.started_at, finished_at=NULL, output_ref=NULL, error=NULL`,
		sr.ID, sr.RunID, sr.StepID, sr.Attempt, sr.Status, nullString(sr.InputRef), nullString(sr.IdemKey), sr.StartedAt)
	if err != nil {
		return sr, err
	}
	// The upsert may keep an older row ID; return the actual row.
	return s.GetStepRunAttempt(ctx, runID, stepID, attempt)
}

func (s *Store) FinishStepRun(ctx context.Context, id, status, outputRef, errText string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE step_runs SET status=?, output_ref=?, error=?, finished_at=? WHERE id=?`,
		status, nullString(outputRef), nullString(errText), ids.NowMS(), id)
	return err
}

func (s *Store) GetStepRunAttempt(ctx context.Context, runID, stepID string, attempt int) (model.StepRun, error) {
	var sr model.StepRun
	var input, output, errText, idem sql.NullString
	var started, finished sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id,run_id,step_id,attempt,status,input_ref,output_ref,error,idem_key,started_at,finished_at
		FROM step_runs WHERE run_id=? AND step_id=? AND attempt=?`, runID, stepID, attempt).
		Scan(&sr.ID, &sr.RunID, &sr.StepID, &sr.Attempt, &sr.Status, &input, &output, &errText, &idem, &started, &finished)
	sr.InputRef, sr.OutputRef, sr.Error, sr.IdemKey = input.String, output.String, errText.String, idem.String
	sr.StartedAt, sr.FinishedAt = started.Int64, finished.Int64
	return sr, fmtErr("step run", err)
}

func (s *Store) LatestStepAttempt(ctx context.Context, runID, stepID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(attempt),0) FROM step_runs WHERE run_id=? AND step_id=?`, runID, stepID).Scan(&n)
	return n, err
}

func (s *Store) SucceededStepAttempt(ctx context.Context, runID, stepID string) (model.StepRun, bool, error) {
	var sr model.StepRun
	var output sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,run_id,step_id,attempt,status,COALESCE(input_ref,''),COALESCE(output_ref,''),COALESCE(error,''),COALESCE(idem_key,''),COALESCE(started_at,0),COALESCE(finished_at,0)
		FROM step_runs WHERE run_id=? AND step_id=? AND status='succeeded' ORDER BY attempt DESC LIMIT 1`, runID, stepID).
		Scan(&sr.ID, &sr.RunID, &sr.StepID, &sr.Attempt, &sr.Status, &sr.InputRef, &output, &sr.Error, &sr.IdemKey, &sr.StartedAt, &sr.FinishedAt)
	if isNoRows(err) {
		return sr, false, nil
	}
	sr.OutputRef = output.String
	return sr, err == nil, err
}

func (s *Store) ListStepRuns(ctx context.Context, runID string) ([]model.StepRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,run_id,step_id,attempt,status,COALESCE(input_ref,''),COALESCE(output_ref,''),COALESCE(error,''),COALESCE(idem_key,''),COALESCE(started_at,0),COALESCE(finished_at,0)
		FROM step_runs WHERE run_id=? ORDER BY started_at, step_id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.StepRun{}
	for rows.Next() {
		var sr model.StepRun
		if err := rows.Scan(&sr.ID, &sr.RunID, &sr.StepID, &sr.Attempt, &sr.Status, &sr.InputRef, &sr.OutputRef, &sr.Error, &sr.IdemKey, &sr.StartedAt, &sr.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, sr)
	}
	return out, rows.Err()
}

func (s *Store) AddRunBinding(ctx context.Context, runID, kind, ref, versionID, hash string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO run_bindings (run_id,kind,ref,version_id,content_hash) VALUES (?,?,?,?,?)
		ON CONFLICT(run_id,kind,ref) DO UPDATE SET version_id=excluded.version_id, content_hash=excluded.content_hash`,
		runID, kind, ref, versionID, hash)
	return err
}

func (s *Store) RunBindings(ctx context.Context, runID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT kind,ref,version_id FROM run_bindings WHERE run_id=?`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var kind, ref, version string
		if err := rows.Scan(&kind, &ref, &version); err != nil {
			return nil, err
		}
		out[kind+":"+ref] = version
	}
	return out, rows.Err()
}

// ---- approvals ----

func (s *Store) CreateApproval(ctx context.Context, a model.Approval) (model.Approval, error) {
	if a.ID == "" {
		a.ID = ids.New()
	}
	if a.CreatedAt == 0 {
		a.CreatedAt = ids.NowMS()
	}
	if a.Status == "" {
		a.Status = "pending"
	}
	if len(a.Request) == 0 {
		a.Request = json.RawMessage(`{}`)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO approvals (id,run_id,step_run_id,conversation_id,kind,request,status,decision,comment,expires_at,decided_at,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, nullString(a.RunID), nullString(a.StepRunID), nullString(a.ConversationID), a.Kind, string(a.Request),
		a.Status, nullString(a.Decision), nullString(a.Comment), nullInt(a.ExpiresAt), nullInt(a.DecidedAt), a.CreatedAt)
	return a, err
}

func (s *Store) GetApproval(ctx context.Context, id string) (model.Approval, error) {
	var a model.Approval
	var request string
	var runID, stepID, convID, decision, comment sql.NullString
	var expires, decided sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id,run_id,step_run_id,conversation_id,kind,request,status,decision,comment,expires_at,decided_at,created_at FROM approvals WHERE id=?`, id).
		Scan(&a.ID, &runID, &stepID, &convID, &a.Kind, &request, &a.Status, &decision, &comment, &expires, &decided, &a.CreatedAt)
	a.Request = json.RawMessage(mustJSON(request))
	a.RunID, a.StepRunID, a.ConversationID = runID.String, stepID.String, convID.String
	a.Decision, a.Comment, a.ExpiresAt, a.DecidedAt = decision.String, comment.String, expires.Int64, decided.Int64
	return a, fmtErr("approval", err)
}

func (s *Store) ResolveApproval(ctx context.Context, id, decision, comment string) error {
	status := "approved"
	if decision == "reject" || decision == "deny" {
		status = "rejected"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE approvals SET status=?, decision=?, comment=?, decided_at=? WHERE id=? AND status='pending'`,
		status, decision, comment, ids.NowMS(), id)
	return err
}

func (s *Store) PendingApprovals(ctx context.Context) ([]model.Approval, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,COALESCE(run_id,''),COALESCE(step_run_id,''),COALESCE(conversation_id,''),kind,request,status,COALESCE(decision,''),COALESCE(comment,''),COALESCE(expires_at,0),COALESCE(decided_at,0),created_at
		FROM approvals WHERE status='pending' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Approval{}
	for rows.Next() {
		var a model.Approval
		var request string
		if err := rows.Scan(&a.ID, &a.RunID, &a.StepRunID, &a.ConversationID, &a.Kind, &request, &a.Status, &a.Decision, &a.Comment, &a.ExpiresAt, &a.DecidedAt, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Request = json.RawMessage(mustJSON(request))
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) ApprovalsForRun(ctx context.Context, runID string) ([]model.Approval, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,COALESCE(run_id,''),COALESCE(step_run_id,''),COALESCE(conversation_id,''),kind,request,status,COALESCE(decision,''),COALESCE(comment,''),COALESCE(expires_at,0),COALESCE(decided_at,0),created_at
		FROM approvals WHERE run_id=? ORDER BY created_at`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Approval{}
	for rows.Next() {
		var a model.Approval
		var request string
		if err := rows.Scan(&a.ID, &a.RunID, &a.StepRunID, &a.ConversationID, &a.Kind, &request, &a.Status, &a.Decision, &a.Comment, &a.ExpiresAt, &a.DecidedAt, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Request = json.RawMessage(mustJSON(request))
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) ExpireApprovalsForRun(ctx context.Context, runID, reason string) (int, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE approvals SET status='expired', decision='interrupted', comment=?, decided_at=? WHERE run_id=? AND status='pending'`,
		reason, ids.NowMS(), runID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *Store) ExpireApprovals(ctx context.Context, now int64) (int, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE approvals SET status='expired', decided_at=? WHERE status='pending' AND expires_at IS NOT NULL AND expires_at <= ?`, now, now)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ---- artifacts ----

func (s *Store) AddArtifact(ctx context.Context, a model.Artifact) (model.Artifact, error) {
	if a.ID == "" {
		a.ID = ids.New()
	}
	if a.CreatedAt == 0 {
		a.CreatedAt = ids.NowMS()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO artifacts (id,run_id,step_run_id,path,sha256,size,mime,created_at) VALUES (?,?,?,?,?,?,?,?)`,
		a.ID, nullString(a.RunID), nullString(a.StepRunID), a.Path, a.SHA256, a.Size, a.Mime, a.CreatedAt)
	return a, err
}

func (s *Store) GetArtifact(ctx context.Context, id string) (model.Artifact, error) {
	var a model.Artifact
	err := s.db.QueryRowContext(ctx, `SELECT id,COALESCE(run_id,''),COALESCE(step_run_id,''),path,sha256,size,mime,created_at FROM artifacts WHERE id=?`, id).
		Scan(&a.ID, &a.RunID, &a.StepRunID, &a.Path, &a.SHA256, &a.Size, &a.Mime, &a.CreatedAt)
	return a, fmtErr("artifact", err)
}

// ---- feedback ----

func (s *Store) UpsertFeedback(ctx context.Context, f model.Feedback) (model.Feedback, error) {
	if f.ID == "" {
		f.ID = ids.New()
	}
	if f.CreatedAt == 0 {
		f.CreatedAt = ids.NowMS()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO feedback (id,target_kind,target_id,rating,note,created_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(target_kind,target_id) DO UPDATE SET rating=excluded.rating, note=excluded.note, created_at=excluded.created_at`,
		f.ID, f.TargetKind, f.TargetID, f.Rating, f.Note, f.CreatedAt)
	if err != nil {
		return f, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT id,target_kind,target_id,rating,note,created_at FROM feedback WHERE target_kind=? AND target_id=?`, f.TargetKind, f.TargetID).
		Scan(&f.ID, &f.TargetKind, &f.TargetID, &f.Rating, &f.Note, &f.CreatedAt)
	return f, err
}

func (s *Store) GetFeedback(ctx context.Context, kind, targetID string) (model.Feedback, error) {
	var f model.Feedback
	err := s.db.QueryRowContext(ctx, `SELECT id,target_kind,target_id,rating,note,created_at FROM feedback WHERE target_kind=? AND target_id=?`, kind, targetID).
		Scan(&f.ID, &f.TargetKind, &f.TargetID, &f.Rating, &f.Note, &f.CreatedAt)
	return f, err
}
