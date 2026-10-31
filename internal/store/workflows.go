package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"myagent/internal/ids"
	"myagent/internal/model"
)

func (s *Store) GetOrCreateWorkflow(ctx context.Context, workspaceID, name string) (model.Workflow, error) {
	var w model.Workflow
	err := s.db.QueryRowContext(ctx, `SELECT id,workspace_id,name,COALESCE(current_version_id,''),created_at FROM workflows WHERE workspace_id=? AND name=?`,
		workspaceID, name).Scan(&w.ID, &w.WorkspaceID, &w.Name, &w.CurrentVersionID, &w.CreatedAt)
	if err == nil {
		return w, nil
	}
	if !isNoRows(err) {
		return w, err
	}
	w = model.Workflow{ID: ids.New(), WorkspaceID: workspaceID, Name: name, CreatedAt: ids.NowMS()}
	_, err = s.db.ExecContext(ctx, `INSERT INTO workflows (id,workspace_id,name,current_version_id,created_at) VALUES (?,?,?,NULL,?)`,
		w.ID, w.WorkspaceID, w.Name, w.CreatedAt)
	return w, err
}

func (s *Store) AddWorkflowVersion(ctx context.Context, v model.WorkflowVersion) (model.WorkflowVersion, error) {
	if v.ID == "" {
		v.ID = ids.New()
	}
	if v.CreatedAt == 0 {
		v.CreatedAt = ids.NowMS()
	}
	if len(v.InputSchema) == 0 {
		v.InputSchema = json.RawMessage(`{}`)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM workflow_versions WHERE workflow_id=?`, v.WorkflowID).Scan(&v.Version); err != nil {
		_ = tx.Rollback()
		return v, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO workflow_versions (id,workflow_id,version,yaml,input_schema,content_hash,status,change_note,parent_id,created_by,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		v.ID, v.WorkflowID, v.Version, v.YAML, string(v.InputSchema), v.ContentHash, v.Status, v.ChangeNote, nullString(v.ParentID), v.CreatedBy, v.CreatedAt)
	if err == nil {
		// The UI edits the newest version even while it is a draft; approval is
		// required before it can be started by a trigger.
		_, err = tx.ExecContext(ctx, `UPDATE workflows SET current_version_id = ? WHERE id = ?`, v.ID, v.WorkflowID)
	}
	if err != nil {
		_ = tx.Rollback()
		return v, err
	}
	if err := tx.Commit(); err != nil {
		return v, err
	}
	return v, nil
}

func (s *Store) GetWorkflowVersion(ctx context.Context, id string) (model.WorkflowVersion, error) {
	var v model.WorkflowVersion
	var schema string
	var parent sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,workflow_id,version,yaml,input_schema,content_hash,status,change_note,COALESCE(parent_id,''),created_by,created_at
		FROM workflow_versions WHERE id = ?`, id).
		Scan(&v.ID, &v.WorkflowID, &v.Version, &v.YAML, &schema, &v.ContentHash, &v.Status, &v.ChangeNote, &parent, &v.CreatedBy, &v.CreatedAt)
	v.InputSchema = json.RawMessage(mustJSON(schema))
	return v, fmtErr("workflow version", err)
}

