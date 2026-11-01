package app

import (
	"context"
	"fmt"

	"myagent/internal/model"
	"myagent/internal/tools"
	"myagent/internal/workflow"
)

func (a *App) validateOptions(ctx context.Context, forRun bool) workflow.ValidateOptions {
	toolList, _ := a.Store.ListTools(ctx, model.ToolFilter{})
	toolsMap := map[string]workflow.ToolInfo{}
	for _, t := range toolList {
		if t.Version == nil {
			continue
		}
		m, err := tools.ParseManifest(t.Version.Manifest)
		if err != nil {
			continue
		}
		info := workflow.ToolInfo{
			Name:        t.Name,
			VersionID:   t.Version.ID,
			Status:      t.Version.Status,
			InputSchema: m.InputSchema,
			SideEffect:  m.SideEffect,
			Network:     len(m.Permissions.Network.Domains) > 0,
			SecretNames: secretNames(m.Secrets),
		}
		// Missing secret bindings.
		binds, _ := a.Store.SecretBindings(ctx, t.ID)
		byName := map[string]bool{}
		for _, b := range binds {
			byName[b.LogicalName] = true
		}
		for _, d := range m.Secrets {
			if d.Required && !byName[d.Name] {
				info.MissingSecrets = append(info.MissingSecrets, d.Name)
			}
		}
		toolsMap[t.Name] = info
		versions, _ := a.Store.ToolVersions(ctx, t.ID)
		for _, tv := range versions {
			if tv.Status == model.ToolActive || tv.Status == model.ToolApproved {
				toolsMap[fmt.Sprintf("%s@%d", t.Name, tv.Version)] = info
			}
		}
	}
	wfMap := map[string]bool{}
	wfs, _ := a.Store.ListWorkflows(ctx)
	for _, w := range wfs {
		if w.Version != nil && w.Version.Status == "approved" {
			wfMap[w.Name] = true
			wfMap[fmt.Sprintf("%s@%d", w.Name, w.Version.Version)] = true
		}
	}
	modelMap := map[string]bool{}
	profiles, _ := a.Store.ListModelProfiles(ctx)
	for _, p := range profiles {
		modelMap[p.ID] = true
		modelMap[p.Name] = true
		modelMap[p.Model] = true
	}
	return workflow.ValidateOptions{
		Tools: toolsMap, Workflows: wfMap, Models: modelMap, ForRun: forRun,
	}
}
