package store

import (
	"context"
	"database/sql"

	"myagent/internal/ids"
	"myagent/internal/model"
)

func (s *Store) GetOrCreateTool(
	ctx context.Context,
	workspaceID, name, kind string,
) (model.Tool, error) {
	var t model.Tool
	var pinned int
	err := s.db.QueryRowContext(ctx, `SELECT id,workspace_id,name,kind,COALESCE(current_version_id,''),COALESCE(draft_version_id,''),COALESCE(folder_id,''),pinned,created_at FROM tools WHERE workspace_id=? AND name=?`,
		workspaceID, name).
		Scan(&t.ID, &t.WorkspaceID, &t.Name, &t.Kind, &t.CurrentVersionID, &t.DraftVersionID, &t.FolderID, &pinned, &t.CreatedAt)
	t.Pinned = pinned == 1
	if err == nil {
		return t, nil
	}
	if !isNoRows(err) {
		return t, err
	}
	t = model.Tool{
		ID:          ids.New(),
		WorkspaceID: workspaceID,
		Name:        name,
		Kind:        kind,
		CreatedAt:   ids.NowMS(),
	}
	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO tools (id,workspace_id,name,kind,current_version_id,folder_id,pinned,created_at) VALUES (?,?,?,?,NULL,NULL,0,?)`,
		t.ID,
		t.WorkspaceID,
		t.Name,
		t.Kind,
		t.CreatedAt,
	)
	return t, err
}

func (s *Store) AddToolVersion(
	ctx context.Context,
	v model.ToolVersion,
) (model.ToolVersion, error) {
	if v.ID == "" {
		v.ID = ids.New()
	}
	if v.CreatedAt == 0 {
		v.CreatedAt = ids.NowMS()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM tool_versions WHERE tool_id=?`, v.ToolID).
		Scan(&v.Version); err != nil {
		_ = tx.Rollback()
		return v, err
	}
	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO tool_versions (id,tool_id,version,manifest,code_path,content_hash,status,created_by,parent_id,approved_at,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		v.ID,
		v.ToolID,
		v.Version,
		v.Manifest,
		nullString(v.CodePath),
		v.ContentHash,
		v.Status,
		v.CreatedBy,
		nullString(v.ParentID),
		nullInt(v.ApprovedAt),
		v.CreatedAt,
	)
	if err == nil {
		switch v.Status {
		case model.ToolDraft, model.ToolTested:
			_, err = tx.ExecContext(
				ctx,
				`UPDATE tools SET draft_version_id = ? WHERE id = ?`,
				v.ID,
				v.ToolID,
			)
		case model.ToolActive, model.ToolApproved:
			_, err = tx.ExecContext(
				ctx,
				`UPDATE tools SET current_version_id = ?, draft_version_id = NULL WHERE id = ?`,
				v.ID,
				v.ToolID,
			)
		}
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

func (s *Store) GetToolVersion(ctx context.Context, id string) (model.ToolVersion, error) {
	var v model.ToolVersion
	var codePath, parentID sql.NullString
	var approved sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id,tool_id,version,manifest,code_path,content_hash,status,created_by,parent_id,approved_at,created_at
		FROM tool_versions WHERE id = ?`, id).
		Scan(&v.ID, &v.ToolID, &v.Version, &v.Manifest, &codePath, &v.ContentHash, &v.Status, &v.CreatedBy, &parentID, &approved, &v.CreatedAt)
	v.CodePath = codePath.String
	v.ParentID = parentID.String
	v.ApprovedAt = approved.Int64
	return v, fmtErr("tool version", err)
}

func (s *Store) GetToolVersionByNumber(
	ctx context.Context,
	toolID string,
	version int,
) (model.ToolVersion, error) {
	var id string
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM tool_versions WHERE tool_id=? AND version=?`, toolID, version).
		Scan(&id); err != nil {
		return model.ToolVersion{}, fmtErr("tool version", err)
	}
	return s.GetToolVersion(ctx, id)
}

