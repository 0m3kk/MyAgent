package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"myagent/internal/ids"
	"myagent/internal/model"
	"myagent/internal/store"
)

// KeyProvider supplies the 32-byte master key. The database only ever holds
// ciphertext; losing the provider means losing the secrets.
type KeyProvider interface {
	MasterKey(ctx context.Context) ([]byte, error)
	Kind() string
	Description() string
}

// FileKeyProvider is intended for development and unsigned builds. The release
// build swaps in the macOS Keychain provider via the same interface.
type FileKeyProvider struct {
	Path string
	mu   sync.Mutex
}

func (p *FileKeyProvider) Kind() string { return "file" }
func (p *FileKeyProvider) Description() string {
	return "Development key file (0600). Release builds use the macOS Keychain."
}

func (p *FileKeyProvider) MasterKey(ctx context.Context) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(p.Path), 0o700); err != nil {
		return nil, err
	}
	if data, err := os.ReadFile(p.Path); err == nil {
		if len(data) != 32 {
			return nil, errors.New("master key file is corrupt (expected 32 bytes)")
		}
		return data, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(key); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return key, nil
}

type Service struct {
	store *store.Store
	key   []byte
	kind  string
	desc  string

	mu     sync.RWMutex
	values map[string]string // secret name -> plaintext, in memory only
}

func New(ctx context.Context, st *store.Store, kp KeyProvider) (*Service, error) {
	key, err := kp.MasterKey(ctx)
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("master key must be 32 bytes")
	}
	s := &Service{
		store:  st,
		key:    key,
		kind:   kp.Kind(),
		desc:   kp.Description(),
		values: map[string]string{},
	}
	if err := s.load(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) load(ctx context.Context) error {
	all, err := s.store.AllSecretRecords(ctx)
	if err != nil {
		return err
	}
	for _, rec := range all {
		val, err := s.decrypt(rec)
		if err != nil {
			// A corrupt row must not prevent the app from starting; it will be
			// surfaced as missing when a tool asks for it.
			continue
		}
		s.values[rec.Name] = val
	}
	return nil
}

func (s *Service) KeyKind() string        { return s.kind }
func (s *Service) KeyDescription() string { return s.desc }

func (s *Service) encrypt(id, value string) (ciphertext, nonce []byte, keyVersion int, err error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, nil, 0, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, 0, err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, 0, err
	}
	return gcm.Seal(nil, nonce, []byte(value), []byte(id)), nonce, 1, nil
}

func (s *Service) decrypt(rec model.Secret) (string, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, rec.Nonce, rec.Ciphertext, []byte(rec.ID))
	if err != nil {
		return "", fmt.Errorf("cannot decrypt secret %s: %w", rec.Name, err)
	}
	return string(plain), nil
}

func (s *Service) Put(
	ctx context.Context,
	name, kind, description, value string,
	expiresAt int64,
	askEachRun bool,
) (model.SecretMeta, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.SecretMeta{}, errors.New("secret name is required")
	}
	if value == "" {
		return model.SecretMeta{}, errors.New("secret value is required")
	}
	existing, err := s.store.GetSecretByName(ctx, name)
	var id string
	var createdAt int64
	if err == nil {
		id, createdAt = existing.ID, existing.CreatedAt
	} else if !errors.Is(err, store.ErrNotFound) {
		return model.SecretMeta{}, err
	}
	if id == "" {
		id = ids.New()
	}
	ciphertext, nonce, kv, err := s.encrypt(id, value)
	if err != nil {
		return model.SecretMeta{}, err
	}
	rec := model.Secret{
		ID:          id,
		Name:        name,
		Kind:        kind,
		Description: description,
		ExpiresAt:   expiresAt,
		AskEachRun:  askEachRun,
		CreatedAt:   createdAt,
		Ciphertext:  ciphertext,
		Nonce:       nonce,
		KeyVersion:  kv,
	}
	if rec.Kind == "" {
		rec.Kind = "api_key"
	}
	if _, err := s.store.UpsertSecret(ctx, rec); err != nil {
		return model.SecretMeta{}, err
	}
	s.mu.Lock()
	s.values[name] = value
	s.mu.Unlock()
	return model.SecretMeta{
		ID:          id,
		Name:        name,
		Kind:        rec.Kind,
		Description: description,
		ExpiresAt:   expiresAt,
		AskEachRun:  askEachRun,
		CreatedAt:   rec.CreatedAt,
		Status:      "present",
	}, nil
}

