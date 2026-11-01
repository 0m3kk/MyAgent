package store

import (
	"context"
	"encoding/json"

	"myagent/internal/ids"
)

func (s *Store) AddNote(ctx context.Context, workspaceID, note string) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO workspace_notes (id,workspace_id,note,created_at) VALUES (?,?,?,?)`,
		ids.New(),
		workspaceID,
		note,
		ids.NowMS(),
	)
	return err
}

func (s *Store) Notes(ctx context.Context, workspaceID string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT note FROM workspace_notes WHERE workspace_id=? ORDER BY created_at DESC LIMIT ?`,
		workspaceID,
		limit,
	)
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

func (s *Store) UpsertPreset(
	ctx context.Context,
	kind, targetID, name string,
	inputs json.RawMessage,
) error {
	if len(inputs) == 0 {
		inputs = json.RawMessage(`{}`)
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO presets (id,target_kind,target_id,name,inputs,created_at) VALUES (?,?,?,?,?,?)
		ON CONFLICT(target_kind,target_id,name) DO UPDATE SET inputs=excluded.inputs, created_at=excluded.created_at`,
		ids.New(),
		kind,
		targetID,
		name,
		string(inputs),
		ids.NowMS(),
	)
	return err
}

func (s *Store) Presets(ctx context.Context, kind, targetID string) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id,name,inputs,created_at FROM presets WHERE target_kind=? AND target_id=? ORDER BY created_at DESC`,
		kind,
		targetID,
	)
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
		out = append(
			out,
			map[string]any{"id": id, "name": name, "inputs": val, "created_at": created},
		)
	}
	return out, rows.Err()
}
