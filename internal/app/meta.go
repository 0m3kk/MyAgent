package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"myagent/internal/llm"
	"myagent/internal/model"
	"myagent/internal/tools"
	"myagent/internal/workflow"
)

func toolDef(name, description string, props map[string]any, required ...string) llm.ToolDef {
	params := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		req := make([]any, len(required))
		for i, r := range required {
			req[i] = r
		}
		params["required"] = req
	}
	return llm.ToolDef{
		Type:     "function",
		Function: llm.FunctionDef{Name: name, Description: description, Parameters: params},
	}
}

func metaToolDefs() []llm.ToolDef {
	return []llm.ToolDef{
		toolDef("search_tools", "Search approved tools by name or description.", map[string]any{
			"query": map[string]any{"type": "string", "description": "Search words"},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum results",
				"default":     10,
			},
		}, "query"),
		toolDef(
			"describe_tool",
			"Get the full input/output schema, permissions, secrets and tests of a tool. Never guess tool schemas.",
			map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Tool name, e.g. fs.list",
				},
				"version": map[string]any{
					"type":        "integer",
					"description": "Optional pinned version",
				},
			},
			"name",
		),
		toolDef(
			"list_workflows",
			"List workflows with current version, status and inputs.",
			map[string]any{},
		),
		toolDef(
			"get_workflow",
			"Get a workflow YAML and its validation diagnostics.",
			map[string]any{
				"name":    map[string]any{"type": "string", "description": "Workflow name"},
				"version": map[string]any{"type": "integer", "description": "Optional version"},
			},
			"name",
		),
		toolDef(
			"delete_workflow",
			"Delete a workflow, its versions and triggers. Always shows an approval card; only call when the user explicitly asks to delete it.",
			map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Workflow name as returned by list_workflows",
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "Short reason shown on the approval card",
				},
			},
			"name",
		),
		toolDef(
			"validate_workflow",
			"Validate workflow YAML without saving it. Returns E-code diagnostics.",
			map[string]any{
				"yaml": map[string]any{"type": "string", "description": "Workflow YAML"},
			},
			"yaml",
		),
		toolDef(
			"save_workflow_draft",
			"Save workflow YAML as a new draft after validation. E-code errors prevent saving. Never approves or activates.",
			map[string]any{
				"yaml": map[string]any{"type": "string", "description": "Workflow YAML"},
				"base_version": map[string]any{
					"type":        "integer",
					"description": "Required when editing an existing workflow; prevents overwriting newer changes",
				},
				"change_note": map[string]any{
					"type":        "string",
					"description": "Short note describing the change",
				},
			},
			"yaml",
		),
		toolDef(
			"dry_run_workflow",
			"Run a workflow with side effects mocked and approvals auto-accepted. Returns a run summary.",
			map[string]any{
				"name":    map[string]any{"type": "string", "description": "Workflow name"},
				"version": map[string]any{"type": "integer", "description": "Optional version"},
				"inputs":  map[string]any{"type": "object", "description": "Workflow inputs"},
			},
			"name",
			"inputs",
		),
		toolDef(
			"run_workflow",
			"Start a real workflow run asynchronously. Returns a run_id.",
			map[string]any{
				"name":    map[string]any{"type": "string", "description": "Workflow name"},
				"version": map[string]any{"type": "integer", "description": "Optional version"},
				"inputs":  map[string]any{"type": "object", "description": "Workflow inputs"},
			},
			"name",
			"inputs",
		),
		toolDef("get_run", "Get the status, steps, costs and errors of a run.", map[string]any{
			"run_id": map[string]any{"type": "string", "description": "Run ID"},
		}, "run_id"),
		toolDef("wait_run", "Wait for a run to finish, then return its summary.", map[string]any{
			"run_id": map[string]any{"type": "string", "description": "Run ID"},
			"timeout_s": map[string]any{
				"type":        "integer",
				"description": "Maximum wait in seconds",
				"default":     60,
			},
		}, "run_id"),
		toolDef("list_runs", "List recent runs, optionally filtered.", map[string]any{
			"workflow": map[string]any{"type": "string", "description": "Workflow ID or name"},
			"status":   map[string]any{"type": "string", "description": "Run status"},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum results",
				"default":     20,
			},
		}),
		toolDef("read_run", "Read a run's events and step details for diagnosis.", map[string]any{
			"run_id": map[string]any{"type": "string", "description": "Run ID"},
		}, "run_id"),
		toolDef(
			"compare_runs",
			"Compare two runs: status, cost, latency and failed steps.",
			map[string]any{
				"run_a": map[string]any{"type": "string", "description": "First run ID"},
				"run_b": map[string]any{"type": "string", "description": "Second run ID"},
			},
			"run_a",
			"run_b",
		),
		toolDef(
			"list_secrets",
			"List secret NAMES and status only. Values are never available.",
			map[string]any{},
		),
		toolDef(
			"get_environment",
			"Get workspace path, date, runtime availability and current model profiles.",
			map[string]any{},
		),
		toolDef(
			"create_tool_draft",
			"Create a new draft tool from a complete manifest YAML. Use write_tool_file for implementation files.",
			map[string]any{
				"manifest_yaml": map[string]any{
					"type":        "string",
					"description": "Tool manifest YAML",
				},
				"base_version": map[string]any{
					"type":        "integer",
					"description": "Required when editing an existing tool",
				},
			},
			"manifest_yaml",
		),
		toolDef(
			"delete_tool",
			"Delete a tool, its versions and secret bindings. Always shows an approval card; only call when the user explicitly asks to delete it.",
			map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Tool name as returned by search_tools",
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "Short reason shown on the approval card",
				},
			},
			"name",
		),
		toolDef(
			"write_tool_file",
			"Write a file inside a draft tool version. Never touches files outside the tool directory.",
			map[string]any{
				"tool":       map[string]any{"type": "string", "description": "Tool name"},
				"version_id": map[string]any{"type": "string", "description": "Draft version ID"},
				"path":       map[string]any{"type": "string", "description": "Relative file path"},
				"content":    map[string]any{"type": "string", "description": "File content"},
			},
			"tool",
			"version_id",
			"path",
			"content",
		),
		toolDef("read_tool_file", "Read a file inside a tool version.", map[string]any{
			"tool":       map[string]any{"type": "string", "description": "Tool name"},
			"version_id": map[string]any{"type": "string", "description": "Version ID"},
			"path":       map[string]any{"type": "string", "description": "Relative file path"},
		}, "tool", "version_id", "path"),
		toolDef("list_tool_files", "List files in a tool version.", map[string]any{
			"tool":       map[string]any{"type": "string", "description": "Tool name"},
			"version_id": map[string]any{"type": "string", "description": "Version ID"},
		}, "tool", "version_id"),
		toolDef(
			"run_tool_tests",
			"Run a tool's manifest tests in the sandbox and report each result.",
			map[string]any{
				"tool":       map[string]any{"type": "string", "description": "Tool name"},
				"version_id": map[string]any{"type": "string", "description": "Version ID"},
			},
			"tool",
			"version_id",
		),
		toolDef(
			"probe_permissions",
			"Run tests in learning mode and report denied resources.",
			map[string]any{
				"tool":       map[string]any{"type": "string", "description": "Tool name"},
				"version_id": map[string]any{"type": "string", "description": "Version ID"},
			},
			"tool",
			"version_id",
		),
		toolDef(
			"request_tool_approval",
			"Show the user an approval card. The user decides; the agent can never activate a tool itself.",
			map[string]any{
				"tool":       map[string]any{"type": "string", "description": "Tool name"},
				"version_id": map[string]any{"type": "string", "description": "Version ID"},
				"summary": map[string]any{
					"type":        "string",
					"description": "Plain-language summary of access and behavior",
				},
			},
			"tool",
			"version_id",
			"summary",
		),
		toolDef(
			"ask_user",
			"Ask exactly one short question when a wrong guess is costly.",
			map[string]any{
				"question": map[string]any{"type": "string", "description": "Question"},
				"kind": map[string]any{
					"type":        "string",
					"enum":        []any{"text", "choice", "confirm"},
					"description": "Question kind",
				},
				"options": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Choices for kind=choice",
				},
			},
			"question",
			"kind",
		),
		toolDef(
			"remember",
			"Save a durable note about the user's preferences or workspace conventions.",
			map[string]any{
				"note": map[string]any{"type": "string", "description": "Short note"},
			},
			"note",
		),
		toolDef(
			"propose_trigger",
			"Propose a schedule, file watcher or local webhook. The user must approve it.",
			map[string]any{
				"workflow": map[string]any{"type": "string", "description": "Workflow name"},
				"kind": map[string]any{
					"type":        "string",
					"enum":        []any{"cron", "file", "webhook"},
					"description": "Trigger kind",
				},
				"config": map[string]any{
					"type":        "object",
					"description": "cron: {schedule}; file: {paths}; webhook: {token}",
				},
				"inputs": map[string]any{"type": "object", "description": "Fixed workflow inputs"},
			},
			"workflow",
			"kind",
			"config",
		),
	}
}

