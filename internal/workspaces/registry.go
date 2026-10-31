package workspaces

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"myagent/internal/ids"
)

const registryVersion = 1

var (
	ErrNotConfigured = errors.New("workspace profiles are not configured")
	ErrNotFound      = errors.New("workspace not found")
	ErrDuplicate     = errors.New("workspace name already exists")
	ErrActive        = errors.New("cannot delete the active workspace")
	ErrLast          = errors.New("cannot delete the last workspace")
)

// Profile is one isolated workspace profile: its own database, artifacts,
// tools and settings. RootPath is the project folder the agent may access.
type Profile struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	RootPath     string `json:"root_path"`
	DataDir      string `json:"data_dir"`
	CreatedAt    int64  `json:"created_at"`
	LastOpenedAt int64  `json:"last_opened_at"`
}

type registryFile struct {
	Version    int       `json:"version"`
	Active     string    `json:"active"`
	Workspaces []Profile `json:"workspaces"`
}

type Registry struct {
	mu     sync.Mutex
	home   string
	path   string
	active string
	list   []Profile
}

// Open loads workspaces.json from home. It returns ErrNotConfigured when the
// file does not exist.
func Open(home string) (*Registry, error) {
	if home == "" {
		return nil, ErrNotConfigured
	}
	home = filepath.Clean(home)
	path := filepath.Join(home, "workspaces.json")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotConfigured
	}
	if err != nil {
		return nil, err
	}
	var f registryFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("read workspaces.json: %w", err)
	}
	if f.Version == 0 {
		f.Version = registryVersion
	}
	for i := range f.Workspaces {
		normalizeProfile(home, &f.Workspaces[i])
	}
	if len(f.Workspaces) == 0 {
		return nil, ErrNotConfigured
	}
	if f.Active == "" || !hasProfile(f.Workspaces, f.Active) {
		f.Active = f.Workspaces[0].ID
	}
	return &Registry{home: home, path: path, active: f.Active, list: f.Workspaces}, nil
}

// OpenOrCreate is used by agentd. When home is empty it returns nil, which
// disables the workspace API for direct/dev agentd runs.
func OpenOrCreate(home, dataDir, root, name string) (*Registry, error) {
	if home == "" {
		return nil, nil
	}
	reg, err := Open(home)
	if err == nil {
		return reg, nil
	}
	if !errors.Is(err, ErrNotConfigured) {
		return nil, err
	}
	if name == "" {
		name = "Personal"
	}
	if root == "" {
		root = defaultHomeRoot()
	}
	if dataDir == "" {
		dataDir = filepath.Join(home, "profiles", "default")
	}
	p, err := newProfile(filepath.Clean(home), "default", name, root, dataDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(p.DataDir, 0o700); err != nil {
		return nil, err
	}
	reg = &Registry{
		home:   filepath.Clean(home),
		path:   filepath.Join(filepath.Clean(home), "workspaces.json"),
		active: p.ID,
		list:   []Profile{p},
	}
	if err := reg.saveLocked(); err != nil {
		return nil, err
	}
	return reg, nil
}

func (r *Registry) Home() string {
	if r == nil {
		return ""
	}
	return r.home
}

// Reload refreshes the in-memory registry after a helper such as Import has
// written profiles.json on disk.
func (r *Registry) Reload() error {
	if r == nil {
		return ErrNotConfigured
	}
	loaded, err := Open(r.home)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active = loaded.active
	r.list = loaded.list
	return nil
}

func (r *Registry) Active() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

func (r *Registry) Profiles() []Profile {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := append([]Profile(nil), r.list...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID == r.active {
			return true
		}
		if out[j].ID == r.active {
			return false
		}
		return out[i].CreatedAt < out[j].CreatedAt
	})
	return out
}

