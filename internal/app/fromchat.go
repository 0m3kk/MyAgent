package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"myagent/internal/llm"
	"myagent/internal/workflow"
)

type toolCallRecord struct {
	Name   string
	Args   map[string]any
	Result any
	Error  string
}

// SaveChatAsWorkflow turns the successful tool chain from a conversation into a
// validated draft workflow. It uses the planner when available and falls back
// to a deterministic chain so the button still works offline.
func (a *App) SaveChatAsWorkflow(ctx context.Context, convID, requestedName string) (SaveWorkflowResult, error) {
	conv, err := a.Store.GetConversation(ctx, convID)
	if err != nil {
		return SaveWorkflowResult{}, err
	}
	msgs, err := a.Store.ListMessages(ctx, convID)
	if err != nil {
		return SaveWorkflowResult{}, err
	}
	requestText := ""
	calls := []toolCallRecord{}
	seen := map[string]bool{}
	meta := map[string]bool{}
	for _, def := range metaToolDefs() {
		meta[def.Function.Name] = true
	}
	for _, m := range msgs {
		c := m.ContentStruct()
		if m.Role == "user" && c.Text != "" {
			requestText = c.Text
		}
		if m.Role != "assistant" {
			continue
		}
		for _, p := range c.Parts {
			if p.Type != "tool-call" || p.Status != "succeeded" || p.Error != "" {
				continue
			}
			if meta[p.Name] {
				// Meta-tools orchestrate the app; they are not workflow steps.
				continue
			}
			args := map[string]any{}
			_ = json.Unmarshal(p.Args, &args)
			key := p.Name + ":" + string(p.Args)
			if seen[key] {
				continue
			}
			seen[key] = true
			var result any
			_ = json.Unmarshal(p.Result, &result)
			calls = append(calls, toolCallRecord{Name: p.Name, Args: args, Result: result})
		}
	}
	if len(calls) == 0 {
		return SaveWorkflowResult{}, fmt.Errorf("there are no successful tool calls to turn into a workflow yet")
	}
	if requestedName == "" {
		requestedName = slugify(firstNonEmpty(conv.Title, requestText, "chat-workflow"))
	}
	// Try the planner first. It parameterizes and explains better than the
	// offline fallback.
	if profileOK, _ := a.hasPlanner(ctx); profileOK {
		if res, err := a.llmAuthorWorkflow(ctx, conv.ID, requestedName, requestText, calls); err == nil {
			return res, nil
		}
	}
	return a.heuristicWorkflow(ctx, requestedName, calls)
}

func (a *App) hasPlanner(ctx context.Context) (bool, error) {
	p, _, err := a.ModelProfileForRole(ctx, "planner")
	if err != nil {
		return false, err
	}
	return p.BaseURL != "mock://demo" && p.Model != "demo", nil
}

