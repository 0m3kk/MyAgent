package workspaces

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"myagent/internal/store"
)

func TestRegistryLifecycle(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "projects", "alpha")
	reg, err := OpenOrCreate(home, filepath.Join(home, "profiles", "default"), root, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	if reg.Active() != "default" {
		t.Fatalf("active = %q", reg.Active())
	}

	work, err := reg.Create("Work", filepath.Join(home, "projects", "work"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create("work", ""); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
	renamed, err := reg.Update(work.ID, "Client", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "Client" {
		t.Fatalf("name = %q", renamed.Name)
	}
	if renamed.RootPath != filepath.Join(home, "projects", "work") {
		t.Fatalf("name-only update changed root to %q", renamed.RootPath)
	}
	if _, err := reg.Activate(work.ID); err != nil {
		t.Fatal(err)
	}
	if err := reg.Delete(work.ID); !errors.Is(err, ErrActive) {
		t.Fatalf("expected active delete error, got %v", err)
	}
	if err := reg.Delete("default"); err != nil {
		t.Fatal(err)
	}
	other, err := reg.Create("Other", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(other.DataDir, "workspace"); other.RootPath != want {
		t.Fatalf("private root = %q, want %q", other.RootPath, want)
	}
	if _, err := os.Stat(other.RootPath); err != nil {
		t.Fatalf("private root was not created: %v", err)
	}
	if _, err := reg.Activate(other.ID); err != nil {
		t.Fatal(err)
	}
	if err := reg.Delete(work.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(work.DataDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("profile dir still exists: %v", err)
	}
}

func TestOpenResetsLegacyProjectFolders(t *testing.T) {
	home := t.TempDir()
	dataDir := filepath.Join(home, "profiles", "p1")
	writeRegistry(t, home, registryFile{
		Version: 1,
		Active:  "p1",
		Workspaces: []Profile{{
			ID:       "p1",
			Name:     "Old",
			RootPath: filepath.Join(home, "source-checkout"),
			DataDir:  dataDir,
		}},
	})

	reg, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	p, err := reg.Get("p1")
	if err != nil {
		t.Fatal(err)
	}
	if want := privateRoot(dataDir); p.RootPath != want {
		t.Fatalf("root = %q, want %q", p.RootPath, want)
	}

	raw, err := os.ReadFile(filepath.Join(home, "workspaces.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved registryFile
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Version != registryVersion {
		t.Fatalf("saved version = %d, want %d", saved.Version, registryVersion)
	}
	if saved.Workspaces[0].RootPath != privateRoot(dataDir) {
		t.Fatalf("saved root = %q", saved.Workspaces[0].RootPath)
	}
}

func writeRegistry(t *testing.T, home string, f registryFile) {
	t.Helper()
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "workspaces.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestExportStripsSecretsAndImportCreatesProfile(t *testing.T) {
	home := t.TempDir()
	reg, err := OpenOrCreate(
		home,
		filepath.Join(home, "profiles", "default"),
		filepath.Join(home, "project"),
		"Personal",
	)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := reg.Get("default")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(profile.DataDir, "myagent.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(
		`INSERT INTO secrets (id,name,ciphertext,nonce,key_version,created_at) VALUES (?,?,?,?,?,?)`,
		"sec1", "OPENAI_KEY", []byte("ciphertext"), []byte("nonce"), 1, 1,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(
		`INSERT INTO model_profiles (id,name,base_url,model,params,capabilities,secret_id,created_at,updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		"model1", "Model", "https://example.com", "gpt", "{}", "{}", "sec1", 1, 1,
	); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := Export(&buf, profile); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var dbBytes []byte
	for _, f := range zr.File {
		if f.Name != archiveDBPath {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		dbBytes, err = io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(dbBytes) == 0 {
		t.Fatal("archive is missing data/myagent.db")
	}
	exported := filepath.Join(t.TempDir(), "exported.db")
	if err := os.WriteFile(exported, dbBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+exported)
	if err != nil {
		t.Fatal(err)
	}
	var secrets int
	if err := db.QueryRow(`SELECT COUNT(*) FROM secrets`).Scan(&secrets); err != nil {
		t.Fatal(err)
	}
	var secretID sql.NullString
	if err := db.QueryRow(`SELECT secret_id FROM model_profiles WHERE id='model1'`).
		Scan(&secretID); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if secrets != 0 || secretID.Valid {
		t.Fatalf("secrets=%d model.secret_id=%v", secrets, secretID)
	}

	archivePath := filepath.Join(t.TempDir(), "workspace.myagentws")
	if err := os.WriteFile(archivePath, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	imported, err := Import(archivePath, home, "")
	if err != nil {
		t.Fatal(err)
	}
	if imported.Name != "Personal (2)" {
		t.Fatalf("imported name = %q", imported.Name)
	}
	if want := filepath.Join(imported.DataDir, "workspace"); imported.RootPath != want {
		t.Fatalf("imported root = %q, want private %q", imported.RootPath, want)
	}
	if _, err := os.Stat(filepath.Join(imported.DataDir, "myagent.db")); err != nil {
		t.Fatal(err)
	}
	if err := reg.Reload(); err != nil {
		t.Fatal(err)
	}
	if len(reg.Profiles()) != 2 {
		t.Fatalf("profiles = %d", len(reg.Profiles()))
	}
}
