package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/robfig/cron/v3"

	"myagent/internal/ids"
	"myagent/internal/model"
	"myagent/internal/workflow"
)

type fileWatcher struct {
	w        *fsnotify.Watcher
	done     chan struct{}
	mu       sync.Mutex
	lastFire map[string]time.Time
}

func (f *fileWatcher) Close() {
	select {
	case <-f.done:
	default:
		close(f.done)
	}
	_ = f.w.Close()
}

func (a *App) CreateTriggerRecord(ctx context.Context, workflowID string, kind string, config, inputs map[string]any,
	enabled bool, unattended, concurrency string) (model.Trigger, error) {
	w, err := a.Store.GetWorkflowByID(ctx, workflowID)
	if err != nil {
		return model.Trigger{}, err
	}
	if w.CurrentVersionID == "" {
		return model.Trigger{}, errors.New("workflow has no version")
	}
	if v, err := a.Store.GetWorkflowVersion(ctx, w.CurrentVersionID); err != nil || v.Status != "approved" {
		return model.Trigger{}, errors.New("approve the workflow before enabling a trigger")
	}
	switch kind {
	case "cron":
		schedule := fmt.Sprint(config["schedule"])
		if schedule == "" || schedule == "<nil>" {
			return model.Trigger{}, errors.New("cron trigger needs a schedule")
		}
		if _, err := cron.ParseStandard(schedule); err != nil {
			return model.Trigger{}, fmt.Errorf("invalid cron schedule: %w", err)
		}
	case "file":
		paths := config["paths"]
		if paths == nil {
			return model.Trigger{}, errors.New("file trigger needs paths")
		}
	case "webhook":
		if v, ok := config["token"].(string); !ok || strings.TrimSpace(v) == "" {
			config["token"] = ids.New()
		}
	case "cli":
	default:
		return model.Trigger{}, errors.New("trigger kind must be cron, file, webhook or cli")
	}
	if unattended == "" {
		unattended = "ask"
	}
	if concurrency == "" {
		concurrency = "queue"
	}
	cfg, _ := json.Marshal(config)
	inp, _ := json.Marshal(inputs)
	t, err := a.Store.CreateTrigger(ctx, model.Trigger{WorkflowID: w.ID, Kind: kind, Config: cfg, Inputs: inp,
		Enabled: enabled, Unattended: unattended, Concurrency: concurrency, ApprovedAt: ids.NowMS()})
	if err != nil {
		return t, err
	}
	a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "trigger.created",
		Payload: a.mustPayload(map[string]any{"trigger_id": t.ID, "workflow": w.Name, "kind": kind})})
	if enabled {
		a.scheduleTrigger(t)
	}
	return t, nil
}

func (a *App) UpdateTriggerRecord(ctx context.Context, id string, patch map[string]any) (model.Trigger, error) {
	t, err := a.Store.GetTrigger(ctx, id)
	if err != nil {
		return t, err
	}
	if v, ok := patch["enabled"].(bool); ok {
		t.Enabled = v
	}
	if v, ok := patch["unattended"].(string); ok {
		t.Unattended = v
	}
	if v, ok := patch["concurrency"].(string); ok {
		t.Concurrency = v
	}
	if v, ok := patch["config"].(map[string]any); ok {
		raw, _ := json.Marshal(v)
		t.Config = raw
	}
	if v, ok := patch["inputs"].(map[string]any); ok {
		raw, _ := json.Marshal(v)
		t.Inputs = raw
	}
	if err := a.Store.UpdateTrigger(ctx, t); err != nil {
		return t, err
	}
	a.unscheduleTrigger(id)
	if t.Enabled {
		a.scheduleTrigger(t)
	}
	return t, nil
}

func (a *App) DeleteTriggerRecord(ctx context.Context, id string) error {
	a.unscheduleTrigger(id)
	return a.Store.DeleteTrigger(ctx, id)
}

func (a *App) reloadTriggers(ctx context.Context) {
	list, err := a.Store.ListTriggers(ctx, "")
	if err != nil {
		return
	}
	for _, t := range list {
		if t.Enabled {
			a.scheduleTrigger(t)
		}
	}
}