func (s *Store) GetWorkflowVersionByNumber(ctx context.Context, workflowID string, version int) (model.WorkflowVersion, error) {
	var id string
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM workflow_versions WHERE workflow_id=? AND version=?`, workflowID, version).Scan(&id); err != nil {
		return model.WorkflowVersion{}, fmtErr("workflow version", err)
	}
	return s.GetWorkflowVersion(ctx, id)
}

func (s *Store) WorkflowVersions(ctx context.Context, workflowID string) ([]model.WorkflowVersion, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,workflow_id,version,yaml,input_schema,content_hash,status,change_note,COALESCE(parent_id,''),created_by,created_at
		FROM workflow_versions WHERE workflow_id = ? ORDER BY version DESC`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.WorkflowVersion{}
	for rows.Next() {
		var v model.WorkflowVersion
		var schema string
		if err := rows.Scan(&v.ID, &v.WorkflowID, &v.Version, &v.YAML, &schema, &v.ContentHash, &v.Status, &v.ChangeNote, &v.ParentID, &v.CreatedBy, &v.CreatedAt); err != nil {
			return nil, err
		}
		v.InputSchema = json.RawMessage(mustJSON(schema))
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) GetWorkflowByName(ctx context.Context, name string) (model.Workflow, error) {
	var w model.Workflow
	err := s.db.QueryRowContext(ctx, `SELECT id,workspace_id,name,COALESCE(current_version_id,''),created_at FROM workflows WHERE name=? ORDER BY created_at LIMIT 1`, name).
		Scan(&w.ID, &w.WorkspaceID, &w.Name, &w.CurrentVersionID, &w.CreatedAt)
	return w, fmtErr("workflow", err)
}

func (s *Store) GetWorkflowByID(ctx context.Context, id string) (model.Workflow, error) {
	var w model.Workflow
	err := s.db.QueryRowContext(ctx, `SELECT id,workspace_id,name,COALESCE(current_version_id,''),created_at FROM workflows WHERE id=?`, id).
		Scan(&w.ID, &w.WorkspaceID, &w.Name, &w.CurrentVersionID, &w.CreatedAt)
	return w, fmtErr("workflow", err)
}

func (s *Store) ListWorkflows(ctx context.Context) ([]model.WorkflowWithVersion, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,name,COALESCE(current_version_id,''),created_at FROM workflows ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.WorkflowWithVersion{}
	for rows.Next() {
		var w model.Workflow
		if err := rows.Scan(&w.ID, &w.WorkspaceID, &w.Name, &w.CurrentVersionID, &w.CreatedAt); err != nil {
			return nil, err
		}
		ww := model.WorkflowWithVersion{Workflow: w}
		if w.CurrentVersionID != "" {
			v, err := s.GetWorkflowVersion(ctx, w.CurrentVersionID)
			if err == nil {
				ww.Version = &v
			}
		}
		out = append(out, ww)
	}
	return out, rows.Err()
}

func (s *Store) SetWorkflowCurrentVersion(ctx context.Context, workflowID, versionID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE workflows SET current_version_id = ? WHERE id = ?`, versionID, workflowID)
	return err
}

func (s *Store) ApproveWorkflowVersion(ctx context.Context, workflowID, versionID string) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE workflow_versions SET status='approved' WHERE id=?`, versionID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE workflows SET current_version_id=? WHERE id=?`, versionID, workflowID)
		return err
	})
}

func (s *Store) DeleteWorkflow(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM workflows WHERE id = ?`, id)
	return err
}

// ResolveWorkflow returns the pinned version when version > 0, otherwise the
// workflow's current approved version, falling back to the latest version.
func (s *Store) ResolveWorkflow(ctx context.Context, name string, version int) (model.Workflow, model.WorkflowVersion, error) {
	w, err := s.GetWorkflowByName(ctx, name)
	if err != nil {
		return w, model.WorkflowVersion{}, err
	}
	var v model.WorkflowVersion
	if version > 0 {
		var id string
		err := s.db.QueryRowContext(ctx, `SELECT id FROM workflow_versions WHERE workflow_id=? AND version=?`, w.ID, version).Scan(&id)
		if err != nil {
			return w, v, fmtErr("workflow version", err)
		}
		v, err = s.GetWorkflowVersion(ctx, id)
		return w, v, err
	}
	if w.CurrentVersionID != "" {
		v, err = s.GetWorkflowVersion(ctx, w.CurrentVersionID)
		if err == nil && v.Status == "approved" {
			return w, v, nil
		}
	}
	err = s.db.QueryRowContext(ctx, `SELECT id FROM workflow_versions WHERE workflow_id=? AND status='approved' ORDER BY version DESC LIMIT 1`, w.ID).Scan(&v.ID)
	if err != nil {
		// Allow draft execution from the editor.
		if isNoRows(err) {
			err = s.db.QueryRowContext(ctx, `SELECT id FROM workflow_versions WHERE workflow_id=? ORDER BY version DESC LIMIT 1`, w.ID).Scan(&v.ID)
			if err == nil {
				v, err = s.GetWorkflowVersion(ctx, v.ID)
				return w, v, err
			}
		}
		return w, v, fmtErr("approved workflow version", err)
	}
	v, err = s.GetWorkflowVersion(ctx, v.ID)
	return w, v, err
}