func (s *Service) Get(ctx context.Context, id string) (string, model.Secret, error) {
	rec, err := s.store.GetSecret(ctx, id)
	if err != nil {
		return "", rec, err
	}
	val, err := s.decrypt(rec)
	if err != nil {
		return "", rec, err
	}
	if err := s.store.TouchSecret(ctx, id); err != nil {
		return "", rec, err
	}
	return val, rec, nil
}

func (s *Service) GetByName(ctx context.Context, name string) (string, model.Secret, error) {
	rec, err := s.store.GetSecretByName(ctx, name)
	if err != nil {
		return "", rec, err
	}
	val, err := s.decrypt(rec)
	if err != nil {
		return "", rec, err
	}
	return val, rec, nil
}

func (s *Service) List(ctx context.Context) ([]model.SecretMeta, error) {
	metas, err := s.store.ListSecrets(ctx)
	if err != nil {
		return nil, err
	}
	now := ids.NowMS()
	for i := range metas {
		metas[i].Status = "present"
		if metas[i].ExpiresAt > 0 && metas[i].ExpiresAt <= now {
			metas[i].Status = "expired"
		}
		metas[i].UsedBy, _ = s.store.SecretUsedBy(ctx, metas[i].ID)
	}
	return metas, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	rec, err := s.store.GetSecret(ctx, id)
	if err != nil {
		return err
	}
	if err := s.store.DeleteSecret(ctx, id); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.values, rec.Name)
	s.mu.Unlock()
	return nil
}

func (s *Service) Status(ctx context.Context, name string) string {
	s.mu.RLock()
	_, ok := s.values[name]
	s.mu.RUnlock()
	if !ok {
		return "missing"
	}
	if rec, err := s.store.GetSecretByName(ctx, name); err == nil && rec.ExpiresAt > 0 &&
		rec.ExpiresAt <= ids.NowMS() {
		return "expired"
	}
	return "present"
}

func (s *Service) Names(ctx context.Context) []string {
	metas, err := s.store.ListSecrets(ctx)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(metas))
	for _, m := range metas {
		out = append(out, m.Name)
	}
	return out
}

// Redact replaces every known secret value, including URL- and base64-encoded
// forms. Call this before persisting events, logs, tool results or LLM input.
func (s *Service) Redact(text string) string {
	if text == "" {
		return text
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for name, val := range s.values {
		if len(val) < 6 {
			continue
		}
		res := "[REDACTED:" + name + "]"
		text = strings.ReplaceAll(text, val, res)
		text = strings.ReplaceAll(text, url.QueryEscape(val), res)
		text = strings.ReplaceAll(text, url.PathEscape(val), res)
		text = strings.ReplaceAll(text, base64.StdEncoding.EncodeToString([]byte(val)), res)
		text = strings.ReplaceAll(text, base64.RawURLEncoding.EncodeToString([]byte(val)), res)
	}
	return text
}

// RedactJSON is a byte-level helper for event payloads.
func (s *Service) RedactJSON(raw []byte) []byte {
	return []byte(s.Redact(string(raw)))
}

// DetectLikelySecret catches common API-key shapes pasted into chat. The value
// must not be sent to the model. Returns the candidate value.
func DetectLikelySecret(text string) (string, bool) {
	prefixes := []string{
		"sk-",
		"sk_live_",
		"ghp_",
		"github_pat_",
		"xoxb-",
		"AKIA",
		"AIza",
		"glpat-",
	}
	for _, field := range strings.Fields(text) {
		clean := strings.Trim(field, `"'`+"`.,;:()[]{}<>")
		for _, p := range prefixes {
			if strings.HasPrefix(clean, p) && len(clean) >= len(p)+12 {
				return clean, true
			}
		}
	}
	return "", false
}
