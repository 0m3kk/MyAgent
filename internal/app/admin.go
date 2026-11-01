package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"myagent/internal/ids"
	"myagent/internal/llm"
	"myagent/internal/model"
	"myagent/internal/tools"
	"myagent/internal/workflow"
)

type Bootstrap struct {
	Workspace      model.Workspace             `json:"workspace"`
	Settings       Settings                    `json:"settings"`
	ModelProfiles  []model.ModelProfile        `json:"model_profiles"`
	KeyKind        string                      `json:"key_kind"`
	KeyDescription string                      `json:"key_description"`
	Conversations  []model.Conversation        `json:"conversations"`
	Workflows      []model.WorkflowWithVersion `json:"workflows"`
	Tools          []model.ToolWithVersion     `json:"tools"`
	Folders        []model.Folder              `json:"folders"`
	Runs           []model.Run                 `json:"runs"`
	Approvals      []model.Approval            `json:"approvals"`
	SecretCount    int                         `json:"secret_count"`
	Version        string                      `json:"version"`
	PendingCount   int                         `json:"pending_count"`
	Counts         map[string]int              `json:"counts"`
}

func (a *App) Bootstrap(ctx context.Context) (Bootstrap, error) {
	convs, _ := a.Store.ListConversations(ctx, 100)
	workflows, _ := a.WorkflowList(ctx)
	toolList, _ := a.ToolList(ctx)
	folders, _ := a.Store.ListFolders(ctx, "")
	runs, _ := a.Store.ListRuns(ctx, model.RunFilter{Limit: 30})
	approvals, _ := a.Store.PendingApprovals(ctx)
	profiles, _ := a.Store.ListModelProfiles(ctx)
	secrets, _ := a.Secrets.List(ctx)
	allRuns, _ := a.Store.ListRuns(ctx, model.RunFilter{Limit: 500})
	counts := map[string]int{
		"workflows": len(workflows), "tools": len(toolList), "runs": len(allRuns),
		"conversations": len(convs), "secrets": len(secrets), "approvals": len(approvals),
	}
	for _, r := range allRuns {
		counts["run_"+r.Status]++
	}
	return Bootstrap{
		Workspace: a.Workspace, Settings: a.GetSettings(), ModelProfiles: profiles,
		KeyKind: a.KeyKind(), KeyDescription: a.KeyDescription(), Conversations: convs,
		Workflows: workflows, Tools: toolList, Folders: folders, Runs: runs, Approvals: approvals,
		SecretCount: len(
			secrets,
		), Version: a.cfg.Version, PendingCount: len(approvals), Counts: counts,
	}, nil
}

func (a *App) WorkflowList(ctx context.Context) ([]model.WorkflowWithVersion, error) {
	list, err := a.Store.ListWorkflows(ctx)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].Version != nil {
			if spec, err := workflow.Parse([]byte(list[i].Version.YAML)); err == nil {
				list[i].Steps = stepViews(spec)
			}
		}
	}
	return list, nil
}

func stepViews(spec *workflow.Spec) []model.WorkflowStepView {
	out := []model.WorkflowStepView{}
	for _, st := range spec.Steps {
		out = append(out, model.WorkflowStepView{ID: st.ID, Kind: st.StepKind(), Needs: st.Needs,
			Tool: st.Tool, Summary: st.Summary()})
	}
	return out
}