func (a *App) executeMetaTool(
	ctx context.Context,
	convID, name string,
	args map[string]any,
) (any, error) {
	switch name {
	case "search_tools":
		return a.metaSearchTools(ctx, str(args, "query"), intg(args, "limit", 10))
	case "describe_tool":
		return a.metaDescribeTool(ctx, str(args, "name"), intg(args, "version", 0))
	case "list_workflows":
		list, err := a.WorkflowList(ctx)
		if err != nil {
			return nil, err
		}
		out := []map[string]any{}
		for _, w := range list {
			item := map[string]any{"name": w.Name, "id": w.ID}
			if w.Version != nil {
				item["version"] = w.Version.Version
				item["status"] = w.Version.Status
				item["description"] = ""
				if spec, err := workflow.Parse([]byte(w.Version.YAML)); err == nil {
					item["description"] = spec.Description
					inputs := []string{}
					for k := range spec.Inputs {
						inputs = append(inputs, k)
					}
					item["inputs"] = inputs
				}
			}
			out = append(out, item)
		}
		return map[string]any{
			"ok":        true,
			"workflows": out,
			"next_hint": "Use get_workflow for the full YAML.",
		}, nil
	case "get_workflow":
		w, v, spec, err := a.ResolveWorkflowVersion(ctx, str(args, "name"), "", "manual")
		if err != nil {
			return nil, err
		}
		if ver := intg(args, "version", 0); ver > 0 {
			v, _ = a.Store.GetWorkflowVersionByNumber(ctx, w.ID, ver)
			spec, _ = workflow.Parse([]byte(v.YAML))
		}
		val := workflow.Validate(spec, a.validateOptions(ctx, false))
		return map[string]any{"ok": true, "name": w.Name, "version_id": v.ID, "version": v.Version,
			"yaml": v.YAML, "diagnostics": val.Errors, "warnings": val.Warnings,
			"next_hint": "Edit the YAML, validate, then save_workflow_draft."}, nil
	case "delete_workflow":
		return a.metaDeleteWorkflow(ctx, convID, str(args, "name"), str(args, "reason"))
	case "validate_workflow":
		spec, res, perr := a.ValidateWorkflowYAML(ctx, str(args, "yaml"), false)
		if perr != nil {
			return map[string]any{
				"ok":          false,
				"diagnostics": res.Errors,
				"next_hint":   "Fix YAML syntax first.",
			}, nil
		}
		return map[string]any{"ok": res.OK(), "name": spec.Name, "diagnostics": res.Errors,
			"warnings": res.Warnings, "next_hint": "Fix every E-code. W-codes are advice."}, nil
	case "save_workflow_draft":
		base := intg(args, "base_version", 0)
		res, err := a.SaveWorkflowDraft(
			ctx,
			str(args, "yaml"),
			str(args, "change_note"),
			"agent",
			base,
		)
		out := map[string]any{
			"saved":       err == nil && res.Validation.OK(),
			"diagnostics": res.Validation.Errors,
			"warnings":    res.Validation.Warnings,
		}
		if res.Version.ID != "" {
			out["version_id"] = res.Version.ID
			out["version"] = res.Version.Version
			out["workflow_id"] = res.Workflow.ID
			out["next_hint"] = "Draft saved. Ask the user to review it, or dry-run with realistic inputs."
		} else {
			out["next_hint"] = "Fix the diagnostics and call save_workflow_draft again (max 5 attempts)."
		}
		if err != nil && len(res.Validation.Errors) == 0 {
			return out, err
		}
		return out, nil
	case "dry_run_workflow":
		inputs := object(args, "inputs")
		run, err := a.RunWorkflow(
			ctx,
			str(args, "name"),
			"",
			inputs,
			"chat",
			convID,
			true,
			"ask",
			"",
		)
		if err != nil {
			return nil, err
		}
		run, _ = a.waitForRun(ctx, run.ID, 120*time.Second)
		return a.runSummary(ctx, run), nil
	case "run_workflow":
		inputs := object(args, "inputs")
		versionID := ""
		if v := intg(args, "version", 0); v > 0 {
			w, err := a.Store.GetWorkflowByName(ctx, str(args, "name"))
			if err != nil {
				return nil, err
			}
			wv, err := a.Store.GetWorkflowVersionByNumber(ctx, w.ID, v)
			if err != nil {
				return nil, err
			}
			versionID = wv.ID
		}
		run, err := a.RunWorkflow(
			ctx,
			str(args, "name"),
			versionID,
			inputs,
			"chat",
			convID,
			false,
			"ask",
			"",
		)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "run_id": run.ID, "status": run.Status,
			"next_hint": "Use get_run or wait_run to follow progress."}, nil
	case "get_run":
		run, err := a.Store.GetRun(ctx, str(args, "run_id"))
		if err != nil {
			return nil, err
		}
		return a.runSummary(ctx, run), nil
	case "wait_run":
		run, err := a.Store.GetRun(ctx, str(args, "run_id"))
		if err != nil {
			return nil, err
		}
		run, _ = a.waitForRun(ctx, run.ID, time.Duration(intg(args, "timeout_s", 60))*time.Second)
		return a.runSummary(ctx, run), nil
	case "list_runs":
		filter := model.RunFilter{Status: str(args, "status"), Limit: intg(args, "limit", 20)}
		if wf := str(args, "workflow"); wf != "" {
			if w, err := a.Store.GetWorkflowByName(ctx, wf); err == nil {
				filter.TargetID = w.ID
			} else {
				filter.TargetID = wf
			}
		}
		runs, err := a.Store.ListRuns(ctx, filter)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "runs": runs}, nil
	case "read_run":
		run, err := a.Store.GetRun(ctx, str(args, "run_id"))
		if err != nil {
			return nil, err
		}
		events, _ := a.Store.RunEvents(ctx, run.ID, 0, 400)
		summary := a.runSummary(ctx, run)
		evs := []map[string]any{}
		for _, e := range events {
			if e.Type == "step.output.delta" || e.Type == "llm.delta" {
				continue
			}
			evs = append(
				evs,
				map[string]any{
					"type":    e.Type,
					"step":    e.StepID,
					"ts":      e.TS,
					"payload": previewJSON(e.Payload, 1000),
				},
			)
		}
		summary["events"] = evs
		return summary, nil
	case "compare_runs":
		a1, err := a.Store.GetRun(ctx, str(args, "run_a"))
		if err != nil {
			return nil, err
		}
		b1, err := a.Store.GetRun(ctx, str(args, "run_b"))
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"ok": true,
			"a":  a.runSummary(ctx, a1),
			"b":  a.runSummary(ctx, b1),
		}, nil
	case "list_secrets":
		list, err := a.Secrets.List(ctx)
		if err != nil {
			return nil, err
		}
		out := []map[string]any{}
		for _, s := range list {
			out = append(
				out,
				map[string]any{
					"name":         s.Name,
					"kind":         s.Kind,
					"status":       s.Status,
					"used_by":      s.UsedBy,
					"ask_each_run": s.AskEachRun,
				},
			)
		}
		return map[string]any{
			"ok":        true,
			"secrets":   out,
			"next_hint": "Values are never available to the agent.",
		}, nil
	case "get_environment":
		profiles, _ := a.Store.ListModelProfiles(ctx)
		mods := []map[string]any{}
		for _, p := range profiles {
			mods = append(
				mods,
				map[string]any{
					"name":     p.Name,
					"model":    p.Model,
					"role":     p.Role,
					"base_url": p.BaseURL,
				},
			)
		}
		bins := map[string]string{}
		for _, b := range []string{"python3", "node", "git", "rg", "jq"} {
			if p, err := exec.LookPath(b); err == nil {
				bins[b] = p
			}
		}
		return map[string]any{
			"ok":        true,
			"workspace": a.Workspace.RootPath,
			"date":      time.Now().Format(time.RFC3339),
			"os":        runtime.GOOS,
			"arch":      runtime.GOARCH,
			"runtimes":  bins,
			"models":    mods,
			"settings":  a.GetSettings(),
		}, nil
	case "create_tool_draft":
		res, err := a.CreateToolDraft(
			ctx,
			str(args, "manifest_yaml"),
			"agent",
			intg(args, "base_version", 0),
		)
		out := map[string]any{"ok": err == nil, "issues": res.Issues}
		if res.Version.ID != "" {
			out["tool"] = res.Tool.Name
			out["version_id"] = res.Version.ID
			out["version"] = res.Version.Version
			out["next_hint"] = "Write implementation files, then run_tool_tests."
		} else {
			out["next_hint"] = "Fix manifest issues and try again."
		}
		return out, nil
	case "delete_tool":
		return a.metaDeleteTool(ctx, convID, str(args, "name"), str(args, "reason"))
	case "write_tool_file":
		t, err := a.Store.GetToolByName(ctx, str(args, "tool"))
		if err != nil {
			return nil, err
		}
		tv, err := a.WriteToolFile(
			ctx,
			t.ID,
			str(args, "version_id"),
			str(args, "path"),
			str(args, "content"),
		)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"ok":         true,
			"version_id": tv.ID,
			"hash":       tv.ContentHash,
			"next_hint":  "Run tests next.",
		}, nil
	case "read_tool_file":
		t, err := a.Store.GetToolByName(ctx, str(args, "tool"))
		if err != nil {
			return nil, err
		}
		content, err := a.ReadToolFile(ctx, t.ID, str(args, "version_id"), str(args, "path"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "content": content}, nil
	case "list_tool_files":
		t, err := a.Store.GetToolByName(ctx, str(args, "tool"))
		if err != nil {
			return nil, err
		}
		tv, err := a.Store.GetToolVersion(ctx, str(args, "version_id"))
		if err != nil {
			return nil, err
		}
		_ = t
		return map[string]any{"ok": true, "files": listCodeFiles(tv.CodePath)}, nil
	case "run_tool_tests":
		t, err := a.Store.GetToolByName(ctx, str(args, "tool"))
		if err != nil {
			return nil, err
		}
		results, err := a.ToolTest(ctx, t.ID, str(args, "version_id"))
		if err != nil {
			return nil, err
		}
		passed := len(results) > 0
		for _, r := range results {
			if !r.Passed {
				passed = false
			}
		}
		return map[string]any{
			"ok":        passed,
			"tests":     results,
			"next_hint": "Fix failures, or request_tool_approval when green.",
		}, nil
	case "probe_permissions":
		t, err := a.Store.GetToolByName(ctx, str(args, "tool"))
		if err != nil {
			return nil, err
		}
		results, err := a.ToolTest(ctx, t.ID, str(args, "version_id"))
		if err != nil {
			return nil, err
		}
		denied := []string{}
		for _, r := range results {
			for _, l := range r.Logs {
				if strings.Contains(strings.ToLower(l), "permission") ||
					strings.Contains(strings.ToLower(l), "denied") {
					denied = append(denied, l)
				}
			}
		}
		return map[string]any{"ok": true, "denied": denied, "tests": results,
			"next_hint": "Only widen permissions if a denied access is truly required."}, nil
	case "request_tool_approval":
		t, err := a.Store.GetToolByName(ctx, str(args, "tool"))
		if err != nil {
			return nil, err
		}
		tv, err := a.Store.GetToolVersion(ctx, str(args, "version_id"))
		if err != nil {
			return nil, err
		}
		m, _ := tools.ParseManifest(tv.Manifest)
		payload := map[string]any{"tool": t.Name, "version_id": tv.ID, "version": tv.Version,
			"summary": str(
				args,
				"summary",
			), "side_effect": m.SideEffect, "permissions": m.Permissions,
			"secrets": m.Secrets, "tests": m.Tests}
		d, err := a.askApproval(ctx, workflow.ApprovalRequest{
			ConversationID: convID, Kind: "tool", Title: "Approve tool " + t.Name + "?",
			Description: str(
				args,
				"summary",
			), Source: "chat", ExpiresAt: time.Now().Add(2 * time.Hour),
		}, payload)
		if err != nil {
			return nil, err
		}
		if !d.Approved {
			return map[string]any{
				"ok":        false,
				"status":    "rejected",
				"next_hint": "Do not retry the approval without changes.",
			}, nil
		}
		if err := a.ApproveToolVersion(ctx, t.ID, tv.ID); err != nil {
			return nil, err
		}
		return map[string]any{
			"ok":        true,
			"status":    "active",
			"next_hint": "The tool is active and can be used in workflows.",
		}, nil
	case "ask_user":
		answer, err := a.AskUser(
			ctx,
			convID,
			str(args, "question"),
			str(args, "kind"),
			stringsArg(args, "options"),
		)
		if err != nil {
			return map[string]any{"ok": false, "answer": "", "error": err.Error()}, nil
		}
		return map[string]any{"ok": true, "answer": answer}, nil
	case "remember":
		if err := a.Store.AddNote(ctx, a.Workspace.ID, str(args, "note")); err != nil {
			return nil, err
		}
		a.Bus.EmitNoErr(
			model.Event{
				Stream:  model.StreamSys,
				Type:    "note.created",
				Payload: a.mustPayload(map[string]any{"note": str(args, "note")}),
			},
		)
		return map[string]any{
			"ok":        true,
			"next_hint": "The note is visible in Settings and workspace prompts.",
		}, nil
	case "propose_trigger":
		return a.metaProposeTrigger(ctx, convID, args)
	}
	return nil, fmt.Errorf("unknown meta tool %s", name)
}

