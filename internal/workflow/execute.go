package workflow

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"myagent/internal/llm"
	"myagent/internal/model"
)

func (ex *executor) runKind(ctx context.Context, st *Step, path string, env *Env, depth int, sr model.StepRun, siblings []Step) (any, error) {
	switch st.StepKind() {
	case "tool":
		return ex.runTool(ctx, st, path, env, sr)
	case "llm":
		return ex.runLLM(ctx, st, path, env)
	case "agent":
		return ex.runAgent(ctx, st, path, env)
	case "set":
		return ex.runSet(st, env)
	case "branch":
		return ex.runBranch(ctx, st, path, env, depth)
	case "map":
		return ex.runMap(ctx, st, path, env, depth)
	case "parallel":
		return ex.runParallel(ctx, st, path, env, depth)
	case "approval":
		return ex.runApproval(ctx, st, path, env, sr.ID)
	case "workflow":
		return ex.runSubworkflow(ctx, st, path, env, depth)
	}
	return nil, fmt.Errorf("unsupported step type %q", st.StepKind())
}

func (ex *executor) resolveBinding(ctx context.Context, ref string) (ToolBinding, error) {
	if b, ok := ex.req.Bindings[ref]; ok && b.VersionID != "" {
		return b, nil
	}
	if ex.e.Tools == nil {
		return ToolBinding{}, errors.New("tool runner is not configured")
	}
	b, err := ex.e.Tools.Resolve(ctx, ref)
	if err != nil {
		return b, err
	}
	_ = ex.e.Store.AddRunBinding(ctx, ex.req.RunID, "tool", ref, b.VersionID, b.ContentHash)
	return b, nil
}

func (ex *executor) runTool(ctx context.Context, st *Step, path string, env *Env, sr model.StepRun) (any, error) {
	argsVal, err := EvalValue(st.With, env)
	if err != nil {
		return nil, fmt.Errorf("tool arguments: %w", err)
	}
	args := map[string]any{}
	if argsVal != nil {
		m, ok := argsVal.(map[string]any)
		if !ok {
			return nil, errors.New("tool arguments must be an object")
		}
		args = m
	}
	binding, err := ex.resolveBinding(ctx, st.Tool)
	if err != nil {
		return nil, err
	}
	effective := st.SideEffect
	if sideEffectRank(effective) < sideEffectRank(binding.SideEffect) {
		effective = binding.SideEffect
	}
	ex.emit(ctx, "tool.call.started", path, map[string]any{
		"tool": binding.Name, "tool_version_id": binding.VersionID, "args": args, "side_effect": effective,
	}, nil)
	if ex.req.Replay != nil {
		if v, ok := ex.req.Replay[path]; ok {
			ex.emit(ctx, "tool.call.finished", path, map[string]any{
				"tool": binding.Name, "replayed": true, "duration_ms": 0,
			}, nil)
			return v, nil
		}
	}
	if ex.req.DryRun && (effective == "write" || effective == "external") {
		out := map[string]any{"mocked": true, "tool": binding.Name, "args": args, "reason": "dry run"}
		ex.emit(ctx, "tool.call.finished", path, map[string]any{"tool": binding.Name, "mocked": true, "duration_ms": 0}, nil)
		return out, nil
	}
	start := time.Now()
	output, err := ex.e.Tools.Execute(ctx, binding, args, ToolExecOptions{
		RunID:     ex.req.RunID,
		StepID:    path,
		StepRunID: sr.ID,
		IdemKey:   ex.req.RunID + ":" + path,
		Source:    ex.req.Source,
		DryRun:    ex.req.DryRun,
		OnLog: func(stream, text string) {
			ex.emit(ctx, "step.log", path, map[string]any{"stream": stream, "text": text}, nil)
		},
	})
	if err != nil {
		var te *ToolError
		kind := "tool_error"
		if errors.As(err, &te) {
			kind = te.Kind
		}
		ex.emit(ctx, "tool.call.finished", path, map[string]any{
			"tool": binding.Name, "status": "failed", "duration_ms": time.Since(start).Milliseconds(), "error": err.Error(), "kind": kind,
		}, nil)
		if kind == "permission_denied" {
			ex.emit(ctx, "tool.sandbox.denied", path, map[string]any{
				"tool_version_id": binding.VersionID, "resource": err.Error(),
			}, nil)
		}
		return nil, err
	}
	ex.emit(ctx, "tool.call.finished", path, map[string]any{
		"tool": binding.Name, "status": "succeeded", "duration_ms": time.Since(start).Milliseconds(), "result": previewValue(output),
	}, nil)
	return output, nil
}

