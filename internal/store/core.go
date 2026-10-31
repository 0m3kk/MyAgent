package store

import (
	"context"
	"encoding/json"

	"myagent/internal/ids"
	"myagent/internal/model"
)

func (s *Store) EnsureWorkspace(ctx context.Context, name, root string) (model.Workspace, error) {
	var w model.Workspace
	err := s.db.QueryRowContext(ctx, `SELECT id, name, root_path, created_at FROM workspaces ORDER BY created_at LIMIT 1`).
		Scan(&w.ID, &w.Name, &w.RootPath, &w.CreatedAt)
	if err == nil {
		// The configured root is authoritative; keep the stored row in sync.
		if w.RootPath != root {
			if _, err := s.db.ExecContext(ctx, `UPDATE workspaces SET root_path = ? WHERE id = ?`, root, w.ID); err != nil {
				return w, err
			}
			w.RootPath = root
		}
		return w, nil
	}
	if !isNoRows(err) {
		return w, err
	}
	w = model.Workspace{ID: ids.New(), Name: name, RootPath: root, CreatedAt: ids.NowMS()}
	_, err = s.db.ExecContext(ctx, `INSERT INTO workspaces (id,name,root_path,created_at) VALUES (?,?,?,?)`,
		w.ID, w.Name, w.RootPath, w.CreatedAt)
	return w, err
}

func (s *Store) GetWorkspace(ctx context.Context) (model.Workspace, error) {
	var w model.Workspace
	err := s.db.QueryRowContext(ctx, `SELECT id, name, root_path, created_at FROM workspaces ORDER BY created_at LIMIT 1`).
		Scan(&w.ID, &w.Name, &w.RootPath, &w.CreatedAt)
	return w, fmtErr("workspace", err)
}

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key = ?`, key).Scan(&v)
	if isNoRows(err) {
		return "", nil
	}
	return v, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO app_settings (key,value) VALUES (?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// ---- model profiles ----

func (s *Store) ListModelProfiles(ctx context.Context) ([]model.ModelProfile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,base_url,model,role,params,capabilities,COALESCE(secret_id,''),created_at,updated_at
		FROM model_profiles ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ModelProfile{}
	for rows.Next() {
		var p model.ModelProfile
		var params, caps string
		if err := rows.Scan(&p.ID, &p.Name, &p.BaseURL, &p.Model, &p.Role, &params, &caps, &p.SecretID, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Params = json.RawMessage(mustJSON(params))
		p.Capabilities = json.RawMessage(mustJSON(caps))
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetModelProfile(ctx context.Context, id string) (model.ModelProfile, error) {
	var p model.ModelProfile
	var params, caps string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,base_url,model,role,params,capabilities,COALESCE(secret_id,''),created_at,updated_at
		FROM model_profiles WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &p.BaseURL, &p.Model, &p.Role, &params, &caps, &p.SecretID, &p.CreatedAt, &p.UpdatedAt)
	p.Params = json.RawMessage(mustJSON(params))
	p.Capabilities = json.RawMessage(mustJSON(caps))
	return p, fmtErr("model profile", err)
}

func (s *Store) GetModelByRole(ctx context.Context, role string) (model.ModelProfile, error) {
	var p model.ModelProfile
	var params, caps string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,base_url,model,role,params,capabilities,COALESCE(secret_id,''),created_at,updated_at
		FROM model_profiles WHERE role = ? ORDER BY created_at LIMIT 1`, role).
		Scan(&p.ID, &p.Name, &p.BaseURL, &p.Model, &p.Role, &params, &caps, &p.SecretID, &p.CreatedAt, &p.UpdatedAt)
	p.Params = json.RawMessage(mustJSON(params))
	p.Capabilities = json.RawMessage(mustJSON(caps))
	return p, fmtErr("model profile", err)
}