// metaDeleteWorkflow and metaDeleteTool are irreversible, so they block on an
// approval card before touching the store. The approval kind is "delete" and
// the payload carries enough context for the UI to show what will disappear.
func (a *App) metaDeleteWorkflow(ctx context.Context, convID, name, reason string) (any, error) {
	w, err := a.Store.GetWorkflowByName(ctx, name)
	if err != nil {
		return nil, err
	}
	versions, _ := a.Store.WorkflowVersions(ctx, w.ID)
	latest := 0
	status := ""
	if len(versions) > 0 {
		latest = versions[0].Version
		status = versions[0].Status
	}
	payload := map[string]any{
		"action": "delete", "object": "workflow", "workflow": w.Name, "workflow_id": w.ID,
		"versions": len(versions), "version": latest, "status": status,
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		payload["reason"] = reason
	}
	title := "Delete workflow " + w.Name + "?"
	description := "This permanently deletes the workflow, its versions and triggers. Past runs stay in history."
	payload["title"] = title
	payload["description"] = description
	d, err := a.askApproval(ctx, workflow.ApprovalRequest{
		ConversationID: convID,
		Kind:           "delete",
		Title:          title,
		Description:    description,
		Source:         "chat",
		ExpiresAt:      time.Now().Add(30 * time.Minute),
	}, payload)
	if err != nil {
		return nil, err
	}
	if !d.Approved {
		return map[string]any{
			"ok": false, "status": "rejected", "workflow": w.Name,
			"next_hint": "The user denied the deletion. Do not retry unless they ask again.",
		}, nil
	}
	if err := a.Store.DeleteWorkflow(ctx, w.ID); err != nil {
		return nil, err
	}
	return map[string]any{
		"ok": true, "deleted": true, "workflow": w.Name, "workflow_id": w.ID,
		"next_hint": "Workflow deleted. Past runs remain in history.",
	}, nil
}

