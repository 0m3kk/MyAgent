package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"myagent/internal/ids"
	"myagent/internal/llm"
	"myagent/internal/model"
	"myagent/internal/workflow"
)

// OrganizeResult is the outcome of one automatic library organization pass.
type OrganizeResult struct {
	Kind           string   `json:"kind"`
	Summary        string   `json:"summary"`
	Model          string   `json:"model,omitempty"`
	CreatedFolders []string `json:"created_folders"`
	AssignedItems  int      `json:"assigned_items"`
	MovedItems     int      `json:"moved_items"`
	UnchangedItems int      `json:"unchanged_items"`
}

type organizeItem struct {
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	Status        string   `json:"status,omitempty"`
	Description   string   `json:"description,omitempty"`
	CurrentFolder []string `json:"current_folder,omitempty"`
}

type organizeTarget struct {
	ID          string
	OldFolderID string
}

type organizePath []string

func (p *organizePath) UnmarshalJSON(data []byte) error {
	var path []string
	if err := json.Unmarshal(data, &path); err == nil {
		*p = path
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		text = strings.TrimSpace(text)
		if text == "" {
			*p = nil
			return nil
		}
		parts := strings.FieldsFunc(text, func(r rune) bool { return r == '/' || r == '>' })
		*p = parts
		return nil
	}
	return errors.New("folder must be an array of folder names")
}

type organizeAssignment struct {
	Item   string       `json:"item"`
	Folder organizePath `json:"folder"`
}

type organizePlan struct {
	Summary     string               `json:"summary"`
	Assignments []organizeAssignment `json:"assignments"`
}

const (
	maxOrganizeDepth   = 4
	maxOrganizeFolders = 40
)

// OrganizeLibrary asks the configured model to propose a folder structure and
// applies it. Existing folders are reused and never renamed or deleted.
func (a *App) OrganizeLibrary(ctx context.Context, kind string) (OrganizeResult, error) {
	if kind != "tool" && kind != "workflow" {
		return OrganizeResult{}, errors.New("kind must be tool or workflow")
	}
	folders, err := a.Store.ListFolders(ctx, kind)
	if err != nil {
		return OrganizeResult{}, err
	}
	paths := folderPathMap(folders)
	items, targets, err := a.organizeItems(ctx, kind, paths)
	if err != nil {
		return OrganizeResult{}, err
	}
	if len(items) == 0 {
		return OrganizeResult{}, fmt.Errorf("there are no %ss to organize", kind)
	}
	_, llmProfile, err := a.ModelProfileForRole(ctx, "planner")
	if err != nil {
		return OrganizeResult{}, err
	}
	temperature := 0.2
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	result := OrganizeResult{Kind: kind, Model: llmProfile.Model, CreatedFolders: []string{}}
	createdSet := map[string]bool{}
	const batchSize = 60
	for start := 0; start < len(items); start += batchSize {
		end := start + batchSize
		if end > len(items) {
			end = len(items)
		}
		batch := items[start:end]
		batchFolders, err := a.Store.ListFolders(ctx, kind)
		if err != nil {
			return result, err
		}
		batchPaths := folderPathMap(batchFolders)
		existing := make([][]string, 0, len(batchFolders))
		for _, folder := range batchFolders {
			existing = append(existing, batchPaths[folder.ID])
		}
		sort.Slice(existing, func(i, j int) bool {
			return pathKey(existing[i]) < pathKey(existing[j])
		})
		payload := map[string]any{
			"kind":             kind,
			"existing_folders": existing,
			"items":            batch,
			"max_depth":        maxOrganizeDepth,
			"max_new_folders":  maxOrganizeFolders,
		}
		raw, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return result, err
		}
		system := `You organize a personal library of agent tools or workflows into folders.
Return only a JSON object, with no markdown and no commentary, in this shape:
{"summary":"one short sentence","assignments":[{"item":"exact item name","folder":["Folder","Subfolder"]}]}
Rules:
- Assign every item exactly once.
- Reuse an existing folder path when it is a good fit. Only create new folders where they reduce clutter.
- Keep the tree shallow: at most 4 levels, at most 40 folders total, and short human-readable names.
- Group by purpose, domain, or workflow intent. Avoid one folder per item.
- Use "Other" for genuine leftovers.
- Item names must match the provided names exactly.
- Never propose deleting or renaming an existing folder.`
		user := "Organize this " + kind + " library using the inventory below.\n\n" + string(raw)
		res, err := a.LLM.Chat(callCtx, llmProfile, llm.ChatRequest{
			Messages: []llm.Message{
				{Role: "system", Content: system},
				{Role: "user", Content: a.Secrets.Redact(user)},
			},
			Temperature: &temperature,
			MaxTokens:   6000,
			SessionID:   ids.New(),
		}, nil)
		if err != nil {
			return result, err
		}
		plan, err := parseOrganizePlan(res.Message.Content)
		if err != nil {
			return result, err
		}
		batchTargets := make(map[string]organizeTarget, len(batch))
		for _, item := range batch {
			batchTargets[item.Name] = targets[item.Name]
		}
		batchResult, err := a.applyOrganizePlan(ctx, kind, plan, batchTargets)
		if err != nil {
			return result, err
		}
		result.AssignedItems += batchResult.AssignedItems
		result.MovedItems += batchResult.MovedItems
		result.UnchangedItems += batchResult.UnchangedItems
		if result.Summary == "" {
			result.Summary = batchResult.Summary
		}
		for _, label := range batchResult.CreatedFolders {
			if createdSet[label] {
				continue
			}
			createdSet[label] = true
			result.CreatedFolders = append(result.CreatedFolders, label)
		}
	}
	if result.Summary == "" {
		result.Summary = fmt.Sprintf("Grouped %d %ss into folders.", result.AssignedItems, kind)
	}
	a.Bus.EmitNoErr(model.Event{Stream: model.StreamSys, Type: "library.organized",
		Payload: a.mustPayload(map[string]any{
			"kind":            kind,
			"summary":         result.Summary,
			"created_folders": result.CreatedFolders,
			"moved_items":     result.MovedItems,
			"model":           result.Model,
		})})
	return result, nil
}

