package workspaces

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"myagent/internal/ids"
	"myagent/internal/store"
)

const (
	archiveFormat   = "myagent-workspace/v1"
	maxArchiveBytes = int64(2 << 30) // 2 GiB of extracted data
	archiveManifest = "workspace.json"
	archiveDBPath   = "data/myagent.db"
	archiveDataDir  = "data"
)

type Manifest struct {
	Format     string `json:"format"`
	Name       string `json:"name"`
	RootPath   string `json:"root_path"`
	CreatedAt  int64  `json:"created_at"`
	ExportedAt int64  `json:"exported_at"`
}

// Export writes a portable workspace archive. Secrets are never included: the
// copied database has its secret rows removed and model profiles unlinked.
func Export(w io.Writer, p Profile) error {
	dbPath := filepath.Join(p.DataDir, "myagent.db")
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("workspace database: %w", err)
	}

	tmp, err := os.MkdirTemp("", "myagent-export-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	exportDB := filepath.Join(tmp, "myagent.db")
	if err := copyDatabase(dbPath, exportDB); err != nil {
		return err
	}

	zw := zip.NewWriter(w)

	manifest := Manifest{
		Format:     archiveFormat,
		Name:       p.Name,
		RootPath:   p.RootPath,
		CreatedAt:  p.CreatedAt,
		ExportedAt: time.Now().UnixMilli(),
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := addBytes(zw, archiveManifest, append(raw, '\n'), 0o644, time.Now()); err != nil {
		return err
	}
	if err := addFile(zw, exportDB, archiveDBPath, 0o600); err != nil {
		return err
	}
	if err := addTree(
		zw,
		filepath.Join(p.DataDir, "artifacts"),
		archiveDataDir+"/artifacts",
	); err != nil {
		return err
	}
	if err := addTree(zw, filepath.Join(p.DataDir, "tools"), archiveDataDir+"/tools"); err != nil {
		return err
	}
	return zw.Close()
}

// Import unpacks a workspace archive into a new profile. rootOverride, when
// set, relinks the workspace to that folder instead of the manifest path.
func Import(archivePath, home, rootOverride string) (Profile, error) {
	if home == "" {
		return Profile{}, ErrNotConfigured
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return Profile{}, err
	}
	tmp, err := os.MkdirTemp(home, ".import-")
	if err != nil {
		return Profile{}, err
	}
	defer os.RemoveAll(tmp)

	if err := extractArchive(archivePath, tmp); err != nil {
		return Profile{}, err
	}
	raw, err := os.ReadFile(filepath.Join(tmp, archiveManifest))
	if err != nil {
		return Profile{}, errors.New("archive is missing workspace.json")
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Profile{}, fmt.Errorf("invalid workspace.json: %w", err)
	}
	if manifest.Format != archiveFormat {
		return Profile{}, fmt.Errorf("unsupported workspace format %q", manifest.Format)
	}

	dbPath := filepath.Join(tmp, filepath.FromSlash(archiveDBPath))
	if _, err := os.Stat(dbPath); err != nil {
		return Profile{}, errors.New("archive is missing data/myagent.db")
	}
	if err := validateDatabase(dbPath); err != nil {
		return Profile{}, err
	}
	if err := stripSecrets(dbPath); err != nil {
		return Profile{}, err
	}

	id := ids.New()
	profileDir := filepath.Join(home, "profiles", id)
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return Profile{}, err
	}
	// Imports keep their files inside the new profile unless the caller
	// explicitly asks to relink to a folder on this machine. The manifest root
	// belongs to the machine that exported the archive, so it is never reused.
	root := strings.TrimSpace(rootOverride)
	if root == "" {
		root = privateRoot(profileDir)
	} else {
		root, err = expandPath(home, root)
		if err != nil {
			return Profile{}, err
		}
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Profile{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(profileDir)
		}
	}()

	dataDir := filepath.Join(tmp, archiveDataDir)
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return Profile{}, err
	}
	for _, entry := range entries {
		src := filepath.Join(dataDir, entry.Name())
		dst := filepath.Join(profileDir, entry.Name())
		if err := os.Rename(src, dst); err != nil {
			return Profile{}, err
		}
	}
	if err := os.Chmod(filepath.Join(profileDir, "myagent.db"), 0o600); err != nil {
		return Profile{}, err
	}

	reg, err := Open(home)
	if err != nil {
		return Profile{}, err
	}
	p := Profile{
		ID:           id,
		Name:         manifest.Name,
		RootPath:     root,
		DataDir:      profileDir,
		CreatedAt:    ids.NowMS(),
		LastOpenedAt: 0,
	}
	p, err = reg.AddImported(p)
	if err != nil {
		return Profile{}, err
	}
	cleanup = false
	return p, nil
}