func (a *App) metaDeleteTool(ctx context.Context, convID, name, reason string) (any, error) {
	t, err := a.Store.GetToolByName(ctx, name)
	if err != nil {
		return nil, err
	}
	versions, _ := a.Store.ToolVersions(ctx, t.ID)
	latest := 0
	status := ""
	if len(versions) > 0 {
		latest = versions[0].Version
		status = versions[0].Status
	}
	payload := map[string]any{
		"action": "delete", "object": "tool", "tool": t.Name, "tool_id": t.ID, "tool_kind": t.Kind,
		"versions": len(versions), "version": latest, "status": status,
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		payload["reason"] = reason
	}
	title := "Delete tool " + t.Name + "?"
	description := "This permanently deletes the tool, its versions and secret bindings."
	payload["title"] = title
	payload["description"] = description
	d, err := a.askApproval(ctx, workflow.ApprovalRequest{
		ConversationID: convID,
		Kind:           "delete",
		Title:          title,
		Description:    description,
		Source:         "chat",
		ExpiresAt:      time.Now().Add(30 * time.Minute),
	}, payload)
	if err != nil {
		return nil, err
	}
	if !d.Approved {
		return map[string]any{
			"ok": false, "status": "rejected", "tool": t.Name,
			"next_hint": "The user denied the deletion. Do not retry unless they ask again.",
		}, nil
	}
	if err := a.Store.DeleteTool(ctx, t.ID); err != nil {
		return nil, err
	}
	return map[string]any{
		"ok": true, "deleted": true, "tool": t.Name, "tool_id": t.ID,
		"next_hint": "Tool deleted. Workflows that referenced it will need another tool.",
	}, nil
}