func (r *Registry) Get(id string) (Profile, error) {
	if r == nil {
		return Profile{}, ErrNotConfigured
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.list {
		if p.ID == id {
			return p, nil
		}
	}
	return Profile{}, ErrNotFound
}

func (r *Registry) Create(name, root string) (Profile, error) {
	if r == nil {
		return Profile{}, ErrNotConfigured
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Profile{}, errors.New("workspace name is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if nameTaken(r.list, name, "") {
		return Profile{}, ErrDuplicate
	}
	if root == "" {
		root = defaultRoot(name)
	}
	id := ids.New()
	p, err := newProfile(r.home, id, name, root, filepath.Join(r.home, "profiles", id))
	if err != nil {
		return Profile{}, err
	}
	if err := os.MkdirAll(p.DataDir, 0o700); err != nil {
		return Profile{}, err
	}
	r.list = append(r.list, p)
	if err := r.saveLocked(); err != nil {
		r.list = r.list[:len(r.list)-1]
		return Profile{}, err
	}
	return p, nil
}

func (r *Registry) Update(id, name, root string) (Profile, error) {
	if r == nil {
		return Profile{}, ErrNotConfigured
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	idx := -1
	for i := range r.list {
		if r.list[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Profile{}, ErrNotFound
	}
	old := r.list[idx]
	p := old
	if name = strings.TrimSpace(name); name != "" && name != p.Name {
		if nameTaken(r.list, name, id) {
			return Profile{}, ErrDuplicate
		}
		p.Name = name
	}
	if root = strings.TrimSpace(root); root != "" {
		expanded, err := expandPath(r.home, root)
		if err != nil {
			return Profile{}, err
		}
		if err := os.MkdirAll(expanded, 0o755); err != nil {
			return Profile{}, err
		}
		p.RootPath = expanded
	}
	r.list[idx] = p
	if err := r.saveLocked(); err != nil {
		r.list[idx] = old
		return Profile{}, err
	}
	return p, nil
}

func (r *Registry) Delete(id string) error {
	if r == nil {
		return ErrNotConfigured
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if id == r.active {
		return ErrActive
	}
	if len(r.list) <= 1 {
		return ErrLast
	}
	idx := -1
	for i := range r.list {
		if r.list[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNotFound
	}
	p := r.list[idx]
	old := append([]Profile(nil), r.list...)
	r.list = append(r.list[:idx], r.list[idx+1:]...)
	if err := r.saveLocked(); err != nil {
		r.list = old
		return err
	}
	// Only remove data we own under home/profiles. Never touch the project root.
	if within, err := pathWithin(
		filepath.Join(r.home, "profiles"),
		p.DataDir,
	); err == nil &&
		within {
		_ = os.RemoveAll(p.DataDir)
	}
	return nil
}

func (r *Registry) Activate(id string) (Profile, error) {
	if r == nil {
		return Profile{}, ErrNotConfigured
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.list {
		if r.list[i].ID == id {
			r.active = id
			r.list[i].LastOpenedAt = ids.NowMS()
			if err := r.saveLocked(); err != nil {
				return Profile{}, err
			}
			return r.list[i], nil
		}
	}
	return Profile{}, ErrNotFound
}

// AddImported adds a profile prepared by Import. It picks a unique display name
// when the imported name is already taken.
func (r *Registry) AddImported(p Profile) (Profile, error) {
	if r == nil {
		return Profile{}, ErrNotConfigured
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p.Name = uniqueName(r.list, p.Name)
	if p.CreatedAt == 0 {
		p.CreatedAt = ids.NowMS()
	}
	r.list = append(r.list, p)
	if err := r.saveLocked(); err != nil {
		r.list = r.list[:len(r.list)-1]
		return Profile{}, err
	}
	return p, nil
}

func (r *Registry) saveLocked() error {
	f := registryFile{Version: registryVersion, Active: r.active, Workspaces: r.list}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(r.home, 0o700); err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, r.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func newProfile(home, id, name, root, dataDir string) (Profile, error) {
	root, err := expandPath(home, root)
	if err != nil {
		return Profile{}, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Profile{}, fmt.Errorf("create workspace folder: %w", err)
	}
	dataDir, err = expandPath(home, dataDir)
	if err != nil {
		return Profile{}, err
	}
	now := ids.NowMS()
	return Profile{
		ID:           id,
		Name:         strings.TrimSpace(name),
		RootPath:     root,
		DataDir:      dataDir,
		CreatedAt:    now,
		LastOpenedAt: now,
	}, nil
}

func normalizeProfile(home string, p *Profile) {
	if p.ID == "" {
		p.ID = ids.New()
	}
	if p.DataDir == "" {
		p.DataDir = filepath.Join(home, "profiles", p.ID)
	}
	if expanded, err := expandPath(home, p.DataDir); err == nil {
		p.DataDir = expanded
	}
	if p.Name == "" {
		p.Name = "Workspace"
	}
	if p.RootPath == "" {
		p.RootPath = defaultRoot(p.Name)
	}
	if expanded, err := expandPath(home, p.RootPath); err == nil {
		p.RootPath = expanded
	}
}

func expandPath(home, p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("path is required")
	}
	if p == "~" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = userHome
	} else if strings.HasPrefix(p, "~/") {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(userHome, strings.TrimPrefix(p, "~/"))
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(home, p)
	}
	return filepath.Clean(p), nil
}

func hasProfile(list []Profile, id string) bool {
	for _, p := range list {
		if p.ID == id {
			return true
		}
	}
	return false
}

func nameTaken(list []Profile, name, exceptID string) bool {
	for _, p := range list {
		if p.ID != exceptID &&
			strings.EqualFold(strings.TrimSpace(p.Name), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

func uniqueName(list []Profile, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Imported workspace"
	}
	if !nameTaken(list, name, "") {
		return name
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s (%d)", name, i)
		if !nameTaken(list, candidate, "") {
			return candidate
		}
	}
}

func slug(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "workspace"
	}
	return out
}

func defaultHomeRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "MyAgent"
	}
	return filepath.Join(home, "MyAgent")
}

func defaultRoot(name string) string {
	return filepath.Join(defaultHomeRoot(), slug(name))
}

func pathWithin(base, target string) (bool, error) {
	base, err := filepath.Abs(base)
	if err != nil {
		return false, err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false, err
	}
	return rel == "." ||
		(rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))), nil
}
