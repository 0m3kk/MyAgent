package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db   *sql.DB
	path string
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn := "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(ON)" +
		"&_pragma=busy_timeout(5000)" +
		"&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(0)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db, path: path}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Path() string { return s.path }
func (s *Store) DB() *sql.DB  { return s.db }

func (s *Store) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schemaSQL); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "tools", "draft_version_id", "TEXT"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "tools", "folder_id", "TEXT"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "tools", "pinned", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "workflows", "folder_id", "TEXT"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "workflows", "pinned", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.dropColumn(ctx, "model_profiles", "role"); err != nil {
		return err
	}
	if err := s.linkLegacyToolDrafts(ctx); err != nil {
		return err
	}
	if err := s.deleteLegacyBuiltinTools(ctx); err != nil {
		return err
	}
	return s.deprecateOrphanToolDrafts(ctx)
}

// deleteLegacyBuiltinTools removes tool rows from the retired built-in tool
// model. Tools are now authored by the agent and use the workspace-scoped
// runtime modules instead of product-seeded built-ins.
func (s *Store) deleteLegacyBuiltinTools(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tools WHERE kind = 'builtin'`)
	return err
}

func (s *Store) linkLegacyToolDrafts(ctx context.Context) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE tools SET draft_version_id = (
		   SELECT tv.id FROM tool_versions tv
		   WHERE tv.tool_id = tools.id AND tv.status IN ('draft','tested')
		   ORDER BY tv.version DESC LIMIT 1
		 )
		 WHERE draft_version_id IS NULL
		   AND EXISTS (
		     SELECT 1 FROM tool_versions tv
		     WHERE tv.tool_id = tools.id AND tv.status IN ('draft','tested')
		   )
		   AND (
		     current_version_id IS NULL OR current_version_id = '' OR
		     (SELECT version FROM tool_versions WHERE id = tools.current_version_id) <
		     (SELECT MAX(version) FROM tool_versions
		      WHERE tool_id = tools.id AND status IN ('draft','tested'))
		   )`,
	)
	return err
}

func (s *Store) deprecateOrphanToolDrafts(ctx context.Context) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE tool_versions SET status='deprecated'
		 WHERE status IN ('draft','tested')
		   AND id NOT IN (
		     SELECT draft_version_id FROM tools
		     WHERE draft_version_id IS NOT NULL AND draft_version_id <> ''
		   )`,
	)
	return err
}

func (s *Store) ensureColumn(ctx context.Context, table, column, decl string) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column+" "+decl)
	return err
}

func (s *Store) dropColumn(ctx context.Context, table, column string) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !found {
		return nil
	}
	_, err = s.db.ExecContext(ctx, "ALTER TABLE "+table+" DROP COLUMN "+column)
	return err
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func nullInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func joinIDs(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "/")
}

func mustJSON(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "{}"
	}
	return raw
}

func fmtErr(context string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s: %w", context, ErrNotFound)
	}
	return fmt.Errorf("%s: %w", context, err)
}
