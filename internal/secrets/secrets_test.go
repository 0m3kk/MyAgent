package secrets

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"myagent/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestSecretRoundTripAndRedaction(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	kp := &FileKeyProvider{Path: filepath.Join(t.TempDir(), "master.key")}
	svc, err := New(ctx, st, kp)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := svc.Put(ctx, "GITHUB_TOKEN", "token", "read only", "ghp_1234567890abcdef", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Status != "present" {
		t.Fatalf("status %q", meta.Status)
	}
	val, _, err := svc.Get(ctx, meta.ID)
	if err != nil || val != "ghp_1234567890abcdef" {
		t.Fatalf("get: %q %v", val, err)
	}
	red := svc.Redact("token=ghp_1234567890abcdef and again ghp_1234567890abcdef")
	if strings.Contains(red, "ghp_") {
		t.Fatalf("value was not redacted: %s", red)
	}
	if !strings.Contains(red, "[REDACTED:GITHUB_TOKEN]") {
		t.Fatalf("missing redaction marker: %s", red)
	}
	if err := svc.Delete(ctx, meta.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Get(ctx, meta.ID); err == nil {
		t.Fatal("expected missing secret")
	}
}

func TestCiphertextIsBoundToID(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	kp := &FileKeyProvider{Path: filepath.Join(t.TempDir(), "master.key")}
	svc, err := New(ctx, st, kp)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := svc.Put(ctx, "TOKEN", "token", "", "supersecretvalue", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := st.GetSecret(ctx, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Copy the ciphertext to a different row: AAD should make it undecryptable.
	rec.ID = "another-id"
	rec.Name = "OTHER"
	_, err = st.UpsertSecret(ctx, rec)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Get(ctx, "another-id"); err == nil {
		t.Fatal("ciphertext should not decrypt under another id")
	}
}

func TestDetectLikelySecret(t *testing.T) {
	if _, ok := DetectLikelySecret("please use sk-proj-1234567890abcdef"); !ok {
		t.Fatal("expected detection")
	}
	if _, ok := DetectLikelySecret("this is a normal sentence"); ok {
		t.Fatal("unexpected detection")
	}
}

func TestKeyFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "master.key")
	kp := &FileKeyProvider{Path: path}
	key, err := kp.MasterKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 32 {
		t.Fatalf("key length %d", len(key))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
}