func (a *App) scheduleTrigger(t model.Trigger) {
	a.cronMu.Lock()
	defer a.cronMu.Unlock()
	if id, ok := a.cronIDs[t.ID]; ok {
		a.cron.Remove(id)
		delete(a.cronIDs, t.ID)
	}
	switch t.Kind {
	case "cron":
		var cfg struct {
			Schedule string `json:"schedule"`
		}
		_ = json.Unmarshal(t.Config, &cfg)
		if cfg.Schedule == "" {
			return
		}
		tid := t.ID
		id, err := a.cron.AddFunc(cfg.Schedule, func() {
			tr, err := a.Store.GetTrigger(context.Background(), tid)
			if err != nil {
				return
			}
			a.fireTrigger(tr, "schedule")
		})
		if err == nil {
			a.cronIDs[t.ID] = id
		}
	case "file":
		a.startFileWatcher(t)
	}
}

func (a *App) unscheduleTrigger(id string) {
	a.cronMu.Lock()
	if eid, ok := a.cronIDs[id]; ok {
		a.cron.Remove(eid)
		delete(a.cronIDs, id)
	}
	a.cronMu.Unlock()
	a.watchMu.Lock()
	if list, ok := a.watchers[id]; ok {
		for _, w := range list {
			w.Close()
		}
		delete(a.watchers, id)
	}
	a.watchMu.Unlock()
}

func (a *App) startFileWatcher(t model.Trigger) {
	var cfg struct {
		Paths []string `json:"paths"`
	}
	_ = json.Unmarshal(t.Config, &cfg)
	if len(cfg.Paths) == 0 {
		return
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	fw := &fileWatcher{w: w, done: make(chan struct{}), lastFire: map[string]time.Time{}}
	for _, p := range cfg.Paths {
		path := p
		if !filepath.IsAbs(path) {
			path = filepath.Join(a.Workspace.RootPath, path)
		}
		_ = filepath.WalkDir(path, func(pth string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = w.Add(pth)
			}
			return nil
		})
	}
	tid := t.ID
	go func() {
		for {
			select {
			case <-fw.done:
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
					continue
				}
				if ev.Op&fsnotify.Create != 0 {
					if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
						_ = w.Add(ev.Name)
					}
				}
				fw.mu.Lock()
				last := fw.lastFire[ev.Name]
				if time.Since(last) < 800*time.Millisecond {
					fw.mu.Unlock()
					continue
				}
				fw.lastFire[ev.Name] = time.Now()
				fw.mu.Unlock()
				tr, err := a.Store.GetTrigger(context.Background(), tid)
				if err != nil {
					continue
				}
				inputs := map[string]any{}
				_ = json.Unmarshal(tr.Inputs, &inputs)
				// Only expose the changed path when the workflow declares it.
				if wf, werr := a.Store.GetWorkflowByID(context.Background(), tr.WorkflowID); werr == nil && wf.CurrentVersionID != "" {
					if v, verr := a.Store.GetWorkflowVersion(context.Background(), wf.CurrentVersionID); verr == nil {
						if spec, perr := workflow.Parse([]byte(v.YAML)); perr == nil {
							if _, ok := spec.Inputs["path"]; ok {
								inputs["path"] = ev.Name
							}
						}
					}
				}
				a.fireTriggerWithInputs(tr, "watcher", inputs)
			case <-w.Errors:
			}
		}
	}()
	a.watchMu.Lock()
	a.watchers[t.ID] = append(a.watchers[t.ID], fw)
	a.watchMu.Unlock()
}

func (a *App) fireTrigger(t model.Trigger, source string) {
	inputs := map[string]any{}
	_ = json.Unmarshal(t.Inputs, &inputs)
	a.fireTriggerWithInputs(t, source, inputs)
}