func (ex *executor) runLLM(ctx context.Context, st *Step, path string, env *Env) (any, error) {
	prompt, err := EvalTemplate(st.LLM.Prompt, env)
	if err != nil {
		return nil, fmt.Errorf("prompt: %w", err)
	}
	system, err := EvalTemplate(st.LLM.System, env)
	if err != nil {
		return nil, fmt.Errorf("system prompt: %w", err)
	}
	role := ex.spec.EffectiveRole(st)
	promptStr := toStringValue(prompt)
	systemStr := toStringValue(system)
	if st.LLM.OutputSchema != nil {
		hint := "\n\nReturn only a JSON object that conforms to this JSON Schema:\n" + mustJSONString(st.LLM.OutputSchema)
		if systemStr != "" {
			systemStr += hint
		} else {
			systemStr = "You are a precise assistant." + hint
		}
	}
	ex.emit(ctx, "llm.request", path, map[string]any{
		"role": role, "prompt": previewString(promptStr, 4000), "system": previewString(systemStr, 2000),
		"output_schema": st.LLM.OutputSchema != nil,
	}, nil)
	stream := func(text string) {
		ex.emitDelta("llm.delta", path, map[string]any{"text": text})
		ex.emitDelta("step.output.delta", path, map[string]any{"chunk": text})
	}
	res, err := ex.e.LLM.ChatWithTools(ctx, LLMChatRequest{
		RunID: ex.req.RunID, StepID: path, SessionID: ex.req.RunID, Role: role, System: systemStr,
		Messages:     []llm.Message{{Role: "user", Content: promptStr}},
		OutputSchema: st.LLM.OutputSchema, MaxTokens: st.LLM.MaxTokens, Temperature: st.LLM.Temperature,
		Stream: stream,
	})
	if err != nil {
		ex.emit(ctx, "llm.finished", path, map[string]any{"status": "failed", "error": err.Error()}, nil)
		return nil, err
	}
	if err := ex.addUsage(res.TokensIn, res.TokensOut, res.CostMicro); err != nil {
		return nil, err
	}
	ex.emit(ctx, "llm.finished", path, map[string]any{
		"model": res.Model, "role": role, "usage": map[string]any{"in": res.TokensIn, "out": res.TokensOut},
		"cost_micro": res.CostMicro, "finish_reason": res.Finish,
	}, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), `UPDATE runs SET tokens_in=tokens_in+?, tokens_out=tokens_out+?, cost_micro=cost_micro+? WHERE id=?`,
			res.TokensIn, res.TokensOut, res.CostMicro, ex.req.RunID)
		return err
	})
	var output any
	if st.LLM.OutputSchema != nil {
		output = res.Data
		if output == nil {
			output = decodeJSONLoose(res.Text)
		}
	} else {
		output = res.Text
	}
	if output == nil {
		output = res.Text
	}
	if st.LLM.OutputSchema != nil {
		if _, ok := output.(map[string]any); !ok {
			return nil, errors.New("model did not return a JSON object matching output_schema")
		}
	}
	return output, nil
}

func (ex *executor) runSet(st *Step, env *Env) (any, error) {
	out := map[string]any{}
	for k, v := range st.Set {
		val, err := EvalValue(v, env)
		if err != nil {
			return nil, fmt.Errorf("set.%s: %w", k, err)
		}
		out[k] = val
	}
	return out, nil
}

func (ex *executor) runBranch(ctx context.Context, st *Step, path string, env *Env, depth int) (any, error) {
	chosen := []Step(nil)
	for i, c := range st.Branch.Cases {
		ok, err := ex.evalCondition(c.When, env)
		if err != nil {
			return nil, fmt.Errorf("branch case %d: %w", i, err)
		}
		if ok {
			chosen = c.Do
			break
		}
	}
	if chosen == nil {
		chosen = st.Branch.Else
	}
	if chosen == nil {
		return nil, nil
	}
	outputs, _, err := ex.executeList(ctx, chosen, env, path+".", depth+1)
	if err != nil {
		return nil, err
	}
	if len(chosen) == 1 {
		return outputs[chosen[0].ID], nil
	}
	out := map[string]any{}
	for _, c := range chosen {
		out[c.ID] = outputs[c.ID]
	}
	return out, nil
}

