package workflow

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"myagent/internal/schema"
)

type ToolInfo struct {
	Name           string
	VersionID      string
	Status         string
	InputSchema    map[string]any
	SideEffect     string
	Network        bool
	SecretNames    []string
	MissingSecrets []string
}

type ValidateOptions struct {
	Tools           map[string]ToolInfo
	Workflows       map[string]bool
	Models          map[string]bool
	AllowUnapproved bool
	ForRun          bool
}

type Diagnostic struct {
	Code    string   `json:"code"`
	Path    string   `json:"path"`
	Message string   `json:"message"`
	Hint    string   `json:"hint,omitempty"`
	Suggest []string `json:"suggest,omitempty"`
}

type ValidationResult struct {
	Errors      []Diagnostic   `json:"errors"`
	Warnings    []Diagnostic   `json:"warnings"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

func (r *ValidationResult) OK() bool { return len(r.Errors) == 0 }

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func Validate(spec *Spec, opts ValidateOptions) ValidationResult {
	res := ValidationResult{}
	addErr := func(code, path, msg, hint string, suggest ...string) {
		res.Errors = append(
			res.Errors,
			Diagnostic{Code: code, Path: path, Message: msg, Hint: hint, Suggest: suggest},
		)
	}
	addWarn := func(code, path, msg, hint string) {
		res.Warnings = append(
			res.Warnings,
			Diagnostic{Code: code, Path: path, Message: msg, Hint: hint},
		)
	}
	if spec == nil {
		addErr("E001", "", "workflow is empty", "Paste a workflow YAML document")
		return res
	}
	if spec.Schema != 1 {
		addErr("E001", "schema", "schema must be 1", "Add `schema: 1` at the top")
	}
	if strings.TrimSpace(spec.Name) == "" {
		addErr("E001", "name", "name is required", "Give the workflow a kebab-case name")
	}
	if len(spec.Steps) == 0 {
		addErr("E001", "steps", "at least one step is required", "Add a tool, llm or set step")
	}
	if spec.Settings.Unattended != "" && spec.Settings.Unattended != "allow" &&
		spec.Settings.Unattended != "ask" &&
		spec.Settings.Unattended != "deny" {
		addErr("E001", "settings.unattended", "unattended must be allow, ask or deny", "")
	}
	if spec.Settings.Concurrency != nil {
		if spec.Settings.Concurrency.Max < 0 {
			addErr("E001", "settings.concurrency.max", "concurrency max cannot be negative", "")
		}
		p := spec.Settings.Concurrency.Policy
		if p != "" && p != "queue" && p != "skip" && p != "cancel-previous" {
			addErr(
				"E001",
				"settings.concurrency.policy",
				"policy must be queue, skip or cancel-previous",
				"",
			)
		}
	}
	if spec.Defaults.Retry != nil {
		validateRetry(&res, *spec.Defaults.Retry, "defaults.retry")
	}
	if _, err := parseDuration(spec.Settings.Timeout); err != nil {
		addErr("E001", "settings.timeout", err.Error(), "Use a duration such as 90s or 10m")
	}
	if _, err := parseDuration(spec.Defaults.Timeout); err != nil {
		addErr("E001", "defaults.timeout", err.Error(), "Use a duration such as 90s or 10m")
	}
	if spec.Defaults.Model != "" && !modelKnown(opts.Models, spec.Defaults.Model) {
		addErr(
			"E001",
			"defaults.model",
			"unknown model "+strconv.Quote(spec.Defaults.Model),
			"Use a model profile name or model ID from Settings → Models",
		)
	}
	if spec.Defaults.Role != "" {
		addWarn(
			"W005",
			"defaults.role",
			"role is deprecated and ignored",
			"Use defaults.model with a profile name or model ID",
		)
	}

	// Inputs.
	inputSchema, err := spec.InputSchema()
	if err != nil {
		addErr("E001", "inputs", err.Error(), "")
	} else {
		res.InputSchema = inputSchema
	}
	for name, in := range spec.Inputs {
		path := "inputs." + name
		switch in.Type {
		case "string", "number", "integer", "boolean", "array", "object":
		default:
			addErr(
				"E001",
				path+".type",
				"unsupported input type "+strconv.Quote(in.Type),
				"Use string, number, integer, boolean, array or object",
			)
		}
		if in.Description == "" {
			addWarn(
				"W004",
				path,
				"input is missing a description",
				"Describe the value so the agent can fill the form correctly",
			)
		}
		if in.Default != nil {
			if iss := validateLiteralType(in.Default, in.Type); len(iss) > 0 {
				addErr("E001", path+".default", iss[0], "The default must match the declared type")
			}
		}
		for i, e := range in.Enum {
			if iss := validateLiteralType(e, in.Type); len(iss) > 0 {
				addErr(
					"E001",
					fmt.Sprintf("%s.enum[%d]", path, i),
					iss[0],
					"Enum values must match the input type",
				)
			}
		}
		if in.Format != "" {
			switch in.Format {
			case "path", "url", "email", "datetime", "text":
			default:
				addErr(
					"E001",
					path+".format",
					"unknown format "+strconv.Quote(in.Format),
					"Use path, url, email, datetime or text",
				)
			}
		}
	}

	// Outputs.
	for name, expr := range spec.Outputs {
		if strings.TrimSpace(expr) == "" {
			addErr("E009", "outputs."+name, "output expression is empty", "")
			continue
		}
		if err := checkExprString(expr, nil, false); err != nil {
			addErr("E009", "outputs."+name, err.Error(), "")
		}
	}

	// Index every step.
	flat := spec.FlattenSteps()
	byID := map[string]FlatStep{}
	byPath := map[string]FlatStep{}
	for i, fs := range flat {
		key := strconv.Itoa(i)
		_ = key
		if prev, ok := byID[fs.Step.ID]; ok {
			addErr(
				"E002",
				"steps."+fs.Path+".id",
				"duplicate step id "+fs.Step.ID,
				"Use the id of "+prev.Path+" only once",
			)
		}
		byID[fs.Step.ID] = fs
		byPath[fs.Path] = fs
	}
	for name, expr := range spec.Outputs {
		for _, ref := range refsFromString(expr) {
			switch ref.Root {
			case "steps":
				if len(ref.Path) == 0 || byID[ref.Path[0]].Path == "" {
					addErr(
						"E005",
						"outputs."+name,
						"output references a step that does not exist",
						"",
					)
				}
			case "inputs":
				if len(ref.Path) == 0 {
					addErr("E009", "outputs."+name, "inputs must be followed by a field name", "")
				} else if _, ok := spec.Inputs[ref.Path[0]]; !ok {
					addErr("E006", "outputs."+name, "input "+ref.Path[0]+" is not declared", "")
				}
			case "run", "item", "index":
			default:
				addErr("E009", "outputs."+name, "unknown variable "+ref.Root, "")
			}
		}
	}
	topAncestors := map[string]map[string]bool{}
	for _, st := range spec.Steps {
		anc := map[string]bool{}
		var visit func(id string)
		visit = func(id string) {
			stp := spec.StepByID(id)
			if stp == nil {
				return
			}
			for _, n := range stp.Needs {
				if !anc[n] {
					anc[n] = true
					visit(n)
				}
			}
		}
		visit(st.ID)
		topAncestors[st.ID] = anc
	}

	sawApprovalAncestor := func(fs FlatStep) bool {
		if fs.TopLevelID != "" {
			top := spec.StepByID(fs.TopLevelID)
			if top != nil && top.Approval != nil {
				return true
			}
			for id := range topAncestors[fs.TopLevelID] {
				if st := spec.StepByID(id); st != nil && st.Approval != nil {
					return true
				}
			}
		}
		return false
	}

	var checkStepList func(steps []Step, prefix string, topID string, inMap bool)
	checkStepList = func(steps []Step, prefix string, topID string, inMap bool) {
		seen := map[string]bool{}
		for i := range steps {
			st := &steps[i]
			path := fmt.Sprintf("%s[%d]", prefix, i)
			fs := FlatStep{Path: st.ID, TopLevelID: topID, Step: *st, InMap: inMap}
			if prefix == "steps" {
				fs.Path = st.ID
				fs.TopLevelID = st.ID
			}
			if st.ID == "" || !idPattern.MatchString(st.ID) {
				addErr(
					"E002",
					path+".id",
					"invalid step id "+strconv.Quote(st.ID),
					"Use lower_snake_case starting with a letter",
				)
			}
			if seen[st.ID] {
				addErr(
					"E002",
					path+".id",
					"duplicate step id "+st.ID,
					"Step ids must be unique within their list",
				)
			}
			seen[st.ID] = true
			kind := st.StepKind()
			if kind == "" {
				addErr(
					"E001",
					path,
					"step has no type",
					"Set one of tool, llm, agent, set, branch, map, parallel, approval or workflow",
				)
			} else if kind == "conflict" {
				addErr(
					"E001",
					path,
					"step declares more than one type",
					"Keep exactly one of tool, llm, agent, set, branch, map, parallel, approval or workflow",
				)
			}
			if st.OnError != "" && st.OnError != "fail" && st.OnError != "continue" {
				addErr("E001", path+".on_error", "on_error must be fail or continue", "")
			}
			if st.SideEffect != "" {
				switch st.SideEffect {
				case "none", "read", "write", "external":
				default:
					addErr(
						"E001",
						path+".side_effect",
						"side_effect must be none, read, write or external",
						"",
					)
				}
			}
			if _, err := parseDuration(st.Timeout); err != nil {
				addErr("E001", path+".timeout", err.Error(), "")
			}
			if st.Retry != nil {
				validateRetry(&res, *st.Retry, path+".retry")
			}
			needSet := map[string]bool{}
			for j, n := range st.Needs {
				if needSet[n] {
					addErr(
						"E003",
						fmt.Sprintf("%s.needs[%d]", path, j),
						"duplicate dependency "+n,
						"",
					)
				}
				needSet[n] = true
				if prefix == "steps" {
					if spec.StepByID(n) == nil {
						addErr(
							"E003",
							fmt.Sprintf("%s.needs[%d]", path, j),
							"dependency "+n+" does not exist",
							"Use the id of a top-level step",
						)
					}
				} else {
					found := false
					for k := range steps {
						if steps[k].ID == n {
							found = true
						}
					}
					if !found {
						addErr(
							"E003",
							fmt.Sprintf("%s.needs[%d]", path, j),
							"dependency "+n+" does not exist in this list",
							"",
						)
					}
				}
			}
			if st.When != "" {
				if err := checkExprString(st.When, &fs, inMap); err != nil {
					addErr(
						"E009",
						path+".when",
						err.Error(),
						"Use a boolean expression such as ${steps.review.output.approved}",
					)
				}
			}
			if err := checkStepExpressions(st, &fs, inMap); err != nil {
				addErr("E009", path, err.Error(), "")
			}
			// Reference ancestry is checked per field by checkRefs; checkExprString
			// only parses. Here we use static reference collection.
			for _, ref := range collectStepRefs(st) {
				if msg, code := validateRef(ref, fs, inMap, spec, byID, topAncestors); msg != "" {
					addErr(code, path, msg, refHint(ref))
				}
			}

			sideEffect := st.SideEffect
			switch kind {
			case "tool":
				name, ver, _ := parseToolRef(st.Tool)
				info, ok := lookupTool(opts.Tools, name, ver)
				if !ok {
					addErr(
						"E007",
						path+".tool",
						"tool "+st.Tool+" does not exist",
						"Run search_tools or create the tool first",
					)
				} else {
					if !opts.AllowUnapproved && info.Status != "approved" &&
						info.Status != "active" {
						addErr(
							"E007",
							path+".tool",
							"tool "+info.Name+" has not been approved",
							"Approve the tool in Tools before using it",
						)
					}
					if sideEffect == "" {
						sideEffect = info.SideEffect
					} else if sideEffectRank(sideEffect) < sideEffectRank(info.SideEffect) {
						addErr(
							"E001",
							path+".side_effect",
							"step cannot lower the tool side effect from "+info.SideEffect+" to "+sideEffect,
							"Remove side_effect or use a value at least as strong as the tool",
						)
					}
					if aerrs := validateArgs(
						path+".with",
						st.With,
						info.InputSchema,
					); len(
						aerrs,
					) > 0 {
						for _, a := range aerrs {
							addErr("E008", a.Path, a.Message, "Check the tool's input schema")
						}
					}
					if opts.ForRun && len(info.MissingSecrets) > 0 {
						addErr(
							"E010",
							path+".tool",
							"tool needs secrets that are not bound: "+strings.Join(
								info.MissingSecrets,
								", ",
							),
							"Add the secret in Settings and bind it to the tool",
						)
					} else if !opts.ForRun && len(info.MissingSecrets) > 0 {
						addWarn(
							"E010",
							path+".tool",
							"tool needs unbound secrets: "+strings.Join(info.MissingSecrets, ", "),
							"Bind them before running",
						)
					}
					effTimeout := spec.EffectiveTimeout(st)
					effRetry := spec.EffectiveRetry(st)
					if info.Network && effTimeout == "" && (effRetry == nil || effRetry.Max == 0) {
						addWarn(
							"W002",
							path,
							"network step has no timeout or retry",
							"Add timeout or retry to handle flaky networks",
						)
					}
				}
			case "llm":
				if strings.TrimSpace(st.LLM.Prompt) == "" {
					addErr("E001", path+".llm.prompt", "prompt is required", "")
				}
				if st.LLM.OutputSchema != nil {
					if _, err := schema.CompileValue(st.LLM.OutputSchema); err != nil {
						addErr("E001", path+".llm.output_schema", err.Error(), "")
					}
				}
				if st.LLM.Model != "" && !modelKnown(opts.Models, st.LLM.Model) {
					addErr(
						"E001",
						path+".llm.model",
						"unknown model "+strconv.Quote(st.LLM.Model),
						"Use a model profile name or model ID from Settings → Models",
					)
				}
				if st.LLM.Role != "" {
					addWarn(
						"W005",
						path+".llm.role",
						"role is deprecated and ignored",
						"Use llm.model with a profile name or model ID",
					)
				}
			case "agent":
				if strings.TrimSpace(st.Agent.Goal) == "" {
					addErr("E001", path+".agent.goal", "goal is required", "")
				}
				if st.Agent.Model != "" && !modelKnown(opts.Models, st.Agent.Model) {
					addErr(
						"E001",
						path+".agent.model",
						"unknown model "+strconv.Quote(st.Agent.Model),
						"Use a model profile name or model ID from Settings → Models",
					)
				}
				if st.Agent.Role != "" {
					addWarn(
						"W005",
						path+".agent.role",
						"role is deprecated and ignored",
						"Use agent.model with a profile name or model ID",
					)
				}
				if len(st.Agent.Tools) == 0 {
					addErr(
						"E001",
						path+".agent.tools",
						"agent step needs a tool allowlist",
						"List the tools the sub-agent may use",
					)
				}
				if st.Agent.MaxSteps <= 0 {
					addWarn(
						"W003",
						path+".agent",
						"agent step has no max_steps",
						"Set max_steps so cost stays bounded",
					)
				}
				if st.Agent.OutputSchema != nil {
					if _, err := schema.CompileValue(st.Agent.OutputSchema); err != nil {
						addErr("E001", path+".agent.output_schema", err.Error(), "")
					}
				}
			case "set":
				if len(st.Set) == 0 {
					addErr("E001", path+".set", "set must declare at least one key", "")
				}
			case "branch":
				if len(st.Branch.Cases) == 0 && len(st.Branch.Else) == 0 {
					addErr("E001", path+".branch", "branch has no cases and no else", "")
				}
				for ci, c := range st.Branch.Cases {
					if c.When == "" {
						addErr(
							"E001",
							fmt.Sprintf("%s.branch.cases[%d].when", path, ci),
							"case condition is required",
							"",
						)
					}
					if len(c.Do) == 0 {
						addErr(
							"E001",
							fmt.Sprintf("%s.branch.cases[%d].do", path, ci),
							"case has no steps",
							"",
						)
					}
				}
			case "map":
				if strings.TrimSpace(st.Map.Over) == "" {
					addErr("E009", path+".map.over", "map.over is required", "")
				}
				if len(st.Map.Do) == 0 {
					addErr("E001", path+".map.do", "map has no steps", "")
				}
				if st.Map.Concurrency < 0 {
					addErr("E001", path+".map.concurrency", "concurrency cannot be negative", "")
				}
				if st.Map.As != "" && !idPattern.MatchString(st.Map.As) {
					addErr("E001", path+".map.as", "map.as must be a valid identifier", "")
				}
			case "parallel":
				if len(st.Parallel.Do) == 0 {
					addErr("E001", path+".parallel.do", "parallel has no steps", "")
				}
			case "approval":
				if strings.TrimSpace(st.Approval.Message) == "" {
					addErr("E001", path+".approval.message", "approval message is required", "")
				}
			case "workflow":
				name, ver, _ := parseToolRef(st.Workflow)
				if opts.Workflows != nil && !opts.Workflows[workflowKey(name, ver)] {
					addErr(
						"E007",
						path+".workflow",
						"workflow "+st.Workflow+" does not exist or is not approved",
						"Save and approve the child workflow first",
					)
				}
			}
			_ = sideEffect
			// W001: risky step without an approval ancestor.
			if (sideEffect == "write" || sideEffect == "external") &&
				spec.Settings.Unattended != "allow" &&
				!sawApprovalAncestor(fs) {
				addWarn(
					"W001",
					path,
					"step with side effect "+sideEffect+" has no approval before it",
					"Add an approval step in its needs chain, or set unattended: allow deliberately",
				)
			}
			// Nested execution.
			switch {
			case st.Branch != nil:
				for ci := range st.Branch.Cases {
					checkStepList(
						st.Branch.Cases[ci].Do,
						fmt.Sprintf("%s.branch.cases[%d].do", path, ci),
						topID,
						inMap,
					)
				}
				checkStepList(st.Branch.Else, path+".branch.else", topID, inMap)
			case st.Map != nil:
				checkStepList(st.Map.Do, path+".map.do", topID, true)
			case st.Parallel != nil:
				checkStepList(st.Parallel.Do, path+".parallel.do", topID, inMap)
			}
		}
		// Local needs cycle detection.
		if hasLocalCycle(steps) {
			addErr("E004", prefix, "step dependencies contain a cycle", "Remove circular needs")
		}
	}
	checkStepList(spec.Steps, "steps", "", false)
	// Top-level cycle detection (also mirrored in local check).
	if hasCycle(spec.Steps) {
		addErr("E004", "steps", "step dependencies contain a cycle", "Remove circular needs")
	}
	return res
}

func validateRetry(res *ValidationResult, r RetrySpec, path string) {
	_ = res
	if r.Max < 0 {
		res.Errors = append(
			res.Errors,
			Diagnostic{Code: "E001", Path: path + ".max", Message: "retry max cannot be negative"},
		)
	}
	if r.Backoff != "" && r.Backoff != "fixed" && r.Backoff != "exp" {
		res.Errors = append(
			res.Errors,
			Diagnostic{
				Code:    "E001",
				Path:    path + ".backoff",
				Message: "backoff must be fixed or exp",
			},
		)
	}
	if _, err := parseDuration(r.Delay); err != nil {
		res.Errors = append(
			res.Errors,
			Diagnostic{Code: "E001", Path: path + ".delay", Message: err.Error()},
		)
	}
}

func checkExprString(s string, fs *FlatStep, inMap bool) error {
	trim := strings.TrimSpace(s)
	if trim == "" {
		return nil
	}
	if !strings.Contains(s, "${") {
		if fs == nil {
			// Bare outputs expressions are allowed.
		}
		_, err := ParseExpression(trim)
		return err
	}
	_, _, err := CompileTemplates(s)
	return err
}

func checkStepExpressions(st *Step, fs *FlatStep, inMap bool) error {
	check := func(s string) error {
		if s == "" {
			return nil
		}
		if !strings.Contains(s, "${") {
			return nil
		}
		_, _, err := CompileTemplates(s)
		return err
	}
	stringsToCheck := []string{}
	if st.LLM != nil {
		stringsToCheck = append(stringsToCheck, st.LLM.Prompt, st.LLM.System)
	}
	if st.Agent != nil {
		stringsToCheck = append(stringsToCheck, st.Agent.Goal, st.Agent.System)
	}
	if st.Approval != nil {
		stringsToCheck = append(stringsToCheck, st.Approval.Message, st.Approval.Show)
	}
	if st.Map != nil {
		if err := checkExprString(st.Map.Over, fs, true); err != nil {
			return fmt.Errorf("map.over: %w", err)
		}
	}
	if st.Branch != nil {
		for _, c := range st.Branch.Cases {
			if err := checkExprString(c.When, fs, inMap); err != nil {
				return fmt.Errorf("branch case: %w", err)
			}
		}
	}
	var walk func(v any) error
	walk = func(v any) error {
		switch x := v.(type) {
		case string:
			return check(x)
		case map[string]any:
			for _, item := range x {
				if err := walk(item); err != nil {
					return err
				}
			}
		case []any:
			for _, item := range x {
				if err := walk(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, s := range stringsToCheck {
		if err := check(s); err != nil {
			return err
		}
	}
	if err := walk(st.With); err != nil {
		return fmt.Errorf("with: %w", err)
	}
	if err := walk(st.Set); err != nil {
		return fmt.Errorf("set: %w", err)
	}
	return nil
}

func collectStepRefs(st *Step) []Ref {
	seen := map[string]bool{}
	out := []Ref{}
	add := func(s string) {
		if !strings.Contains(s, "${") {
			return
		}
		exprs, exact, err := CompileTemplates(s)
		if err != nil {
			return
		}
		_ = exact
		for _, c := range exprs {
			for _, ref := range c.Refs {
				key := ref.Root + "." + strings.Join(ref.Path, ".")
				if !seen[key] {
					seen[key] = true
					out = append(out, ref)
				}
			}
		}
	}
	addExpr := func(s string) {
		if s == "" {
			return
		}
		if !strings.Contains(s, "${") {
			c, err := CompileExpression(s)
			if err != nil {
				return
			}
			for _, ref := range c.Refs {
				key := ref.Root + "." + strings.Join(ref.Path, ".")
				if !seen[key] {
					seen[key] = true
					out = append(out, ref)
				}
			}
			return
		}
		add(s)
	}
	if st.When != "" {
		addExpr(st.When)
	}
	if st.Map != nil {
		addExpr(st.Map.Over)
	}
	if st.Branch != nil {
		for _, c := range st.Branch.Cases {
			addExpr(c.When)
		}
	}
	if st.LLM != nil {
		add(st.LLM.Prompt)
		add(st.LLM.System)
	}
	if st.Agent != nil {
		add(st.Agent.Goal)
		add(st.Agent.System)
	}
	if st.Approval != nil {
		add(st.Approval.Message)
		add(st.Approval.Show)
	}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			add(x)
		case map[string]any:
			for _, item := range x {
				walk(item)
			}
		case []any:
			for _, item := range x {
				walk(item)
			}
		}
	}
	walk(st.With)
	walk(st.Set)
	return out
}

func validateRef(
	ref Ref,
	current FlatStep,
	inMap bool,
	spec *Spec,
	byID map[string]FlatStep,
	ancestors map[string]map[string]bool,
) (string, string) {
	switch ref.Root {
	case "inputs":
		if len(ref.Path) == 0 {
			return "inputs must be followed by a field name", "E009"
		}
		if _, ok := spec.Inputs[ref.Path[0]]; !ok {
			return "input " + ref.Path[0] + " is not declared", "E006"
		}
	case "steps":
		if len(ref.Path) == 0 {
			return "steps must be followed by a step id", "E009"
		}
		id := ref.Path[0]
		target, ok := byID[id]
		if !ok {
			return "step " + id + " does not exist", "E005"
		}
		currentTop := current.TopLevelID
		if currentTop == "" {
			currentTop = current.Path
		}
		targetTop := target.TopLevelID
		if targetTop == "" {
			targetTop = target.Path
		}
		currentIsTop := current.TopLevelID == current.Path
		if current.Path == target.Path {
			return "step " + id + " cannot reference itself", "E005"
		}
		if currentIsTop {
			if targetTop == currentTop {
				return "step " + id + " is inside " + currentTop + " and is not complete yet", "E005"
			}
			if ancestors[currentTop][targetTop] {
				return "", ""
			}
			return "step " + id + " is not an ancestor of " + currentTop + " (add it to needs)", "E005"
		}
		// Sibling inside the same branch/map/parallel group.
		if targetTop == currentTop {
			return "", ""
		}
		// A completed top-level ancestor, or a step inside one.
		if ancestors[currentTop][targetTop] {
			return "", ""
		}
		return "step " + id + " is not an ancestor of " + currentTop + " (add it to needs)", "E005"
	case "run":
		if len(ref.Path) == 0 || (ref.Path[0] != "id" && ref.Path[0] != "source") {
			return "run only exposes run.id and run.source", "E009"
		}
	case "item", "index":
		if !inMap {
			return ref.Root + " is only available inside a map step", "E009"
		}
	case "true", "false", "null":
	default:
		// Inside a map, `as: file` introduces a custom alias in addition to
		// item/index. The alias is not visible to the validator's flat scope.
		if inMap {
			return "", ""
		}
		return "unknown variable " + ref.Root, "E009"
	}
	return "", ""
}

func refsFromString(s string) []Ref {
	out := []Ref{}
	if s == "" {
		return out
	}
	if strings.Contains(s, "${") {
		exprs, _, err := CompileTemplates(s)
		if err != nil {
			return out
		}
		for _, c := range exprs {
			out = append(out, c.Refs...)
		}
		return out
	}
	if c, err := CompileExpression(s); err == nil {
		out = append(out, c.Refs...)
	}
	return out
}

func refHint(ref Ref) string {
	if ref.Root == "steps" && len(ref.Path) > 0 {
		return "Add " + ref.Path[0] + " to needs, or reference an earlier step"
	}
	if ref.Root == "inputs" && len(ref.Path) > 0 {
		return "Declare " + ref.Path[0] + " under inputs"
	}
	return ""
}

func parseToolRef(ref string) (string, int, bool) {
	ref = strings.TrimSpace(ref)
	if i := strings.LastIndex(ref, "@"); i > 0 {
		if v, err := strconv.Atoi(ref[i+1:]); err == nil {
			return ref[:i], v, true
		}
	}
	return ref, 0, false
}

func workflowKey(name string, version int) string {
	if version > 0 {
		return name + "@" + strconv.Itoa(version)
	}
	return name
}

func lookupTool(tools map[string]ToolInfo, name string, version int) (ToolInfo, bool) {
	if tools == nil {
		return ToolInfo{}, false
	}
	if version > 0 {
		if t, ok := tools[name+"@"+strconv.Itoa(version)]; ok {
			return t, true
		}
	}
	if t, ok := tools[name]; ok {
		return t, true
	}
	return ToolInfo{}, false
}

func sideEffectRank(s string) int {
	switch s {
	case "write":
		return 2
	case "external":
		return 3
	default:
		return 1
	}
}

func modelKnown(models map[string]bool, name string) bool {
	if models == nil {
		return true
	}
	if models[name] {
		return true
	}
	for configured := range models {
		if strings.EqualFold(configured, name) {
			return true
		}
	}
	return false
}

func validateLiteralType(v any, typ string) []string {
	raw, _ := json.Marshal(v)
	var n any
	_ = json.Unmarshal(raw, &n)
	s, err := schema.Compile([]byte(fmt.Sprintf(`{"type":%q}`, typ)))
	if err != nil {
		return nil
	}
	iss := s.Validate(n)
	out := []string{}
	for _, i := range iss {
		out = append(out, i.Message)
	}
	return out
}

func validateArgs(path string, args map[string]any, inputSchema map[string]any) []schema.Issue {
	if inputSchema == nil {
		return nil
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return []schema.Issue{{Path: path, Message: "arguments are not valid JSON"}}
	}
	var generic map[string]any
	_ = json.Unmarshal(raw, &generic)
	props, _ := inputSchema["properties"].(map[string]any)
	issues := []schema.Issue{}
	if req, ok := inputSchema["required"].([]any); ok {
		for _, r := range req {
			name, _ := r.(string)
			if _, exists := generic[name]; !exists {
				issues = append(
					issues,
					schema.Issue{
						Path:    path + "." + name,
						Message: "required tool argument is missing",
					},
				)
			}
		}
	}
	for name, val := range generic {
		propRaw, exists := props[name]
		if !exists {
			if ap, ok := inputSchema["additionalProperties"].(bool); ok && !ap {
				issues = append(
					issues,
					schema.Issue{Path: path + "." + name, Message: "unexpected tool argument"},
				)
			}
			continue
		}
		if s, ok := val.(string); ok && strings.Contains(s, "${") {
			continue
		}
		pm, ok := propRaw.(map[string]any)
		if !ok {
			continue
		}
		ps, err := schema.CompileValue(pm)
		if err != nil {
			continue
		}
		for _, iss := range ps.Validate(val) {
			issues = append(
				issues,
				schema.Issue{Path: path + "." + name + iss.Path, Message: iss.Message},
			)
		}
	}
	return issues
}

func parseDuration(s string) (time.Duration, error) {
	if strings.TrimSpace(s) == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	if d < 0 {
		return 0, fmt.Errorf("duration cannot be negative")
	}
	return d, nil
}

func hasCycle(steps []Step) bool {
	state := map[string]int{}
	var dfs func(id string) bool
	dfs = func(id string) bool {
		switch state[id] {
		case 1:
			return true
		case 2:
			return false
		}
		state[id] = 1
		st := findStep(steps, id)
		if st != nil {
			for _, n := range st.Needs {
				if dfs(n) {
					return true
				}
			}
		}
		state[id] = 2
		return false
	}
	for _, st := range steps {
		if dfs(st.ID) {
			return true
		}
	}
	return false
}

func hasLocalCycle(steps []Step) bool {
	return hasCycle(steps)
}

func findStep(steps []Step, id string) *Step {
	for i := range steps {
		if steps[i].ID == id {
			return &steps[i]
		}
	}
	return nil
}

// SortDiagnostics makes output deterministic, which matters for tests and for
// feeding stable repair instructions to the model.
func SortDiagnostics(ds []Diagnostic) {
	sort.SliceStable(ds, func(i, j int) bool {
		if ds[i].Code != ds[j].Code {
			return ds[i].Code < ds[j].Code
		}
		return ds[i].Path < ds[j].Path
	})
}