func (s *Store) ToolVersions(ctx context.Context, toolID string) ([]model.ToolVersion, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id,tool_id,version,manifest,COALESCE(code_path,''),content_hash,status,created_by,COALESCE(parent_id,''),COALESCE(approved_at,0),created_at
		FROM tool_versions
		WHERE tool_id = ?
		  AND (status <> 'deprecated' OR id = (
		    SELECT current_version_id FROM tools WHERE id = ?
		  ))
		ORDER BY version DESC`,
		toolID,
		toolID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ToolVersion{}
	for rows.Next() {
		var v model.ToolVersion
		if err := rows.Scan(
			&v.ID,
			&v.ToolID,
			&v.Version,
			&v.Manifest,
			&v.CodePath,
			&v.ContentHash,
			&v.Status,
			&v.CreatedBy,
			&v.ParentID,
			&v.ApprovedAt,
			&v.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpdateToolVersionStatus(
	ctx context.Context,
	id, status string,
	approvedAt int64,
) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE tool_versions SET status = ?, approved_at = ? WHERE id = ?`,
		status,
		nullInt(approvedAt),
		id,
	)
	return err
}

func (s *Store) UpdateToolVersionHash(ctx context.Context, id, hash string) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE tool_versions SET content_hash = ? WHERE id = ?`,
		hash,
		id,
	)
	return err
}

func (s *Store) UpdateToolVersionManifest(ctx context.Context, id, manifest, hash string) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE tool_versions SET manifest = ?, content_hash = ? WHERE id = ?`,
		manifest,
		hash,
		id,
	)
	return err
}

func (s *Store) UpdateToolDraft(
	ctx context.Context,
	id, manifest, hash string,
) (model.ToolVersion, error) {
	res, err := s.db.ExecContext(
		ctx,
		`UPDATE tool_versions
		 SET manifest = ?, content_hash = ?, status = 'draft', approved_at = NULL
		 WHERE id = ? AND status IN ('draft','tested')`,
		manifest,
		hash,
		id,
	)
	if err != nil {
		return model.ToolVersion{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return model.ToolVersion{}, err
	}
	if affected == 0 {
		return model.ToolVersion{}, ErrNotFound
	}
	return s.GetToolVersion(ctx, id)
}

func (s *Store) SetToolCurrentVersion(ctx context.Context, toolID, versionID string) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE tools
		 SET current_version_id = ?,
		     draft_version_id = CASE WHEN draft_version_id = ? THEN NULL ELSE draft_version_id END
		 WHERE id = ?`,
		versionID,
		versionID,
		toolID,
	)
	return err
}

func (s *Store) GetToolByID(ctx context.Context, id string) (model.Tool, error) {
	var t model.Tool
	var pinned int
	err := s.db.QueryRowContext(ctx, `SELECT id,workspace_id,name,kind,COALESCE(current_version_id,''),COALESCE(draft_version_id,''),COALESCE(folder_id,''),pinned,created_at FROM tools WHERE id = ?`, id).
		Scan(&t.ID, &t.WorkspaceID, &t.Name, &t.Kind, &t.CurrentVersionID, &t.DraftVersionID, &t.FolderID, &pinned, &t.CreatedAt)
	t.Pinned = pinned == 1
	return t, fmtErr("tool", err)
}

func (s *Store) GetToolByName(ctx context.Context, name string) (model.Tool, error) {
	var t model.Tool
	var pinned int
	err := s.db.QueryRowContext(ctx, `SELECT id,workspace_id,name,kind,COALESCE(current_version_id,''),COALESCE(draft_version_id,''),COALESCE(folder_id,''),pinned,created_at FROM tools WHERE name = ? ORDER BY created_at LIMIT 1`, name).
		Scan(&t.ID, &t.WorkspaceID, &t.Name, &t.Kind, &t.CurrentVersionID, &t.DraftVersionID, &t.FolderID, &pinned, &t.CreatedAt)
	t.Pinned = pinned == 1
	return t, fmtErr("tool", err)
}

func (s *Store) ListTools(
	ctx context.Context,
	filter model.ToolFilter,
) ([]model.ToolWithVersion, error) {
	query := `SELECT t.id,t.workspace_id,t.name,t.kind,COALESCE(t.current_version_id,''),COALESCE(t.draft_version_id,''),COALESCE(t.folder_id,''),t.pinned,t.created_at
		FROM tools t WHERE 1=1`
	args := []any{}
	if filter.Kind != "" {
		query += ` AND t.kind = ?`
		args = append(args, filter.Kind)
	}
	if filter.Query != "" {
		query += ` AND t.name LIKE ?`
		args = append(args, "%"+filter.Query+"%")
	}
	query += ` ORDER BY t.name`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ToolWithVersion{}
	for rows.Next() {
		var t model.Tool
		var pinned int
		if err := rows.Scan(
			&t.ID,
			&t.WorkspaceID,
			&t.Name,
			&t.Kind,
			&t.CurrentVersionID,
			&t.DraftVersionID,
			&t.FolderID,
			&pinned,
			&t.CreatedAt,
		); err != nil {
			return nil, err
		}
		t.Pinned = pinned == 1
		tw := model.ToolWithVersion{Tool: t}
		if t.CurrentVersionID != "" {
			v, err := s.GetToolVersion(ctx, t.CurrentVersionID)
			if err == nil {
				tw.Version = &v
			}
		}
		if tw.Version == nil {
			var vid string
			if err := s.db.QueryRowContext(ctx, `SELECT id FROM tool_versions WHERE tool_id=? ORDER BY version DESC LIMIT 1`, t.ID).
				Scan(&vid); err == nil {
				if v, err := s.GetToolVersion(ctx, vid); err == nil {
					tw.Version = &v
				}
			}
		}
		if filter.Status != "" && (tw.Version == nil || tw.Version.Status != filter.Status) {
			continue
		}
		out = append(out, tw)
	}
	return out, rows.Err()
}

func (s *Store) ListActiveTools(ctx context.Context) ([]model.ToolWithVersion, error) {
	all, err := s.ListTools(ctx, model.ToolFilter{})
	if err != nil {
		return nil, err
	}
	out := []model.ToolWithVersion{}
	for _, t := range all {
		if t.Version != nil &&
			(t.Version.Status == model.ToolActive || t.Version.Status == model.ToolApproved) {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *Store) CountToolVersions(ctx context.Context, toolID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tool_versions WHERE tool_id = ?`, toolID).
		Scan(&n)
	return n, err
}

