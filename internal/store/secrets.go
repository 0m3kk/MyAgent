package store

import (
	"context"

	"myagent/internal/ids"
	"myagent/internal/model"
)

func (s *Store) ListSecrets(ctx context.Context) ([]model.SecretMeta, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,kind,description,COALESCE(expires_at,0),COALESCE(last_used_at,0),ask_each_run,created_at
		FROM secrets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.SecretMeta{}
	for rows.Next() {
		var m model.SecretMeta
		var ask int
		if err := rows.Scan(&m.ID, &m.Name, &m.Kind, &m.Description, &m.ExpiresAt, &m.LastUsedAt, &ask, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.AskEachRun = ask == 1
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) GetSecret(ctx context.Context, id string) (model.Secret, error) {
	var sec model.Secret
	var ask int
	err := s.db.QueryRowContext(ctx, `SELECT id,name,kind,description,COALESCE(expires_at,0),COALESCE(last_used_at,0),ask_each_run,created_at,ciphertext,nonce,key_version
		FROM secrets WHERE id=?`, id).
		Scan(&sec.ID, &sec.Name, &sec.Kind, &sec.Description, &sec.ExpiresAt, &sec.LastUsedAt, &ask, &sec.CreatedAt, &sec.Ciphertext, &sec.Nonce, &sec.KeyVersion)
	sec.AskEachRun = ask == 1
	return sec, fmtErr("secret", err)
}

func (s *Store) GetSecretByName(ctx context.Context, name string) (model.Secret, error) {
	var sec model.Secret
	var ask int
	err := s.db.QueryRowContext(ctx, `SELECT id,name,kind,description,COALESCE(expires_at,0),COALESCE(last_used_at,0),ask_each_run,created_at,ciphertext,nonce,key_version
		FROM secrets WHERE name=?`, name).
		Scan(&sec.ID, &sec.Name, &sec.Kind, &sec.Description, &sec.ExpiresAt, &sec.LastUsedAt, &ask, &sec.CreatedAt, &sec.Ciphertext, &sec.Nonce, &sec.KeyVersion)
	sec.AskEachRun = ask == 1
	return sec, fmtErr("secret", err)
}

func (s *Store) UpsertSecret(ctx context.Context, sec model.Secret) (model.Secret, error) {
	if sec.ID == "" {
		sec.ID = ids.New()
	}
	if sec.CreatedAt == 0 {
		sec.CreatedAt = ids.NowMS()
	}
	if sec.KeyVersion == 0 {
		sec.KeyVersion = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO secrets (id,name,kind,description,expires_at,last_used_at,ciphertext,nonce,key_version,ask_each_run,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, kind=excluded.kind, description=excluded.description,
			expires_at=excluded.expires_at, ciphertext=excluded.ciphertext, nonce=excluded.nonce,
			key_version=excluded.key_version, ask_each_run=excluded.ask_each_run`,
		sec.ID, sec.Name, sec.Kind, sec.Description, nullInt(sec.ExpiresAt), nullInt(sec.LastUsedAt),
		sec.Ciphertext, sec.Nonce, sec.KeyVersion, boolInt(sec.AskEachRun), sec.CreatedAt)
	if err != nil {
		return sec, err
	}
	return sec, nil
}

func (s *Store) DeleteSecret(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM secrets WHERE id=?`, id)
	return err
}

func (s *Store) TouchSecret(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE secrets SET last_used_at=? WHERE id=?`, ids.NowMS(), id)
	return err
}

func (s *Store) LogSecretAccess(ctx context.Context, secretID, runID, toolVersionID string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO secret_access_log (id,secret_id,run_id,tool_version_id,ts) VALUES (?,?,?,?,?)`,
		ids.New(), secretID, nullString(runID), nullString(toolVersionID), ids.NowMS())
	return err
}

func (s *Store) SecretUsedBy(ctx context.Context, secretID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT t.name FROM tool_secret_bindings b JOIN tools t ON t.id=b.tool_id WHERE b.secret_id=? ORDER BY t.name`, secretID)
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

func (s *Store) AllSecretRecords(ctx context.Context) ([]model.Secret, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,kind,description,COALESCE(expires_at,0),COALESCE(last_used_at,0),ask_each_run,created_at,ciphertext,nonce,key_version FROM secrets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Secret{}
	for rows.Next() {
		var sec model.Secret
		var ask int
		if err := rows.Scan(&sec.ID, &sec.Name, &sec.Kind, &sec.Description, &sec.ExpiresAt, &sec.LastUsedAt, &ask, &sec.CreatedAt, &sec.Ciphertext, &sec.Nonce, &sec.KeyVersion); err != nil {
			return nil, err
		}
		sec.AskEachRun = ask == 1
		out = append(out, sec)
	}
	return out, rows.Err()
}
