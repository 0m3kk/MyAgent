package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"myagent/internal/eventbus"
	"myagent/internal/ids"
	"myagent/internal/llm"
	"myagent/internal/model"
	"myagent/internal/secrets"
	"myagent/internal/store"
	"myagent/internal/tools"
	"myagent/internal/workflow"
)

type Config struct {
	DataDir       string
	WorkspaceRoot string
	DBPath        string
	ArtifactDir   string
	ToolsDir      string
	Address       string
	Version       string
}

type App struct {
	cfg       Config
	Store     *store.Store
	Bus       *eventbus.Bus
	Secrets   *secrets.Service
	Executor  *tools.Executor
	Engine    *workflow.Engine
	LLM       *llm.Client
	Workspace model.Workspace

	runMu      sync.Mutex
	runs       map[string]*RunControl
	approvalMu sync.Mutex
	waiters    map[string]*ApprovalWaiter

	chatMu      sync.Mutex
	chatActive  map[string]bool
	chatCancels map[string]context.CancelFunc

	cronMu   sync.Mutex
	cron     *cron.Cron
	cronIDs  map[string]cron.EntryID
	watchMu  sync.Mutex
	watchers map[string][]*fileWatcher

	closeCh chan struct{}
	sem     chan struct{}

	settingsMu sync.RWMutex
	settings   Settings
}

type Settings struct {
	GlobalConcurrency int     `json:"global_concurrency"`
	AgentMaxSteps     int     `json:"agent_max_steps"`
	AgentBudgetTokens int     `json:"agent_budget_tokens"`
	Notifications     bool    `json:"notifications"`
	DefaultModelID    string  `json:"default_model_id"`
	Theme             string  `json:"theme"`
	FontScale         float64 `json:"font_scale"`
	RetentionDays     int     `json:"retention_days"`
	AutoApproveReads  bool    `json:"auto_approve_reads"`
}

func DefaultSettings() Settings {
	return Settings{
		GlobalConcurrency: 4,
		AgentMaxSteps:     12,
		AgentBudgetTokens: 200000,
		Notifications:     true,
		Theme:             "system",
		FontScale:         1,
		RetentionDays:     90,
		AutoApproveReads:  true,
	}
}

func normalizeSettings(s Settings) Settings {
	if s.Theme != "light" && s.Theme != "dark" && s.Theme != "system" {
		s.Theme = "system"
	}
	if s.FontScale == 0 {
		s.FontScale = 1
	}
	if s.FontScale < 0.85 {
		s.FontScale = 0.85
	}
	if s.FontScale > 1.6 {
		s.FontScale = 1.6
	}
	if s.GlobalConcurrency < 1 || s.GlobalConcurrency > 64 {
		s.GlobalConcurrency = DefaultSettings().GlobalConcurrency
	}
	if s.AgentMaxSteps < 1 || s.AgentMaxSteps > 50 {
		s.AgentMaxSteps = DefaultSettings().AgentMaxSteps
	}
	if s.AgentBudgetTokens <= 0 {
		s.AgentBudgetTokens = DefaultSettings().AgentBudgetTokens
	}
	return s
}

type RunControl struct {
	Cancel   context.CancelFunc
	mu       sync.Mutex
	paused   bool
	resumeCh chan struct{}
}

func (c *RunControl) Pause() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.paused {
		c.paused = true
		c.resumeCh = make(chan struct{})
	}
}

func (c *RunControl) Resume() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.paused {
		c.paused = false
		close(c.resumeCh)
	}
}

func (c *RunControl) IsPaused() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.paused
}

func (c *RunControl) Wait(ctx context.Context) error {
	for {
		c.mu.Lock()
		paused := c.paused
		ch := c.resumeCh
		c.mu.Unlock()
		if !paused {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		}
	}
}

type ApprovalWaiter struct {
	ID   string
	Ch   chan workflow.ApprovalDecision
	Kind string
}