func (a *App) organizeItems(
	ctx context.Context,
	kind string,
	paths map[string][]string,
) ([]organizeItem, map[string]organizeTarget, error) {
	items := []organizeItem{}
	targets := map[string]organizeTarget{}
	if kind == "tool" {
		list, err := a.ToolList(ctx)
		if err != nil {
			return nil, nil, err
		}
		for _, tool := range list {
			status := ""
			if tool.Version != nil {
				status = tool.Version.Status
			}
			items = append(items, organizeItem{
				Name:          tool.Name,
				Kind:          tool.Kind,
				Status:        status,
				Description:   shortenText(tool.Description, 220),
				CurrentFolder: paths[tool.FolderID],
			})
			targets[tool.Name] = organizeTarget{ID: tool.ID, OldFolderID: tool.FolderID}
		}
	} else {
		list, err := a.WorkflowList(ctx)
		if err != nil {
			return nil, nil, err
		}
		for _, wf := range list {
			status := ""
			description := ""
			if wf.Version != nil {
				status = wf.Version.Status
				description = workflowSummary(wf)
			}
			items = append(items, organizeItem{
				Name:          wf.Name,
				Kind:          "workflow",
				Status:        status,
				Description:   shortenText(description, 220),
				CurrentFolder: paths[wf.FolderID],
			})
			targets[wf.Name] = organizeTarget{ID: wf.ID, OldFolderID: wf.FolderID}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, targets, nil
}

func workflowSummary(wf model.WorkflowWithVersion) string {
	if wf.Version == nil {
		return ""
	}
	spec, err := workflow.Parse([]byte(wf.Version.YAML))
	if err != nil {
		return ""
	}
	if strings.TrimSpace(spec.Description) != "" {
		return spec.Description
	}
	parts := make([]string, 0, len(spec.Steps))
	for _, step := range spec.Steps {
		parts = append(parts, step.Summary())
	}
	return fmt.Sprintf("%d steps: %s", len(spec.Steps), strings.Join(parts, " -> "))
}

func parseOrganizePlan(text string) (organizePlan, error) {
	text = stripFences(strings.TrimSpace(text))
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return organizePlan{}, errors.New("the model did not return a folder plan")
	}
	var plan organizePlan
	if err := json.Unmarshal([]byte(text[start:end+1]), &plan); err != nil {
		return organizePlan{}, fmt.Errorf("the model returned an invalid folder plan: %w", err)
	}
	return plan, nil
}

func (a *App) applyOrganizePlan(
	ctx context.Context,
	kind string,
	plan organizePlan,
	targets map[string]organizeTarget,
) (OrganizeResult, error) {
	paths := map[string]organizePath{}
	order := []string{}
	for _, assignment := range plan.Assignments {
		name := strings.TrimSpace(assignment.Item)
		if name == "" {
			continue
		}
		if _, ok := targets[name]; !ok {
			continue
		}
		path, err := cleanOrganizePath(assignment.Folder)
		if err != nil {
			return OrganizeResult{}, fmt.Errorf("invalid folder for %s: %w", name, err)
		}
		if len(path) == 0 {
			continue
		}
		if _, exists := paths[name]; !exists {
			order = append(order, name)
		}
		paths[name] = path
	}
	if len(order) == 0 {
		return OrganizeResult{}, errors.New(
			"the model did not return any usable folder assignments",
		)
	}
	folders, err := a.Store.ListFolders(ctx, kind)
	if err != nil {
		return OrganizeResult{}, err
	}
	pathIndex := map[string]string{}
	folderPaths := folderPathMap(folders)
	for id, path := range folderPaths {
		pathIndex[pathKey(path)] = id
	}
	newFolders := map[string]bool{}
	for _, path := range paths {
		for i := 1; i <= len(path); i++ {
			key := pathKey(path[:i])
			if _, ok := pathIndex[key]; !ok {
				newFolders[key] = true
			}
		}
	}
	if len(newFolders) > maxOrganizeFolders {
		return OrganizeResult{}, fmt.Errorf(
			"the model proposed too many new folders (%d)",
			len(newFolders),
		)
	}

	type move struct {
		name     string
		folderID string
	}
	moves := make([]move, 0, len(order))
	created := []string{}
	createdSet := map[string]bool{}
	for _, name := range order {
		folderID, err := a.ensureOrganizePath(
			ctx,
			kind,
			paths[name],
			pathIndex,
			&created,
			createdSet,
		)
		if err != nil {
			return OrganizeResult{}, err
		}
		moves = append(moves, move{name: name, folderID: folderID})
	}

	result := OrganizeResult{
		Kind:           kind,
		Summary:        shortenText(plan.Summary, 240),
		CreatedFolders: created,
		AssignedItems:  len(moves),
	}
	for _, mv := range moves {
		target := targets[mv.name]
		if target.OldFolderID == mv.folderID {
			result.UnchangedItems++
			continue
		}
		var err error
		if kind == "tool" {
			err = a.SetToolOrganization(ctx, target.ID, &mv.folderID, nil)
		} else {
			err = a.SetWorkflowOrganization(ctx, target.ID, &mv.folderID, nil)
		}
		if err != nil {
			return result, err
		}
		result.MovedItems++
	}
	if result.Summary == "" {
		result.Summary = fmt.Sprintf("Grouped %d %ss into folders.", result.AssignedItems, kind)
	}
	return result, nil
}

func (a *App) ensureOrganizePath(
	ctx context.Context,
	kind string,
	path []string,
	pathIndex map[string]string,
	created *[]string,
	createdSet map[string]bool,
) (string, error) {
	parent := ""
	for i := range path {
		part := path[:i+1]
		key := pathKey(part)
		if id, ok := pathIndex[key]; ok {
			parent = id
			continue
		}
		folder, err := a.CreateFolder(ctx, kind, path[i], parent)
		if err != nil {
			return "", err
		}
		pathIndex[key] = folder.ID
		label := strings.Join(part, " / ")
		if !createdSet[label] {
			createdSet[label] = true
			*created = append(*created, label)
		}
		parent = folder.ID
	}
	return parent, nil
}

func cleanOrganizePath(path organizePath) ([]string, error) {
	if len(path) > maxOrganizeDepth {
		return nil, fmt.Errorf("folder path is deeper than %d levels", maxOrganizeDepth)
	}
	out := make([]string, 0, len(path))
	for _, segment := range path {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return nil, errors.New("folder names cannot be empty")
		}
		if utf8.RuneCountInString(segment) > 80 {
			return nil, errors.New("folder name is longer than 80 characters")
		}
		if strings.ContainsAny(segment, "\r\n") {
			return nil, errors.New("folder names cannot contain line breaks")
		}
		out = append(out, segment)
	}
	return out, nil
}

func folderPathMap(folders []model.Folder) map[string][]string {
	byID := make(map[string]model.Folder, len(folders))
	for _, folder := range folders {
		byID[folder.ID] = folder
	}
	out := make(map[string][]string, len(folders))
	for _, folder := range folders {
		path := []string{}
		seen := map[string]bool{}
		current := folder
		for {
			if seen[current.ID] {
				break
			}
			seen[current.ID] = true
			path = append([]string{current.Name}, path...)
			if current.ParentID == "" {
				break
			}
			parent, ok := byID[current.ParentID]
			if !ok {
				break
			}
			current = parent
		}
		out[folder.ID] = path
	}
	return out
}

func pathKey(path []string) string {
	parts := make([]string, len(path))
	for i, part := range path {
		parts[i] = strings.ToLower(strings.TrimSpace(part))
	}
	return strings.Join(parts, "\x1f")
}

func shortenText(text string, max int) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:max])) + "…"
}
