package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"myagent/internal/model"
)

func TestOpenDropsLegacyModelProfileRole(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")

	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE model_profiles (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		base_url TEXT NOT NULL,
		model TEXT NOT NULL,
		role TEXT NOT NULL,
		params TEXT NOT NULL DEFAULT '{}',
		capabilities TEXT NOT NULL DEFAULT '{}',
		secret_id TEXT,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(
		`INSERT INTO model_profiles (id,name,base_url,model,role,created_at,updated_at)
		 VALUES ('m1','Legacy','https://example.com/v1','old-model','planner',1,1)`,
	); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if columnExists(t, st.DB(), "model_profiles", "role") {
		t.Fatal("legacy role column was not dropped")
	}
	profiles, err := st.ListModelProfiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].Model != "old-model" {
		t.Fatalf("profiles = %+v", profiles)
	}
}

func TestUpsertModelProfileWithoutRole(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	profiles, err := st.ListModelProfiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 0 {
		t.Fatalf("profiles = %+v", profiles)
	}
	if columnExists(t, st.DB(), "model_profiles", "role") {
		t.Fatal("role column should not exist")
	}

	created, err := st.UpsertModelProfile(ctx, model.ModelProfile{
		Name: "Local", BaseURL: "http://127.0.0.1:1234/v1", Model: "local-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.GetModelProfile(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "local-model" {
		t.Fatalf("model = %q", got.Model)
	}
}

func columnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return false
}