func (ex *executor) runMap(ctx context.Context, st *Step, path string, env *Env, depth int) (any, error) {
	overVal, err := evalWorkflowString(st.Map.Over, env)
	if err != nil {
		return nil, fmt.Errorf("map.over: %w", err)
	}
	items, ok := overVal.([]any)
	if !ok {
		if overVal == nil {
			items = []any{}
		} else {
			return nil, fmt.Errorf("map.over must evaluate to an array, got %T", overVal)
		}
	}
	concurrency := st.Map.Concurrency
	if concurrency <= 0 {
		concurrency = 4
	}
	if concurrency > 8 {
		concurrency = 8
	}
	results := make([]any, len(items))
	iterCtx, iterCancel := context.WithCancel(ctx)
	defer iterCancel()
	var firstErr error
	var errMu sync.Mutex
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i := range items {
		i := i
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			select {
			case <-iterCtx.Done():
				return
			default:
			}
			iterEnv := copyEnv(env)
			iterEnv.Item = items[i]
			iterEnv.Index = i
			iterEnv.HasItem = true
			if st.Map.As != "" {
				iterEnv.Vars[st.Map.As] = items[i]
			}
			prefix := fmt.Sprintf("%s[%d].", path, i)
			outputs, _, err := ex.executeList(iterCtx, st.Map.Do, iterEnv, prefix, depth+1)
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
				iterCancel()
				return
			}
			var last any
			if len(st.Map.Do) > 0 {
				last = outputs[st.Map.Do[len(st.Map.Do)-1].ID]
			}
			results[i] = last
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

func (ex *executor) runParallel(ctx context.Context, st *Step, path string, env *Env, depth int) (any, error) {
	outputs, _, err := ex.executeList(ctx, st.Parallel.Do, env, path+".", depth+1)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	for _, c := range st.Parallel.Do {
		out[c.ID] = outputs[c.ID]
	}
	return out, nil
}

func (ex *executor) runApproval(ctx context.Context, st *Step, path string, env *Env, stepRunID string) (any, error) {
	var msg any = st.Approval.Message
	var err error
	if strings.Contains(st.Approval.Message, "${") {
		msg, err = EvalTemplate(st.Approval.Message, env)
		if err != nil {
			return nil, fmt.Errorf("approval message: %w", err)
		}
	}
	var show any
	if st.Approval.Show != "" {
		show, err = evalWorkflowString(st.Approval.Show, env)
		if err != nil {
			return nil, fmt.Errorf("approval show: %w", err)
		}
	}
	if ex.req.DryRun {
		ex.emit(ctx, "step.log", path, map[string]any{"stream": "stdout", "text": "dry run: approval auto-approved"}, nil)
		return map[string]any{"approved": true, "comment": "dry run"}, nil
	}
	unattended := ex.req.Unattended
	if unattended == "" {
		unattended = ex.spec.Settings.Unattended
	}
	if unattended == "" {
		unattended = "ask"
	}
	timeout := st.Approval.Timeout
	d, _ := parseDuration(timeout)
	if d <= 0 {
		d = 24 * time.Hour
	}
	if ex.e.Approvals == nil {
		return nil, errors.New("approval broker is not configured")
	}
	decision, err := ex.e.Approvals.RequestApproval(ctx, ApprovalRequest{
		RunID:          ex.req.RunID,
		StepID:         path,
		StepRunID:      stepRunID,
		ConversationID: ex.req.ConversationID,
		Kind:           "step",
		Title:          fallback(st.Name, "Approval required"),
		Description:    toStringValue(msg),
		Message:        toStringValue(msg),
		Show:           show,
		Source:         ex.req.Source,
		Unattended:     unattended,
		ExpiresAt:      time.Now().Add(d),
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"approved": decision.Approved, "comment": decision.Comment}, nil
}

func (ex *executor) runSubworkflow(ctx context.Context, st *Step, path string, env *Env, depth int) (any, error) {
	if depth > 6 {
		return nil, errors.New("workflow nesting is too deep")
	}
	if ex.e.Loader == nil {
		return nil, errors.New("subworkflow loader is not configured")
	}
	child, versionID, err := ex.e.Loader.LoadWorkflow(ctx, st.Workflow)
	if err != nil {
		return nil, err
	}
	argsVal, err := EvalValue(st.With, env)
	if err != nil {
		return nil, err
	}
	inputs := map[string]any{}
	if argsVal != nil {
		m, ok := argsVal.(map[string]any)
		if !ok {
			return nil, errors.New("subworkflow inputs must be an object")
		}
		inputs = m
	}
	ex.emit(ctx, "step.log", path, map[string]any{"stream": "stdout", "text": "running subworkflow " + st.Workflow + " (" + versionID + ")"}, nil)
	childEnv := &Env{Inputs: inputs, Steps: map[string]StepView{}, Run: env.Run}
	childOutputs, _, err := ex.executeList(ctx, child.Steps, childEnv, path+".", depth+1)
	if err != nil {
		return nil, err
	}
	outEnv := &Env{Inputs: inputs, Steps: childEnv.Steps, Run: env.Run}
	out := map[string]any{}
	for name, expr := range child.Outputs {
		v, err := evalWorkflowString(expr, outEnv)
		if err != nil {
			return nil, fmt.Errorf("subworkflow output %s: %w", name, err)
		}
		out[name] = v
	}
	_ = childOutputs
	return out, nil
}

func (ex *executor) runAgent(ctx context.Context, st *Step, path string, env *Env) (any, error) {
	goal, err := EvalTemplate(st.Agent.Goal, env)
	if err != nil {
		return nil, err
	}
	system, err := EvalTemplate(st.Agent.System, env)
	if err != nil {
		return nil, err
	}
	role := ex.spec.EffectiveRole(st)
	defs := []llm.ToolDef{}
	for _, ref := range st.Agent.Tools {
		b, err := ex.resolveBinding(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("agent tool %s: %w", ref, err)
		}
		params := b.InputSchema
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		defs = append(defs, llm.ToolDef{Type: "function", Function: llm.FunctionDef{
			Name: b.Name, Description: b.Description, Parameters: params,
		}})
	}
	maxSteps := st.Agent.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 8
	}
	if maxSteps > 20 {
		maxSteps = 20
	}
	systemStr := toStringValue(system)
	if systemStr == "" {
		systemStr = "You are a careful executor. Use the provided tools to complete the goal. Reply briefly when done."
	}
	if st.Agent.OutputSchema != nil {
		systemStr += "\n\nWhen finished, return only JSON matching this schema:\n" + mustJSONString(st.Agent.OutputSchema)
	}
	messages := []llm.Message{{Role: "user", Content: toStringValue(goal)}}
	var finalText string
	for step := 0; step < maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ex.emit(ctx, "llm.request", path+":agent", map[string]any{"role": role, "step": step + 1, "tools": len(defs)}, nil)
		res, err := ex.e.LLM.ChatWithTools(ctx, LLMChatRequest{
			RunID: ex.req.RunID, StepID: path + ":agent", SessionID: ex.req.RunID, Role: role, System: systemStr,
			Messages: messages, Tools: defs, MaxTokens: 0,
			Stream: func(text string) {
				ex.emitDelta("llm.delta", path, map[string]any{"text": text})
				ex.emitDelta("step.output.delta", path, map[string]any{"chunk": text})
			},
		})
		if err != nil {
			return nil, err
		}
		if err := ex.addUsage(res.TokensIn, res.TokensOut, res.CostMicro); err != nil {
			return nil, err
		}
		ex.emit(ctx, "llm.finished", path+":agent", map[string]any{
			"model": res.Model, "usage": map[string]any{"in": res.TokensIn, "out": res.TokensOut}, "cost_micro": res.CostMicro,
		}, nil)
		assistant := res.Message
		assistant.Role = "assistant"
		if assistant.Content == "" {
			assistant.Content = res.Text
		}
		messages = append(messages, assistant)
		if len(assistant.ToolCalls) == 0 {
			finalText = assistant.Content
			break
		}
		for _, call := range assistant.ToolCalls {
			b, rerr := ex.resolveBinding(ctx, call.Function.Name)
			if rerr != nil {
				messages = append(messages, llm.Message{Role: "tool", ToolCallID: call.ID, Content: mustJSONString(map[string]any{"error": rerr.Error()})})
				continue
			}
			args := map[string]any{}
			if call.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
					messages = append(messages, llm.Message{Role: "tool", ToolCallID: call.ID, Content: mustJSONString(map[string]any{"error": "invalid arguments: " + err.Error()})})
					continue
				}
			}
			ex.emit(ctx, "tool.call.started", path, map[string]any{"tool": b.Name, "tool_version_id": b.VersionID, "args": args}, nil)
			start := time.Now()
			out, terr := ex.e.Tools.Execute(ctx, b, args, ToolExecOptions{
				RunID: ex.req.RunID, StepID: path, IdemKey: ex.req.RunID + ":" + path + ":" + call.ID,
				Source: ex.req.Source, DryRun: ex.req.DryRun,
				OnLog: func(stream, text string) {
					ex.emit(ctx, "step.log", path, map[string]any{"stream": stream, "text": text}, nil)
				},
			})
			if terr != nil {
				ex.emit(ctx, "tool.call.finished", path, map[string]any{"tool": b.Name, "status": "failed", "duration_ms": time.Since(start).Milliseconds(), "error": terr.Error()}, nil)
				messages = append(messages, llm.Message{Role: "tool", ToolCallID: call.ID, Content: mustJSONString(map[string]any{"error": terr.Error()})})
			} else {
				ex.emit(ctx, "tool.call.finished", path, map[string]any{"tool": b.Name, "status": "succeeded", "duration_ms": time.Since(start).Milliseconds(), "result": previewValue(out)}, nil)
				messages = append(messages, llm.Message{Role: "tool", ToolCallID: call.ID, Content: mustJSONString(out)})
			}
		}
	}
	if finalText == "" {
		return nil, errors.New("agent_max_steps: agent did not finish within max_steps")
	}
	if st.Agent.OutputSchema != nil {
		parsed := decodeJSONLoose(finalText)
		if parsed == nil {
			return nil, errors.New("agent did not return JSON matching output_schema")
		}
		return parsed, nil
	}
	return finalText, nil
}