func ArchiveFilename(p Profile) string {
	name := slug(p.Name)
	if name == "" {
		name = "workspace"
	}
	return fmt.Sprintf("MyAgent-%s-%s.myagentws", name, time.Now().Format("2006-01-02"))
}

func copyDatabase(src, dst string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := sql.Open("sqlite", "file:"+src+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "VACUUM INTO "+sqlString(dst)); err != nil {
		return fmt.Errorf("copy database: %w", err)
	}
	if err := stripSecrets(dst); err != nil {
		return err
	}
	return nil
}

func validateDatabase(path string) error {
	st, err := store.Open(path)
	if err != nil {
		return fmt.Errorf("open workspace database: %w", err)
	}
	var check string
	err = st.DB().QueryRow("PRAGMA quick_check").Scan(&check)
	if err == nil && check != "ok" {
		err = fmt.Errorf("database check failed: %s", check)
	}
	_ = st.Close()
	return err
}

func stripSecrets(dbPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := sql.Open(
		"sqlite",
		"file:"+dbPath+"?_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)",
	)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, q := range []string{
		`DELETE FROM secret_access_log`,
		`DELETE FROM secrets`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, _ = db.ExecContext(ctx, `VACUUM`)
	_, _ = db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return nil
}

func extractArchive(path, dest string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer zr.Close()
	var total int64
	for _, f := range zr.File {
		name := filepath.Clean(filepath.FromSlash(f.Name))
		if name == "." || filepath.IsAbs(name) || name == ".." ||
			strings.HasPrefix(name, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("invalid archive path %q", f.Name)
		}
		target := filepath.Join(dest, name)
		if within, err := pathWithin(dest, target); err != nil || !within {
			return fmt.Errorf("invalid archive path %q", f.Name)
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive contains a symlink: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		if f.UncompressedSize64 > uint64(maxArchiveBytes-total) {
			return errors.New("workspace archive is too large")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			rc.Close()
			return err
		}
		n, copyErr := io.Copy(dst, io.LimitReader(rc, maxArchiveBytes-total+1))
		closeErr := dst.Close()
		rc.Close()
		total += n
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if total > maxArchiveBytes {
			return errors.New("workspace archive is too large")
		}
	}
	return nil
}

func addTree(zw *zip.Writer, root, prefix string) error {
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := prefix + "/" + filepath.ToSlash(rel)
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			h := &zip.FileHeader{Name: name + "/", Method: zip.Store}
			h.SetMode(os.ModeDir | 0o700)
			h.Modified = time.Now()
			_, err := zw.CreateHeader(h)
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return addFile(zw, path, name, info.Mode().Perm())
	})
}

func addFile(zw *zip.Writer, path, name string, mode os.FileMode) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	h, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	h.Name = name
	h.Method = zip.Deflate
	if mode != 0 {
		h.SetMode(mode)
	}
	w, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

func addBytes(zw *zip.Writer, name string, data []byte, mode os.FileMode, modTime time.Time) error {
	h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: modTime}
	h.SetMode(mode)
	w, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func sqlString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