func New(ctx context.Context, cfg Config) (*App, error) {
	if cfg.DataDir == "" {
		cfg.DataDir = defaultDataDir()
	}
	if cfg.WorkspaceRoot == "" {
		cfg.WorkspaceRoot = defaultWorkspace()
	}
	if cfg.DBPath == "" {
		cfg.DBPath = filepath.Join(cfg.DataDir, "myagent.db")
	}
	if cfg.ArtifactDir == "" {
		cfg.ArtifactDir = filepath.Join(cfg.DataDir, "artifacts")
	}
	if cfg.ToolsDir == "" {
		cfg.ToolsDir = filepath.Join(cfg.DataDir, "tools")
	}
	if err := os.MkdirAll(cfg.WorkspaceRoot, 0o755); err != nil {
		return nil, err
	}
	for _, d := range []string{cfg.DataDir, cfg.ArtifactDir, cfg.ToolsDir, filepath.Join(cfg.DataDir, "tmp")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
		_ = os.Chmod(d, 0o700)
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	// Ciphertext-only or not, the database is private app state.
	_ = os.Chmod(cfg.DBPath, 0o600)
	_ = os.Chmod(cfg.DBPath+"-wal", 0o600)
	_ = os.Chmod(cfg.DBPath+"-shm", 0o600)
	bus := eventbus.New(st)
	kp, err := secrets.NewKeyProvider(os.Getenv("MYAGENT_KEY_PROVIDER"), cfg.DataDir)
	if err != nil {
		st.Close()
		return nil, err
	}
	sec, err := secrets.New(ctx, st, kp)
	if err != nil {
		st.Close()
		return nil, err
	}
	ws, err := st.EnsureWorkspace(ctx, "Personal", cfg.WorkspaceRoot)
	if err != nil {
		st.Close()
		return nil, err
	}
	a := &App{
		cfg: cfg, Store: st, Bus: bus, Secrets: sec, Workspace: ws,
		Executor:    tools.NewExecutor(cfg.WorkspaceRoot, cfg.DataDir),
		LLM:         llm.NewClient(),
		runs:        map[string]*RunControl{},
		waiters:     map[string]*ApprovalWaiter{},
		chatActive:  map[string]bool{},
		chatCancels: map[string]context.CancelFunc{},
		cronIDs:     map[string]cron.EntryID{},
		watchers:    map[string][]*fileWatcher{},
		closeCh:     make(chan struct{}),
		sem:         make(chan struct{}, 8),
		settings:    DefaultSettings(),
	}
	a.loadSettings(ctx)
	// Concurrency cap follows settings on boot; changes apply to new runs.
	if a.settings.GlobalConcurrency > 0 && a.settings.GlobalConcurrency < 64 {
		a.sem = make(chan struct{}, a.settings.GlobalConcurrency)
	}
	a.Engine = &workflow.Engine{
		Store: st, Bus: bus, Tools: a, LLM: a, Approvals: a, Controls: a, Loader: a,
		ArtifactDir: cfg.ArtifactDir, Redact: sec.Redact, Notifier: a.notify,
	}
	if err := a.seed(ctx); err != nil {
		st.Close()
		return nil, err
	}
	a.cron = cron.New()
	a.cron.Start()
	a.reloadTriggers(ctx)
	a.recoverInterrupted(ctx)
	_, _ = a.Bus.EmitSimple(ctx, model.Event{Stream: model.StreamSys, Type: "app.started",
		Payload: json.RawMessage(fmt.Sprintf(`{"version":%q,"workspace":%q}`, cfg.Version, ws.RootPath))})
	return a, nil
}

func (a *App) Close() {
	select {
	case <-a.closeCh:
	default:
		close(a.closeCh)
	}
	if a.cron != nil {
		ctx := a.cron.Stop()
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
	a.watchMu.Lock()
	for _, list := range a.watchers {
		for _, w := range list {
			w.Close()
		}
	}
	a.watchers = map[string][]*fileWatcher{}
	a.watchMu.Unlock()
	a.runMu.Lock()
	for _, c := range a.runs {
		c.Cancel()
	}
	a.runMu.Unlock()
	a.Store.Close()
}

func (a *App) Config() Config                 { return a.cfg }
func (a *App) WorkspaceInfo() model.Workspace { return a.Workspace }
func (a *App) KeyKind() string                { return a.Secrets.KeyKind() }
func (a *App) KeyDescription() string         { return a.Secrets.KeyDescription() }

func (a *App) loadSettings(ctx context.Context) {
	raw, err := a.Store.GetSetting(ctx, "settings")
	if err == nil && raw != "" {
		s := DefaultSettings()
		if json.Unmarshal([]byte(raw), &s) == nil {
			a.settings = normalizeSettings(s)
			a.persistSettings(context.Background())
			return
		}
	}
	a.settings = normalizeSettings(DefaultSettings())
	a.persistSettings(context.Background())
}

func (a *App) persistSettings(ctx context.Context) {
	raw, _ := json.Marshal(a.settings)
	_ = a.Store.SetSetting(ctx, "settings", string(raw))
}

func (a *App) GetSettings() Settings {
	a.settingsMu.RLock()
	defer a.settingsMu.RUnlock()
	return a.settings
}

func (a *App) UpdateSettings(ctx context.Context, patch map[string]any) (Settings, error) {
	a.settingsMu.Lock()
	cur := a.settings
	raw, _ := json.Marshal(cur)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	for k, v := range patch {
		m[k] = v
	}
	raw, _ = json.Marshal(m)
	var next Settings
	if err := json.Unmarshal(raw, &next); err != nil {
		a.settingsMu.Unlock()
		return cur, err
	}
	next = normalizeSettings(next)
	a.settings = next
	a.settingsMu.Unlock()
	a.persistSettings(ctx)
	return next, nil
}

func (a *App) registerRun(runID string, cancel context.CancelFunc) *RunControl {
	c := &RunControl{Cancel: cancel, resumeCh: make(chan struct{})}
	a.runMu.Lock()
	a.runs[runID] = c
	a.runMu.Unlock()
	return c
}

func (a *App) unregisterRun(runID string) {
	a.runMu.Lock()
	delete(a.runs, runID)
	a.runMu.Unlock()
}

func (a *App) runControl(runID string) *RunControl {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	return a.runs[runID]
}

func (a *App) WaitIfPaused(ctx context.Context, runID string) error {
	c := a.runControl(runID)
	if c == nil {
		return nil
	}
	return c.Wait(ctx)
}

func (a *App) notify(title, message string) {
	if !a.GetSettings().Notifications {
		return
	}
	if len(message) > 400 {
		message = message[:400]
	}
	script := fmt.Sprintf(`display notification %s with title %s`, quoteAppleScript(message), quoteAppleScript(title))
	go func() {
		_ = exec.Command("/usr/bin/osascript", "-e", script).Run()
	}()
}

func quoteAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func defaultDataDir() string {
	if v := os.Getenv("MYAGENT_DATA_DIR"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "MyAgent")
}

func defaultWorkspace() string {
	if v := os.Getenv("MYAGENT_WORKSPACE"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "MyAgent Workspace")
}

func parseToolRef(ref string) (string, int) {
	ref = strings.TrimSpace(ref)
	if i := strings.LastIndex(ref, "@"); i > 0 {
		if v, err := strconv.Atoi(ref[i+1:]); err == nil {
			return ref[:i], v
		}
	}
	return ref, 0
}

func profileParams(p model.ModelProfile) map[string]any {
	var params map[string]any
	if err := json.Unmarshal(p.Params, &params); err != nil {
		return map[string]any{}
	}
	return params
}

func (a *App) ModelProfileForRole(ctx context.Context, role string) (model.ModelProfile, llm.Profile, error) {
	profiles, err := a.Store.ListModelProfiles(ctx)
	if err != nil {
		return model.ModelProfile{}, llm.Profile{}, err
	}
	if len(profiles) == 0 {
		return model.ModelProfile{}, llm.Profile{}, errors.New("no model profiles configured")
	}
	settings := a.GetSettings()
	var picked *model.ModelProfile
	if settings.DefaultModelID != "" {
		for i := range profiles {
			if profiles[i].ID == settings.DefaultModelID {
				picked = &profiles[i]
				break
			}
		}
	}
	if picked == nil {
		order := []string{role, "executor", "planner", "cheap", "judge"}
		for _, r := range order {
			for i := range profiles {
				if profiles[i].Role == r {
					picked = &profiles[i]
					break
				}
			}
			if picked != nil {
				break
			}
		}
	}
	if picked == nil {
		picked = &profiles[0]
	}
	p := *picked
	apiKey := ""
	if p.SecretID != "" {
		val, _, err := a.Secrets.Get(ctx, p.SecretID)
		if err != nil {
			return p, llm.Profile{}, fmt.Errorf("API key secret for %s is unavailable: %w", p.Name, err)
		}
		apiKey = val
	}
	return p, llm.Profile{
		ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, Model: p.Model, Role: p.Role,
		Params: profileParams(p), APIKey: apiKey, SecretID: p.SecretID,
	}, nil
}

func (a *App) ChatWithTools(ctx context.Context, req workflow.LLMChatRequest) (workflow.LLMChatResult, error) {
	_, prof, err := a.ModelProfileForRole(ctx, req.Role)
	if err != nil {
		return workflow.LLMChatResult{}, err
	}
	msgs := make([]llm.Message, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, llm.Message{Role: "system", Content: a.Secrets.Redact(req.System)})
	}
	for _, m := range req.Messages {
		m.Content = a.Secrets.Redact(m.Content)
		for i := range m.ToolCalls {
			m.ToolCalls[i].Function.Arguments = a.Secrets.Redact(m.ToolCalls[i].Function.Arguments)
		}
		msgs = append(msgs, m)
	}
	chatReq := llm.ChatRequest{
		Messages: msgs, Tools: req.Tools, Temperature: req.Temperature, MaxTokens: req.MaxTokens,
		OutputSchema: req.OutputSchema, Stream: true,
	}
	res, err := a.LLM.Chat(ctx, prof, chatReq, func(ev llm.StreamEvent) {
		if ev.TextDelta != "" && req.Stream != nil {
			req.Stream(a.Secrets.Redact(ev.TextDelta))
		}
	})
	if err != nil {
		return workflow.LLMChatResult{}, err
	}
	usage := res.Usage
	cost := llm.CostMicro(prof, usage)
	out := workflow.LLMChatResult{
		Message: res.Message, Text: res.Message.Content, TokensIn: usage.PromptTokens,
		TokensOut: usage.CompletionTokens, CostMicro: cost, Model: res.Model, Finish: res.FinishReason,
	}
	if req.OutputSchema != nil {
		var parsed any
		text := stripFences(res.Message.Content)
		if json.Unmarshal([]byte(text), &parsed) == nil {
			out.Data = parsed
		}
	}
	return out, nil
}

func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return strings.TrimSpace(s)
}

