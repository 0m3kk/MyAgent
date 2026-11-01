package store

import (
	"context"
	"database/sql"
	"strings"

	"myagent/internal/ids"
	"myagent/internal/model"
)

func (s *Store) ListFolders(ctx context.Context, kind string) ([]model.Folder, error) {
	query := `SELECT id,workspace_id,kind,name,COALESCE(parent_id,''),created_at FROM folders`
	args := []any{}
	if kind != "" {
		query += ` WHERE kind = ?`
		args = append(args, kind)
	}
	query += ` ORDER BY name COLLATE NOCASE, created_at`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Folder{}
	for rows.Next() {
		var f model.Folder
		if err := rows.Scan(
			&f.ID,
			&f.WorkspaceID,
			&f.Kind,
			&f.Name,
			&f.ParentID,
			&f.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) GetFolder(ctx context.Context, id string) (model.Folder, error) {
	var f model.Folder
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id,workspace_id,kind,name,COALESCE(parent_id,''),created_at FROM folders WHERE id = ?`,
		id,
	).Scan(&f.ID, &f.WorkspaceID, &f.Kind, &f.Name, &f.ParentID, &f.CreatedAt)
	return f, fmtErr("folder", err)
}

func (s *Store) CreateFolder(ctx context.Context, f model.Folder) (model.Folder, error) {
	if f.ID == "" {
		f.ID = ids.New()
	}
	if f.CreatedAt == 0 {
		f.CreatedAt = ids.NowMS()
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO folders (id,workspace_id,kind,name,parent_id,created_at) VALUES (?,?,?,?,?,?)`,
		f.ID,
		f.WorkspaceID,
		f.Kind,
		f.Name,
		nullString(f.ParentID),
		f.CreatedAt,
	)
	return f, err
}

func (s *Store) UpdateFolder(ctx context.Context, f model.Folder) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE folders SET name = ?, parent_id = ? WHERE id = ?`,
		f.Name,
		nullString(f.ParentID),
		f.ID,
	)
	return err
}

// FolderNameExists checks the visible tree for a duplicate sibling name. The
// database also has a unique expression index as a final guard.
func (s *Store) FolderNameExists(
	ctx context.Context,
	workspaceID, kind, parentID, name, excludeID string,
) (bool, error) {
	query := `SELECT 1 FROM folders
		WHERE workspace_id = ? AND kind = ? AND COALESCE(parent_id,'') = ?
		  AND name = ? COLLATE NOCASE`
	args := []any{workspaceID, kind, parentID, name}
	if excludeID != "" {
		query += ` AND id <> ?`
		args = append(args, excludeID)
	}
	var found int
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&found)
	if isNoRows(err) {
		return false, nil
	}
	return err == nil, err
}

// DeleteFolder removes a folder and its descendants. Items inside the deleted
// subtree move up to the deleted folder's parent so nothing is lost.
func (s *Store) DeleteFolder(ctx context.Context, id string) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		var parentID, kind string
		if err := tx.QueryRowContext(
			ctx,
			`SELECT COALESCE(parent_id,''), kind FROM folders WHERE id = ?`,
			id,
		).Scan(&parentID, &kind); err != nil {
			return fmtErr("folder", err)
		}
		rows, err := tx.QueryContext(
			ctx,
			`WITH RECURSIVE tree(id) AS (
			   SELECT id FROM folders WHERE id = ?
			   UNION
			   SELECT child.id FROM folders child JOIN tree parent ON child.parent_id = parent.id
			 )
			 SELECT id FROM tree`,
			id,
		)
		if err != nil {
			return err
		}
		folderIDs := []string{}
		for rows.Next() {
			var childID string
			if err := rows.Scan(&childID); err != nil {
				rows.Close()
				return err
			}
			folderIDs = append(folderIDs, childID)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(folderIDs) == 0 {
			return ErrNotFound
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(folderIDs)), ",")
		args := make([]any, 0, len(folderIDs)+1)
		args = append(args, nullString(parentID))
		for _, folderID := range folderIDs {
			args = append(args, folderID)
		}
		table := "tools"
		if kind == "workflow" {
			table = "workflows"
		}
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE `+table+` SET folder_id = ? WHERE folder_id IN (`+placeholders+`)`,
			args...,
		); err != nil {
			return err
		}
		deleteArgs := make([]any, 0, len(folderIDs))
		for _, folderID := range folderIDs {
			deleteArgs = append(deleteArgs, folderID)
		}
		_, err = tx.ExecContext(
			ctx,
			`DELETE FROM folders WHERE id IN (`+placeholders+`)`,
			deleteArgs...,
		)
		return err
	})
}

func (s *Store) SetToolOrganization(
	ctx context.Context,
	id, folderID string,
	pinned *bool,
) error {
	if pinned == nil {
		_, err := s.db.ExecContext(
			ctx,
			`UPDATE tools SET folder_id = ? WHERE id = ?`,
			nullString(folderID),
			id,
		)
		return err
	}
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE tools SET folder_id = ?, pinned = ? WHERE id = ?`,
		nullString(folderID),
		boolInt(*pinned),
		id,
	)
	return err
}

func (s *Store) SetWorkflowOrganization(
	ctx context.Context,
	id, folderID string,
	pinned *bool,
) error {
	if pinned == nil {
		_, err := s.db.ExecContext(
			ctx,
			`UPDATE workflows SET folder_id = ? WHERE id = ?`,
			nullString(folderID),
			id,
		)
		return err
	}
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE workflows SET folder_id = ?, pinned = ? WHERE id = ?`,
		nullString(folderID),
		boolInt(*pinned),
		id,
	)
	return err
}