func (a *App) llmAuthorWorkflow(ctx context.Context, sessionID, name, request string, calls []toolCallRecord) (SaveWorkflowResult, error) {
	chain, _ := json.Marshal(calls)
	system := `You are the workflow author mode of MyAgent. Turn one successful chat task into a small reusable workflow.
Rules:
- Return JSON only: {"yaml":"<workflow yaml>"}.
- Use schema: 1 and a kebab-case name.
- Parameterize values that could differ between runs. Never hardcode paths, emails or dates from the conversation.
- Use tool steps with the exact input schemas; you may call describe_tool if needed.
- Insert an approval step before any write or external step.
- Prefer tool and set steps over llm steps unless the task itself requires language work.
- The workflow must be valid and runnable.`
	prompt := fmt.Sprintf("Name: %s\nOriginal request: %s\n\nSuccessful tool calls (in order):\n%s\n\nGenerate the workflow YAML. Keep it minimal.", name, request, string(chain))
	res, err := a.ChatWithTools(ctx, workflow.LLMChatRequest{
		SessionID: sessionID, Role: "planner", System: system, Messages: []llm.Message{{Role: "user", Content: prompt}},
		OutputSchema: map[string]any{"type": "object", "required": []any{"yaml"},
			"properties": map[string]any{"yaml": map[string]any{"type": "string"}}},
	})
	if err != nil {
		return SaveWorkflowResult{}, err
	}
	data, _ := res.Data.(map[string]any)
	yamlText := ""
	if data != nil {
		yamlText, _ = data["yaml"].(string)
	}
	if yamlText == "" {
		var parsed map[string]any
		if json.Unmarshal([]byte(stripFences(res.Text)), &parsed) == nil {
			yamlText, _ = parsed["yaml"].(string)
		}
	}
	if yamlText == "" {
		return SaveWorkflowResult{}, fmt.Errorf("planner returned no YAML")
	}
	// One repair pass with structured diagnostics.
	resSave, err := a.SaveWorkflowDraft(ctx, yamlText, "Saved from chat: "+request, "agent", 0)
	if err == nil && resSave.Validation.OK() {
		return resSave, nil
	}
	diagnostics, _ := json.Marshal(resSave.Validation.Errors)
	repair := fmt.Sprintf("The YAML failed validation. Diagnostics:\n%s\n\nReturn the corrected full YAML as JSON {\"yaml\":\"...\"}.", string(diagnostics))
	res2, err := a.ChatWithTools(ctx, workflow.LLMChatRequest{
		SessionID: sessionID, Role: "planner", System: system,
		Messages: []llm.Message{{Role: "user", Content: prompt}, {Role: "assistant", Content: "```yaml\n" + yamlText + "\n```"}, {Role: "user", Content: repair}},
		OutputSchema: map[string]any{"type": "object", "required": []any{"yaml"},
			"properties": map[string]any{"yaml": map[string]any{"type": "string"}}},
	})
	if err != nil {
		return resSave, err
	}
	data2, _ := res2.Data.(map[string]any)
	fixed := ""
	if data2 != nil {
		fixed, _ = data2["yaml"].(string)
	}
	if fixed == "" {
		return resSave, fmt.Errorf("planner could not repair the workflow")
	}
	return a.SaveWorkflowDraft(ctx, fixed, "Saved from chat: "+request, "agent", 0)
}

func (a *App) heuristicWorkflow(ctx context.Context, name string, calls []toolCallRecord) (SaveWorkflowResult, error) {
	base := slugify(name)
	unique := base
	for i := 2; ; i++ {
		if _, err := a.Store.GetWorkflowByName(ctx, unique); err != nil {
			break
		}
		unique = fmt.Sprintf("%s-%d", base, i)
	}
	spec := &workflow.Spec{Schema: 1, Name: unique,
		Description: "Draft created from a chat session. Review inputs and approvals before enabling it.",
		Settings:    workflow.Settings{Unattended: "ask"},
		Steps:       []workflow.Step{}}
	prev := ""
	approvalCount := 0
	for i, call := range calls {
		binding, err := a.Resolve(ctx, call.Name)
		if err != nil {
			return SaveWorkflowResult{}, err
		}
		needs := []string{}
		if prev != "" {
			needs = []string{prev}
		}
		risky := binding.SideEffect == "write" || binding.SideEffect == "external"
		prevForTool := prev
		if risky {
			approvalCount++
			aid := fmt.Sprintf("approve_%d", approvalCount)
			approval := workflow.Step{ID: aid, Needs: needs,
				Approval: &workflow.ApprovalSpec{Message: "Allow " + call.Name + " to run?", Timeout: "24h"}}
			if prev != "" {
				approval.Approval.Show = "${steps." + prev + ".output}"
			}
			spec.Steps = append(spec.Steps, approval)
			prevForTool = aid
		}
		stepID := fmt.Sprintf("step_%d", i+1)
		needs = []string{}
		if prevForTool != "" {
			needs = []string{prevForTool}
		}
		st := workflow.Step{ID: stepID, Needs: needs, Tool: call.Name, With: call.Args}
		if risky {
			st.SideEffect = binding.SideEffect
		}
		spec.Steps = append(spec.Steps, st)
		prev = stepID
	}
	if prev != "" {
		spec.Outputs = map[string]string{"result": "${steps." + prev + ".output}"}
	}
	raw, err := yaml.Marshal(spec)
	if err != nil {
		return SaveWorkflowResult{}, err
	}
	return a.SaveWorkflowDraft(ctx, string(raw), "Draft created from a chat session", "agent", 0)
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "chat-workflow"
	}
	if len(out) > 48 {
		out = strings.Trim(out[:48], "-")
	}
	if out[0] < 'a' || out[0] > 'z' {
		out = "wf-" + out
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