func (a *App) LoadWorkflow(ctx context.Context, ref string) (*workflow.Spec, string, error) {
	name, ver := parseToolRef(ref)
	_, wv, err := a.Store.ResolveWorkflow(ctx, name, ver)
	if err != nil {
		return nil, "", err
	}
	spec, err := workflow.Parse([]byte(wv.YAML))
	if err != nil {
		return nil, "", err
	}
	return spec, wv.ID, nil
}

func (a *App) Resolve(ctx context.Context, ref string) (workflow.ToolBinding, error) {
	name, ver := parseToolRef(ref)
	t, err := a.Store.GetToolByName(ctx, name)
	if err != nil {
		return workflow.ToolBinding{}, err
	}
	var tv model.ToolVersion
	if ver > 0 {
		tv, err = a.Store.GetToolVersionByNumber(ctx, t.ID, ver)
	} else if t.CurrentVersionID != "" {
		tv, err = a.Store.GetToolVersion(ctx, t.CurrentVersionID)
	} else {
		return workflow.ToolBinding{}, fmt.Errorf("tool %s has no version", name)
	}
	if err != nil {
		return workflow.ToolBinding{}, err
	}
	if tv.Status != model.ToolActive && tv.Status != model.ToolApproved && tv.Status != model.ToolDeprecated {
		return workflow.ToolBinding{}, fmt.Errorf("tool %s version %d is %s, not approved", name, tv.Version, tv.Status)
	}
	m, err := tools.ParseManifest(tv.Manifest)
	if err != nil {
		return workflow.ToolBinding{}, err
	}
	return workflow.ToolBinding{
		Name: m.Name, VersionID: tv.ID, ContentHash: tv.ContentHash, SideEffect: m.SideEffect,
		Description: m.Description, InputSchema: m.InputSchema, Kind: m.Kind,
		Secrets: secretNames(m.Secrets),
	}, nil
}