func (a *App) metaSearchTools(ctx context.Context, query string, limit int) (any, error) {
	list, err := a.ToolList(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	words := strings.Fields(strings.ToLower(query))
	out := []map[string]any{}
	for _, t := range list {
		if t.Version == nil {
			continue
		}
		hay := strings.ToLower(t.Name + " " + t.Description)
		match := len(words) == 0
		for _, w := range words {
			if strings.Contains(hay, w) {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		item := map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"status":      t.Version.Status,
			"side_effect": t.SideEffect,
			"version":     t.Version.Version,
		}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	return map[string]any{
		"ok":        true,
		"tools":     out,
		"next_hint": "Call describe_tool before using or referencing a tool.",
	}, nil
}

func (a *App) metaDescribeTool(ctx context.Context, name string, version int) (any, error) {
	t, err := a.Store.GetToolByName(ctx, name)
	if err != nil {
		return nil, err
	}
	var tv model.ToolVersion
	if version > 0 {
		tv, err = a.Store.GetToolVersionByNumber(ctx, t.ID, version)
	} else if t.CurrentVersionID != "" {
		tv, err = a.Store.GetToolVersion(ctx, t.CurrentVersionID)
	} else {
		versions, _ := a.Store.ToolVersions(ctx, t.ID)
		if len(versions) == 0 {
			return nil, fmt.Errorf("tool has no versions")
		}
		tv = versions[0]
	}
	if err != nil {
		return nil, err
	}
	m, err := tools.ParseManifest(tv.Manifest)
	if err != nil {
		return nil, err
	}
	secrets := []map[string]any{}
	binds, _ := a.Store.SecretBindings(ctx, t.ID)
	bound := map[string]string{}
	for _, b := range binds {
		bound[b.LogicalName] = b.SecretID
	}
	for _, d := range m.Secrets {
		status := "missing"
		if bound[d.Name] != "" {
			status = "bound"
		}
		secrets = append(secrets, map[string]any{"name": d.Name, "description": d.Description,
			"inject": d.Inject, "required": d.Required, "status": status, "scope": d.Scope})
	}
	return map[string]any{
		"ok":            true,
		"name":          m.Name,
		"description":   m.Description,
		"kind":          m.Kind,
		"version":       tv.Version,
		"version_id":    tv.ID,
		"status":        tv.Status,
		"side_effect":   m.SideEffect,
		"input_schema":  m.InputSchema,
		"output_schema": m.OutputSchema,
		"permissions":   m.Permissions,
		"secrets":       secrets,
		"limits":        m.Limits,
		"tests":         m.Tests,
		"next_hint":     "Use the exact input schema when calling this tool or writing a workflow.",
	}, nil
}

func (a *App) waitForRun(
	ctx context.Context,
	runID string,
	timeout time.Duration,
) (model.Run, error) {
	deadline := time.Now().Add(timeout)
	for {
		run, err := a.Store.GetRun(ctx, runID)
		if err != nil {
			return run, err
		}
		if model.RunTerminal(run.Status) {
			return run, nil
		}
		if time.Now().After(deadline) {
			return run, nil
		}
		select {
		case <-ctx.Done():
			return run, ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func (a *App) runSummary(ctx context.Context, run model.Run) map[string]any {
	steps := []map[string]any{}
	for _, s := range run.Steps {
		steps = append(
			steps,
			map[string]any{"step_id": s.StepID, "attempt": s.Attempt, "status": s.Status,
				"error": s.Error, "started_at": s.StartedAt, "finished_at": s.FinishedAt},
		)
	}
	return map[string]any{
		"ok":          true,
		"run_id":      run.ID,
		"target":      run.TargetKind,
		"name":        run.TargetName,
		"status":      run.Status,
		"source":      run.Source,
		"error":       run.Error,
		"steps":       steps,
		"tokens_in":   run.TokensIn,
		"tokens_out":  run.TokensOut,
		"cost_micro":  run.CostMicro,
		"started_at":  run.StartedAt,
		"finished_at": run.FinishedAt,
		"next_hint":   "If the run failed, call read_run to inspect events and explain the likely cause.",
	}
}

func previewJSON(raw json.RawMessage, n int) any {
	if len(raw) <= n {
		var v any
		if json.Unmarshal(raw, &v) == nil {
			return v
		}
		return string(raw)
	}
	return string(raw[:n]) + "…"
}

func str(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	if v, ok := args[key]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

func intg(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	case string:
		if n, err := strconv.Atoi(x); err == nil {
			return n
		}
	}
	return def
}

func object(args map[string]any, key string) map[string]any {
	if v, ok := args[key].(map[string]any); ok {
		return v
	}
	return map[string]any{}
}

func stringsArg(args map[string]any, key string) []string {
	out := []string{}
	if raw, ok := args[key].([]any); ok {
		for _, v := range raw {
			out = append(out, fmt.Sprint(v))
		}
	}
	return out
}
