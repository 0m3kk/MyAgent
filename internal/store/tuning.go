package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"myagent/internal/ids"
	"myagent/internal/model"
)

func (s *Store) CreateEvalSuite(ctx context.Context, suite model.EvalSuite) (model.EvalSuite, error) {
	if suite.ID == "" {
		suite.ID = ids.New()
	}
	if suite.CreatedAt == 0 {
		suite.CreatedAt = ids.NowMS()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO eval_suites (id,name,target_kind,target_id,created_at) VALUES (?,?,?,?,?)`,
		suite.ID, suite.Name, suite.TargetKind, suite.TargetID, suite.CreatedAt)
	return suite, err
}

func (s *Store) ListEvalSuites(ctx context.Context) ([]model.EvalSuite, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,target_kind,target_id,created_at FROM eval_suites ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.EvalSuite{}
	for rows.Next() {
		var e model.EvalSuite
		if err := rows.Scan(&e.ID, &e.Name, &e.TargetKind, &e.TargetID, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) EvalSuiteByTarget(ctx context.Context, targetID string) (model.EvalSuite, error) {
	var e model.EvalSuite
	err := s.db.QueryRowContext(ctx, `SELECT id,name,target_kind,target_id,created_at FROM eval_suites WHERE target_id=? ORDER BY created_at LIMIT 1`, targetID).
		Scan(&e.ID, &e.Name, &e.TargetKind, &e.TargetID, &e.CreatedAt)
	return e, fmtErr("eval suite", err)
}

func (s *Store) EvalSuiteByName(ctx context.Context, name string) (model.EvalSuite, error) {
	var e model.EvalSuite
	err := s.db.QueryRowContext(ctx, `SELECT id,name,target_kind,target_id,created_at FROM eval_suites WHERE name=? ORDER BY created_at LIMIT 1`, name).
		Scan(&e.ID, &e.Name, &e.TargetKind, &e.TargetID, &e.CreatedAt)
	return e, fmtErr("eval suite", err)
}

func (s *Store) GetEvalSuite(ctx context.Context, id string) (model.EvalSuite, error) {
	var e model.EvalSuite
	err := s.db.QueryRowContext(ctx, `SELECT id,name,target_kind,target_id,created_at FROM eval_suites WHERE id=?`, id).
		Scan(&e.ID, &e.Name, &e.TargetKind, &e.TargetID, &e.CreatedAt)
	if err != nil {
		return e, fmtErr("eval suite", err)
	}
	e.Cases, _ = s.EvalCases(ctx, id)
	return e, nil
}

func (s *Store) AddEvalCase(ctx context.Context, c model.EvalCase) (model.EvalCase, error) {
	if c.ID == "" {
		c.ID = ids.New()
	}
	if c.CreatedAt == 0 {
		c.CreatedAt = ids.NowMS()
	}
	if len(c.Inputs) == 0 {
		c.Inputs = json.RawMessage(`{}`)
	}
	if len(c.Assertions) == 0 {
		c.Assertions = json.RawMessage(`[]`)
	}
	if c.Split == "" {
		c.Split = "train"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO eval_cases (id,suite_id,source_run_id,inputs,assertions,recorded_io_ref,split,created_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		c.ID, c.SuiteID, nullString(c.SourceRunID), string(c.Inputs), string(c.Assertions), nullString(c.RecordedIORef), c.Split, c.CreatedAt)
	return c, err
}

func (s *Store) EvalCases(ctx context.Context, suiteID string) ([]model.EvalCase, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,suite_id,COALESCE(source_run_id,''),inputs,assertions,COALESCE(recorded_io_ref,''),split,created_at
		FROM eval_cases WHERE suite_id=? ORDER BY created_at`, suiteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.EvalCase{}
	for rows.Next() {
		var c model.EvalCase
		var inputs, assertions string
		if err := rows.Scan(&c.ID, &c.SuiteID, &c.SourceRunID, &inputs, &assertions, &c.RecordedIORef, &c.Split, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.Inputs = json.RawMessage(mustJSON(inputs))
		c.Assertions = json.RawMessage(mustJSON(assertions))
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetEvalCase(ctx context.Context, id string) (model.EvalCase, error) {
	var c model.EvalCase
	var inputs, assertions string
	err := s.db.QueryRowContext(ctx, `SELECT id,suite_id,COALESCE(source_run_id,''),inputs,assertions,COALESCE(recorded_io_ref,''),split,created_at
		FROM eval_cases WHERE id=?`, id).
		Scan(&c.ID, &c.SuiteID, &c.SourceRunID, &inputs, &assertions, &c.RecordedIORef, &c.Split, &c.CreatedAt)
	c.Inputs = json.RawMessage(mustJSON(inputs))
	c.Assertions = json.RawMessage(mustJSON(assertions))
	return c, fmtErr("eval case", err)
}

func (s *Store) CreateEvalRun(ctx context.Context, r model.EvalRun) (model.EvalRun, error) {
	if r.ID == "" {
		r.ID = ids.New()
	}
	if r.StartedAt == 0 {
		r.StartedAt = ids.NowMS()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO eval_runs (id,suite_id,target_ver,status,started_at) VALUES (?,?,?,?,?)`,
		r.ID, r.SuiteID, r.TargetVer, r.Status, r.StartedAt)
	return r, err
}

func (s *Store) FinishEvalRun(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE eval_runs SET status=?, finished_at=? WHERE id=?`, status, ids.NowMS(), id)
	return err
}

func (s *Store) AddEvalResult(ctx context.Context, r model.EvalResult) (model.EvalResult, error) {
	if r.ID == "" {
		r.ID = ids.New()
	}
	if r.CreatedAt == 0 {
		r.CreatedAt = ids.NowMS()
	}
	if len(r.Output) == 0 {
		r.Output = json.RawMessage(`{}`)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO eval_results (id,eval_run_id,case_id,target_ver,passed,score,judge,cost_micro,latency_ms,output,error,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.EvalRunID, r.CaseID, r.TargetVer, boolInt(r.Passed), r.Score, r.Judge, r.CostMicro, r.LatencyMS, string(r.Output), r.Error, r.CreatedAt)
	return r, err
}

func (s *Store) GetEvalRun(ctx context.Context, id string) (model.EvalRun, error) {
	var r model.EvalRun
	var started, finished sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id,suite_id,target_ver,status,started_at,finished_at FROM eval_runs WHERE id=?`, id).
		Scan(&r.ID, &r.SuiteID, &r.TargetVer, &r.Status, &started, &finished)
	r.StartedAt, r.FinishedAt = started.Int64, finished.Int64
	if err != nil {
		return r, fmtErr("eval run", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,eval_run_id,case_id,target_ver,passed,score,judge,cost_micro,latency_ms,output,error,created_at
		FROM eval_results WHERE eval_run_id=? ORDER BY created_at`, id)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		var x model.EvalResult
		var passed int
		var output string
		if err := rows.Scan(&x.ID, &x.EvalRunID, &x.CaseID, &x.TargetVer, &passed, &x.Score, &x.Judge, &x.CostMicro, &x.LatencyMS, &output, &x.Error, &x.CreatedAt); err != nil {
			return r, err
		}
		x.Passed = passed == 1
		x.Output = json.RawMessage(mustJSON(output))
		r.Results = append(r.Results, x)
	}
	return r, rows.Err()
}

func (s *Store) CreateTuningProposal(ctx context.Context, p model.TuningProposal) (model.TuningProposal, error) {
	if p.ID == "" {
		p.ID = ids.New()
	}
	if p.CreatedAt == 0 {
		p.CreatedAt = ids.NowMS()
	}
	if p.Status == "" {
		p.Status = "proposed"
	}
	if len(p.EvalBefore) == 0 {
		p.EvalBefore = json.RawMessage(`{}`)
	}
	if len(p.EvalAfter) == 0 {
		p.EvalAfter = json.RawMessage(`{}`)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO tuning_proposals (id,target_kind,target_id,base_version_id,diagnosis,patch,proposed_yaml,status,eval_before,eval_after,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.TargetKind, p.TargetID, p.BaseVersionID, p.Diagnosis, p.Patch, p.ProposedYAML, p.Status,
		string(p.EvalBefore), string(p.EvalAfter), p.CreatedAt)
	return p, err
}

func (s *Store) ListTuningProposals(ctx context.Context) ([]model.TuningProposal, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,target_kind,target_id,base_version_id,diagnosis,patch,proposed_yaml,status,eval_before,eval_after,created_at
		FROM tuning_proposals ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.TuningProposal{}
	for rows.Next() {
		var p model.TuningProposal
		var before, after string
		if err := rows.Scan(&p.ID, &p.TargetKind, &p.TargetID, &p.BaseVersionID, &p.Diagnosis, &p.Patch, &p.ProposedYAML, &p.Status, &before, &after, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.EvalBefore = json.RawMessage(mustJSON(before))
		p.EvalAfter = json.RawMessage(mustJSON(after))
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetTuningProposal(ctx context.Context, id string) (model.TuningProposal, error) {
	var p model.TuningProposal
	var before, after string
	err := s.db.QueryRowContext(ctx, `SELECT id,target_kind,target_id,base_version_id,diagnosis,patch,proposed_yaml,status,eval_before,eval_after,created_at
		FROM tuning_proposals WHERE id=?`, id).
		Scan(&p.ID, &p.TargetKind, &p.TargetID, &p.BaseVersionID, &p.Diagnosis, &p.Patch, &p.ProposedYAML, &p.Status, &before, &after, &p.CreatedAt)
	p.EvalBefore = json.RawMessage(mustJSON(before))
	p.EvalAfter = json.RawMessage(mustJSON(after))
	return p, fmtErr("tuning proposal", err)
}

func (s *Store) SetTuningStatus(ctx context.Context, id, status string, after json.RawMessage) error {
	if len(after) == 0 {
		after = json.RawMessage(`{}`)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE tuning_proposals SET status=?, eval_after=? WHERE id=?`, status, string(after), id)
	return err
}

func (s *Store) AddNote(ctx context.Context, workspaceID, note string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO workspace_notes (id,workspace_id,note,created_at) VALUES (?,?,?,?)`,
		ids.New(), workspaceID, note, ids.NowMS())
	return err
}

func (s *Store) Notes(ctx context.Context, workspaceID string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT note FROM workspace_notes WHERE workspace_id=? ORDER BY created_at DESC LIMIT ?`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) UpsertPreset(ctx context.Context, kind, targetID, name string, inputs json.RawMessage) error {
	if len(inputs) == 0 {
		inputs = json.RawMessage(`{}`)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO presets (id,target_kind,target_id,name,inputs,created_at) VALUES (?,?,?,?,?,?)
		ON CONFLICT(target_kind,target_id,name) DO UPDATE SET inputs=excluded.inputs, created_at=excluded.created_at`,
		ids.New(), kind, targetID, name, string(inputs), ids.NowMS())
	return err
}

func (s *Store) Presets(ctx context.Context, kind, targetID string) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,inputs,created_at FROM presets WHERE target_kind=? AND target_id=? ORDER BY created_at DESC`, kind, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, inputs string
		var created int64
		if err := rows.Scan(&id, &name, &inputs, &created); err != nil {
			return nil, err
		}
		var val any
		_ = json.Unmarshal([]byte(mustJSON(inputs)), &val)
		out = append(out, map[string]any{"id": id, "name": name, "inputs": val, "created_at": created})
	}
	return out, rows.Err()
}