func (s *Store) UpsertModelProfile(ctx context.Context, p model.ModelProfile) (model.ModelProfile, error) {
	now := ids.NowMS()
	if p.ID == "" {
		p.ID = ids.New()
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	if len(p.Params) == 0 {
		p.Params = json.RawMessage(`{}`)
	}
	if len(p.Capabilities) == 0 {
		p.Capabilities = json.RawMessage(`{}`)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO model_profiles (id,name,base_url,model,role,params,capabilities,secret_id,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, base_url=excluded.base_url, model=excluded.model,
			role=excluded.role, params=excluded.params, capabilities=excluded.capabilities,
			secret_id=excluded.secret_id, updated_at=excluded.updated_at`,
		p.ID, p.Name, p.BaseURL, p.Model, p.Role, string(p.Params), string(p.Capabilities), nullString(p.SecretID), p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return p, err
	}
	return p, nil
}

func (s *Store) DeleteModelProfile(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM model_profiles WHERE id = ?`, id)
	return err
}

// ---- conversations & messages ----

func (s *Store) CreateConversation(ctx context.Context, workspaceID, title string) (model.Conversation, error) {
	now := ids.NowMS()
	c := model.Conversation{ID: ids.New(), WorkspaceID: workspaceID, Title: title, CreatedAt: now, UpdatedAt: now}
	_, err := s.db.ExecContext(ctx, `INSERT INTO conversations (id,workspace_id,title,created_at,updated_at) VALUES (?,?,?,?,?)`,
		c.ID, c.WorkspaceID, c.Title, c.CreatedAt, c.UpdatedAt)
	return c, err
}

func (s *Store) ListConversations(ctx context.Context, limit int) ([]model.Conversation, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,title,created_at,updated_at FROM conversations ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Conversation{}
	for rows.Next() {
		var c model.Conversation
		if err := rows.Scan(&c.ID, &c.WorkspaceID, &c.Title, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetConversation(ctx context.Context, id string) (model.Conversation, error) {
	var c model.Conversation
	err := s.db.QueryRowContext(ctx, `SELECT id,workspace_id,title,created_at,updated_at FROM conversations WHERE id = ?`, id).
		Scan(&c.ID, &c.WorkspaceID, &c.Title, &c.CreatedAt, &c.UpdatedAt)
	return c, fmtErr("conversation", err)
}

func (s *Store) UpdateConversationTitle(ctx context.Context, id, title string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE conversations SET title = ?, updated_at = ? WHERE id = ?`, title, ids.NowMS(), id)
	return err
}

func (s *Store) DeleteConversation(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM conversations WHERE id = ?`, id)
	return err
}

func (s *Store) AppendMessage(ctx context.Context, m model.Message) (model.Message, error) {
	if m.ID == "" {
		m.ID = ids.New()
	}
	if m.CreatedAt == 0 {
		m.CreatedAt = ids.NowMS()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return m, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0)+1 FROM messages WHERE conversation_id = ?`, m.ConversationID).Scan(&m.Seq); err != nil {
		_ = tx.Rollback()
		return m, err
	}
	if len(m.Content) == 0 {
		m.Content = json.RawMessage(`{"text":""}`)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO messages (id,conversation_id,seq,role,content,model_profile_id,tokens_in,tokens_out,cost_micro,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.ConversationID, m.Seq, m.Role, string(m.Content), nullString(m.ModelProfileID), m.TokensIn, m.TokensOut, m.CostMicro, m.CreatedAt)
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE conversations SET updated_at = ? WHERE id = ?`, m.CreatedAt, m.ConversationID)
	}
	if err != nil {
		_ = tx.Rollback()
		return m, err
	}
	if err := tx.Commit(); err != nil {
		return m, err
	}
	return m, nil
}

func (s *Store) ListMessages(ctx context.Context, convID string) ([]model.Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,conversation_id,seq,role,content,COALESCE(model_profile_id,''),tokens_in,tokens_out,cost_micro,created_at
		FROM messages WHERE conversation_id = ? ORDER BY seq`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Message{}
	for rows.Next() {
		var m model.Message
		var content string
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Seq, &m.Role, &content, &m.ModelProfileID, &m.TokensIn, &m.TokensOut, &m.CostMicro, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.Content = json.RawMessage(mustJSON(content))
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) GetMessage(ctx context.Context, id string) (model.Message, error) {
	var m model.Message
	var content string
	err := s.db.QueryRowContext(ctx, `SELECT id,conversation_id,seq,role,content,COALESCE(model_profile_id,''),tokens_in,tokens_out,cost_micro,created_at
		FROM messages WHERE id = ?`, id).
		Scan(&m.ID, &m.ConversationID, &m.Seq, &m.Role, &content, &m.ModelProfileID, &m.TokensIn, &m.TokensOut, &m.CostMicro, &m.CreatedAt)
	m.Content = json.RawMessage(mustJSON(content))
	return m, fmtErr("message", err)
}

// LatestUserMessage helps the agent compact old conversations.
func (s *Store) LatestUserMessage(ctx context.Context, convID string) (model.Message, error) {
	var m model.Message
	var content string
	err := s.db.QueryRowContext(ctx, `SELECT id,conversation_id,seq,role,content,COALESCE(model_profile_id,''),tokens_in,tokens_out,cost_micro,created_at
		FROM messages WHERE conversation_id = ? AND role='user' ORDER BY seq DESC LIMIT 1`, convID).
		Scan(&m.ID, &m.ConversationID, &m.Seq, &m.Role, &content, &m.ModelProfileID, &m.TokensIn, &m.TokensOut, &m.CostMicro, &m.CreatedAt)
	m.Content = json.RawMessage(mustJSON(content))
	return m, fmtErr("user message", err)
}

func (s *Store) ConversationCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM conversations`).Scan(&n)
	return n, err
}

func (s *Store) TouchConversation(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE conversations SET updated_at = ? WHERE id = ?`, ids.NowMS(), id)
	return err
}