func secretNames(decls []tools.SecretDecl) []string {
	out := []string{}
	for _, d := range decls {
		out = append(out, d.Name)
	}
	return out
}

func (a *App) Execute(ctx context.Context, binding workflow.ToolBinding, input map[string]any, opts workflow.ToolExecOptions) (any, error) {
	tv, err := a.Store.GetToolVersion(ctx, binding.VersionID)
	if err != nil {
		return nil, err
	}
	m, err := tools.ParseManifest(tv.Manifest)
	if err != nil {
		return nil, err
	}
	t, err := a.Store.GetToolByID(ctx, tv.ToolID)
	if err != nil {
		return nil, err
	}
	secVals, err := a.resolveToolSecretsForVersion(ctx, t.ID, tv.ID, m, opts.RunID, opts.StepID, opts.StepRunID, opts.Source)
	if err != nil {
		return nil, err
	}
	out, err := a.Executor.Execute(ctx, m, input, tools.ExecOptions{
		ToolDir: tv.CodePath, ToolName: m.Name, Manifest: m, Secrets: secVals,
		RunID: opts.RunID, IdemKey: opts.IdemKey, OnLog: opts.OnLog, DryRun: opts.DryRun, Source: opts.Source,
	})
	if err != nil {
		return nil, err
	}
	// A tool can echo a secret. Redact before the value reaches a message,
	// event, artifact, trace or the model.
	return redactAny(a.Secrets.Redact, out), nil
}

func redactAny(redact func(string) string, v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	redacted := redact(string(raw))
	var out any
	if json.Unmarshal([]byte(redacted), &out) != nil {
		return redact(fmt.Sprint(v))
	}
	return out
}

func (a *App) NewID() string { return ids.New() }

func (a *App) ApprovalWaiterFor(id string) *ApprovalWaiter {
	a.approvalMu.Lock()
	defer a.approvalMu.Unlock()
	return a.waiters[id]
}
