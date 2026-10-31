package store

import (
	"context"
	"path/filepath"
	"strings"
)

// RebaseDataPaths rewrites absolute paths inside the database after a profile
// was moved, imported, or migrated to a new data directory. Tool code and
// artifacts live under the profile, but their database rows store absolute
// paths so they must follow the profile.
func (s *Store) RebaseDataPaths(ctx context.Context, toolsDir, artifactsDir string) error {
	if err := s.rebasePaths(ctx, "tool_versions", "code_path", "tools", toolsDir); err != nil {
		return err
	}
	return s.rebasePaths(ctx, "artifacts", "path", "artifacts", artifactsDir)
}

func (s *Store) rebasePaths(
	ctx context.Context,
	table, column, segment, newRoot string,
) error {
	rows, err := s.db.QueryContext(
		ctx,
		"SELECT id, "+column+" FROM "+table+" WHERE "+column+" IS NOT NULL AND "+column+" <> ''",
	)
	if err != nil {
		return err
	}
	type update struct {
		id, path string
	}
	var updates []update
	for rows.Next() {
		var u update
		if err := rows.Scan(&u.id, &u.path); err != nil {
			rows.Close()
			return err
		}
		suffix, ok := pathAfterSegment(u.path, segment)
		if !ok {
			continue
		}
		next := filepath.Join(newRoot, filepath.FromSlash(suffix))
		if filepath.Clean(next) != filepath.Clean(u.path) {
			updates = append(updates, update{id: u.id, path: next})
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, u := range updates {
		if _, err := s.db.ExecContext(
			ctx,
			"UPDATE "+table+" SET "+column+" = ? WHERE id = ?",
			u.path,
			u.id,
		); err != nil {
			return err
		}
	}
	return nil
}

func pathAfterSegment(path, segment string) (string, bool) {
	slashed := filepath.ToSlash(path)
	marker := "/" + segment + "/"
	if i := strings.LastIndex(slashed, marker); i >= 0 {
		return slashed[i+len(marker):], true
	}
	return "", false
}