func (a *App) ToolList(ctx context.Context) ([]model.ToolWithVersion, error) {
	list, err := a.Store.ListTools(ctx, model.ToolFilter{})
	if err != nil {
		return nil, err
	}
	for i := range list {
		a.attachToolManifest(ctx, &list[i])
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

func (a *App) attachToolManifest(ctx context.Context, t *model.ToolWithVersion) {
	if t.Version == nil {
		return
	}
	m, err := tools.ParseManifest(t.Version.Manifest)
	if err != nil {
		t.Description = "invalid manifest: " + err.Error()
		return
	}
	t.Description = m.Description
	t.InputSchema = m.InputSchemaJSON()
	t.OutputSchema = m.OutputSchemaJSON()
	t.SideEffect = m.SideEffect
	t.Permissions = m.PermissionsJSON()
	t.Secrets = m.SecretsJSON()
	t.Tests = m.TestsJSON()
	t.Runtime = m.RuntimeJSON()
}

type ToolDetail struct {
	Tool         model.ToolWithVersion `json:"tool"`
	Versions     []model.ToolVersion   `json:"versions"`
	Files        []ToolFile            `json:"files"`
	Bindings     []model.SecretBinding `json:"bindings"`
	ManifestYAML string                `json:"manifest_yaml"`
}

type ToolFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

func (a *App) ToolDetail(ctx context.Context, id string) (ToolDetail, error) {
	t, err := a.Store.GetToolByID(ctx, id)
	if err != nil {
		return ToolDetail{}, err
	}
	tw := model.ToolWithVersion{Tool: t}
	versions, err := a.Store.ToolVersions(ctx, id)
	if err != nil {
		return ToolDetail{}, err
	}
	tw.Versions = versions
	chosen := t.CurrentVersionID
	if t.DraftVersionID != "" {
		chosen = t.DraftVersionID
	}
	if chosen != "" {
		for i := range versions {
			if versions[i].ID == chosen {
				v := versions[i]
				tw.Version = &v
			}
		}
	}
	if tw.Version == nil && t.CurrentVersionID != "" {
		for i := range versions {
			if versions[i].ID == t.CurrentVersionID {
				v := versions[i]
				tw.Version = &v
			}
		}
	}
	if tw.Version == nil && len(versions) > 0 {
		v := versions[0]
		tw.Version = &v
	}
	a.attachToolManifest(ctx, &tw)
	detail := ToolDetail{Tool: tw, Versions: versions}
	if tw.Version != nil {
		detail.ManifestYAML = tw.Version.Manifest
		detail.Files = listCodeFiles(tw.Version.CodePath)
	}
	detail.Bindings, _ = a.Store.SecretBindings(ctx, id)
	return detail, nil
}

func listCodeFiles(dir string) []ToolFile {
	out := []ToolFile{}
	if dir == "" {
		return out
	}
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, _ := d.Info()
		rel, _ := filepath.Rel(dir, path)
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		out = append(out, ToolFile{Path: rel, Size: size})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func (a *App) ReadToolFile(ctx context.Context, toolID, versionID, path string) (string, error) {
	tv, err := a.Store.GetToolVersion(ctx, versionID)
	if err != nil {
		return "", err
	}
	full := filepath.Join(tv.CodePath, filepath.Clean(path))
	if !strings.HasPrefix(full, filepath.Clean(tv.CodePath)+string(os.PathSeparator)) {
		return "", fmt.Errorf("path is outside the tool directory")
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	if len(raw) > 512*1024 {
		return "", fmt.Errorf("file is too large")
	}
	return string(raw), nil
}

// ---- model profiles ----

func (a *App) SaveModelProfile(
	ctx context.Context,
	p model.ModelProfile,
	apiKey string,
) (model.ModelProfile, error) {
	if strings.TrimSpace(p.Name) == "" {
		p.Name = p.Model
	}
	if p.Role == "" {
		p.Role = "executor"
	}
	if apiKey != "" {
		secretName := "MODEL_" + strings.ToUpper(sanitizeIdent(p.Name)) + "_API_KEY"
		meta, err := a.Secrets.Put(
			ctx,
			secretName,
			"api_key",
			"API key for model profile "+p.Name,
			apiKey,
			0,
			false,
		)
		if err != nil {
			return p, err
		}
		p.SecretID = meta.ID
	}
	return a.Store.UpsertModelProfile(ctx, p)
}

func (a *App) ProbeModel(ctx context.Context, id string) (llm.Capabilities, error) {
	p, err := a.Store.GetModelProfile(ctx, id)
	if err != nil {
		return llm.Capabilities{}, err
	}
	_, prof, err := a.profileToLLM(ctx, p)
	if err != nil {
		return llm.Capabilities{}, err
	}
	pctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	caps := a.LLM.Probe(pctx, prof)
	raw, _ := json.Marshal(caps)
	_, err = a.Store.UpsertModelProfile(ctx, model.ModelProfile{
		ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, Model: p.Model, Role: p.Role,
		Params: p.Params, Capabilities: raw, SecretID: p.SecretID, CreatedAt: p.CreatedAt,
	})
	if err != nil {
		return caps, err
	}
	return caps, nil
}

func (a *App) profileToLLM(
	ctx context.Context,
	p model.ModelProfile,
) (model.ModelProfile, llm.Profile, error) {
	apiKey := ""
	if p.SecretID != "" {
		val, _, err := a.Secrets.Get(ctx, p.SecretID)
		if err != nil {
			return p, llm.Profile{}, err
		}
		apiKey = val
	}
	return p, llm.Profile{ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, Model: p.Model,
		Role: p.Role, Params: profileParams(p), APIKey: apiKey, SecretID: p.SecretID}, nil
}

func (a *App) DeleteModelProfile(ctx context.Context, id string) error {
	return a.Store.DeleteModelProfile(ctx, id)
}

// ---- secrets ----

func (a *App) PutSecret(
	ctx context.Context,
	name, kind, description, value string,
	expiresAt int64,
	askEachRun bool,
) (model.SecretMeta, error) {
	return a.Secrets.Put(ctx, name, kind, description, value, expiresAt, askEachRun)
}

func (a *App) BindToolSecret(ctx context.Context, toolID, logicalName, secretID string) error {
	if _, err := a.Store.GetToolByID(ctx, toolID); err != nil {
		return err
	}
	if _, _, err := a.Secrets.Get(ctx, secretID); err != nil {
		return err
	}
	return a.Store.SetSecretBinding(ctx, model.SecretBinding{
		ToolID: toolID, LogicalName: logicalName, SecretID: secretID,
		ScopeHash: secretID, ApprovedAt: ids.NowMS(),
	})
}

func (a *App) RotateSecrets(ctx context.Context) error {
	return fmt.Errorf("master key rotation is not enabled in this build")
}

func sanitizeIdent(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func (a *App) TriggerList(ctx context.Context, workflowID string) ([]model.Trigger, error) {
	return a.Store.ListTriggers(ctx, workflowID)
}

func (a *App) FeedbackList(ctx context.Context, runID string) (model.Feedback, error) {
	return a.Store.GetFeedback(ctx, "run", runID)
}