// ---- output persistence ----

func (ex *executor) saveOutput(ctx context.Context, stepRunID, path string, value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if ex.e.Redact != nil {
		raw = ex.e.redactRaw(raw)
		var clean any
		if json.Unmarshal(raw, &clean) == nil {
			value = clean
			raw, _ = json.Marshal(clean)
		}
	}
	if len(raw) <= 64*1024 || ex.e.ArtifactDir == "" {
		return string(raw), nil
	}
	dir := filepath.Join(ex.e.ArtifactDir, ex.req.RunID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := sanitizeFile(path) + "-" + stepRunID + ".json"
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, raw, 0o600); err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	art, err := ex.e.Store.AddArtifact(ctx, model.Artifact{
		RunID: ex.req.RunID, StepRunID: stepRunID, Path: full, SHA256: hex.EncodeToString(sum[:]),
		Size: int64(len(raw)), Mime: "application/json",
	})
	if err != nil {
		return "", err
	}
	return "artifact:" + art.ID, nil
}

func (ex *executor) loadOutput(ctx context.Context, ref string) (any, error) {
	if ref == "" {
		return nil, nil
	}
	if strings.HasPrefix(ref, "artifact:") {
		id := strings.TrimPrefix(ref, "artifact:")
		art, err := ex.e.Store.GetArtifact(ctx, id)
		if err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(art.Path)
		if err != nil {
			return nil, err
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		return v, nil
	}
	var v any
	if err := json.Unmarshal([]byte(ref), &v); err != nil {
		return nil, err
	}
	return v, nil
}

func sanitizeFile(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := b.String()
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}

func toStringValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	default:
		raw, _ := json.Marshal(x)
		return string(raw)
	}
}

func mustJSONString(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func previewString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func fallback(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func decodeJSONLoose(text string) any {
	s := strings.TrimSpace(text)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	var v any
	if err := json.Unmarshal([]byte(s), &v); err == nil {
		return v
	}
	start := strings.IndexAny(s, "{[")
	if start < 0 {
		return nil
	}
	end := strings.LastIndexAny(s, "}]")
	if end <= start {
		return nil
	}
	if err := json.Unmarshal([]byte(s[start:end+1]), &v); err != nil {
		return nil
	}
	return v
}