// ---- triggers ----

func (s *Store) CreateTrigger(ctx context.Context, t model.Trigger) (model.Trigger, error) {
	if t.ID == "" {
		t.ID = ids.New()
	}
	if t.CreatedAt == 0 {
		t.CreatedAt = ids.NowMS()
	}
	if len(t.Config) == 0 {
		t.Config = json.RawMessage(`{}`)
	}
	if len(t.Inputs) == 0 {
		t.Inputs = json.RawMessage(`{}`)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO triggers (id,workflow_id,kind,config,inputs,enabled,unattended,concurrency,last_fired_at,approved_at,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.WorkflowID, t.Kind, string(t.Config), string(t.Inputs), boolInt(t.Enabled), t.Unattended, t.Concurrency,
		nullInt(t.LastFiredAt), nullInt(t.ApprovedAt), t.CreatedAt)
	return t, err
}

func (s *Store) ListTriggers(ctx context.Context, workflowID string) ([]model.Trigger, error) {
	query := `SELECT id,workflow_id,kind,config,inputs,enabled,unattended,concurrency,COALESCE(last_fired_at,0),COALESCE(approved_at,0),created_at FROM triggers`
	args := []any{}
	if workflowID != "" {
		query += ` WHERE workflow_id = ?`
		args = append(args, workflowID)
	}
	query += ` ORDER BY created_at`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Trigger{}
	for rows.Next() {
		var t model.Trigger
		var config, inputs string
		var enabled int
		if err := rows.Scan(&t.ID, &t.WorkflowID, &t.Kind, &config, &inputs, &enabled, &t.Unattended, &t.Concurrency, &t.LastFiredAt, &t.ApprovedAt, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.Config = json.RawMessage(mustJSON(config))
		t.Inputs = json.RawMessage(mustJSON(inputs))
		t.Enabled = enabled == 1
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) GetTrigger(ctx context.Context, id string) (model.Trigger, error) {
	var t model.Trigger
	var config, inputs string
	var enabled int
	err := s.db.QueryRowContext(ctx, `SELECT id,workflow_id,kind,config,inputs,enabled,unattended,concurrency,COALESCE(last_fired_at,0),COALESCE(approved_at,0),created_at FROM triggers WHERE id=?`, id).
		Scan(&t.ID, &t.WorkflowID, &t.Kind, &config, &inputs, &enabled, &t.Unattended, &t.Concurrency, &t.LastFiredAt, &t.ApprovedAt, &t.CreatedAt)
	t.Config = json.RawMessage(mustJSON(config))
	t.Inputs = json.RawMessage(mustJSON(inputs))
	t.Enabled = enabled == 1
	return t, fmtErr("trigger", err)
}

func (s *Store) UpdateTrigger(ctx context.Context, t model.Trigger) error {
	_, err := s.db.ExecContext(ctx, `UPDATE triggers SET kind=?,config=?,inputs=?,enabled=?,unattended=?,concurrency=?,last_fired_at=?,approved_at=? WHERE id=?`,
		t.Kind, string(t.Config), string(t.Inputs), boolInt(t.Enabled), t.Unattended, t.Concurrency, nullInt(t.LastFiredAt), nullInt(t.ApprovedAt), t.ID)
	return err
}

func (s *Store) DeleteTrigger(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM triggers WHERE id=?`, id)
	return err
}