func (a *App) fireTriggerWithInputs(t model.Trigger, source string, inputs map[string]any) {
	ctx := context.Background()
	if t.Concurrency == "skip" {
		if n, _ := a.Store.CountActiveRunsForTarget(ctx, t.WorkflowID); n > 0 {
			a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "trigger.skipped",
				Payload: a.mustPayload(map[string]any{"trigger_id": t.ID, "reason": "concurrency"})})
			return
		}
	}
	w, err := a.Store.GetWorkflowByID(ctx, t.WorkflowID)
	if err != nil {
		return
	}
	a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "trigger.fired",
		Payload: a.mustPayload(map[string]any{"trigger_id": t.ID, "workflow": w.Name, "source": source})})
	run, err := a.RunWorkflow(ctx, w.ID, "", inputs, source, "", false, t.Unattended, "")
	if err != nil {
		a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "trigger.skipped",
			Payload: a.mustPayload(map[string]any{"trigger_id": t.ID, "reason": err.Error()})})
		return
	}
	t.LastFiredAt = ids.NowMS()
	_ = a.Store.UpdateTrigger(ctx, t)
	_ = run
}

func (a *App) TriggerWebhook(ctx context.Context, triggerID, token string, inputs map[string]any) (model.Run, error) {
	t, err := a.Store.GetTrigger(ctx, triggerID)
	if err != nil {
		return model.Run{}, err
	}
	if t.Kind != "webhook" {
		return model.Run{}, errors.New("not a webhook trigger")
	}
	var cfg struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(t.Config, &cfg)
	if cfg.Token == "" || token != cfg.Token {
		return model.Run{}, errors.New("invalid webhook token")
	}
	if !t.Enabled {
		return model.Run{}, errors.New("trigger is disabled")
	}
	merged := map[string]any{}
	_ = json.Unmarshal(t.Inputs, &merged)
	for k, v := range inputs {
		merged[k] = v
	}
	w, err := a.Store.GetWorkflowByID(ctx, t.WorkflowID)
	if err != nil {
		return model.Run{}, err
	}
	a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "trigger.fired",
		Payload: a.mustPayload(map[string]any{"trigger_id": t.ID, "workflow": w.Name, "source": "webhook"})})
	return a.RunWorkflow(ctx, w.ID, "", merged, "webhook", "", false, t.Unattended, "")
}

func (a *App) metaProposeTrigger(ctx context.Context, convID string, args map[string]any) (any, error) {
	w, err := a.Store.GetWorkflowByName(ctx, str(args, "workflow"))
	if err != nil {
		return nil, err
	}
	if w.CurrentVersionID == "" {
		return nil, errors.New("workflow has no version to trigger")
	}
	if v, verr := a.Store.GetWorkflowVersion(ctx, w.CurrentVersionID); verr != nil || v.Status != "approved" {
		return nil, errors.New("the workflow must be approved before a trigger can be proposed")
	}
	kind := str(args, "kind")
	config := object(args, "config")
	inputs := object(args, "inputs")
	payload := map[string]any{"workflow": w.Name, "trigger_kind": kind, "config": config, "inputs": inputs}
	d, err := a.askApproval(ctx, workflow.ApprovalRequest{
		ConversationID: convID, Kind: "trigger", Title: "Enable a trigger for " + w.Name + "?",
		Description: "Kind: " + kind + ". Automated runs can use tools without you watching; approve only if you trust this schedule.",
		Source:      "chat", ExpiresAt: time.Now().Add(2 * time.Hour),
	}, payload)
	if err != nil {
		return nil, err
	}
	if !d.Approved {
		return map[string]any{"ok": false, "status": "rejected"}, nil
	}
	t, err := a.CreateTriggerRecord(ctx, w.ID, kind, config, inputs, true, "ask", "queue")
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "trigger_id": t.ID, "kind": t.Kind, "next_hint": "The trigger is enabled. You can disable it in the workflow's Triggers tab."}, nil
}

func (a *App) sortedTriggers(ts []model.Trigger) []model.Trigger {
	sort.SliceStable(ts, func(i, j int) bool { return ts[i].CreatedAt < ts[j].CreatedAt })
	return ts
}