func (s *Store) SetSecretBinding(ctx context.Context, b model.SecretBinding) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO tool_secret_bindings (tool_id,logical_name,secret_id,scope_hash,approved_at)
		VALUES (?,?,?,?,?)
		ON CONFLICT(tool_id,logical_name) DO UPDATE SET secret_id=excluded.secret_id,
			scope_hash=excluded.scope_hash, approved_at=excluded.approved_at`,
		b.ToolID,
		b.LogicalName,
		b.SecretID,
		b.ScopeHash,
		nullInt(b.ApprovedAt),
	)
	return err
}

func (s *Store) SecretBindings(ctx context.Context, toolID string) ([]model.SecretBinding, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT tool_id,logical_name,secret_id,scope_hash,COALESCE(approved_at,0)
		FROM tool_secret_bindings WHERE tool_id=?`,
		toolID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.SecretBinding{}
	for rows.Next() {
		var b model.SecretBinding
		if err := rows.Scan(
			&b.ToolID,
			&b.LogicalName,
			&b.SecretID,
			&b.ScopeHash,
			&b.ApprovedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) DeleteTool(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tools WHERE id = ?`, id)
	return err
}

// toolSummary is used by workflow validation to decide whether a version is runnable.
func (s *Store) ToolVersionStatus(ctx context.Context, toolID string) (string, error) {
	var status string
	err := s.db.QueryRowContext(ctx, `SELECT tv.status FROM tools t JOIN tool_versions tv ON tv.id=t.current_version_id WHERE t.id=?`, toolID).
		Scan(&status)
	return status, err
}
