package workflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Spec struct {
	Schema      int                  `yaml:"schema"                json:"schema"`
	Name        string               `yaml:"name"                  json:"name"`
	Description string               `yaml:"description,omitempty" json:"description,omitempty"`
	Inputs      map[string]InputSpec `yaml:"inputs,omitempty"      json:"inputs,omitempty"`
	Outputs     map[string]string    `yaml:"outputs,omitempty"     json:"outputs,omitempty"`
	Settings    Settings             `yaml:"settings,omitempty"    json:"settings,omitempty"`
	Defaults    Defaults             `yaml:"defaults,omitempty"    json:"defaults,omitempty"`
	Steps       []Step               `yaml:"steps"                 json:"steps"`
}

type InputSpec struct {
	Type        string `yaml:"type"                  json:"type"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Default     any    `yaml:"default,omitempty"     json:"default,omitempty"`
	Enum        []any  `yaml:"enum,omitempty"        json:"enum,omitempty"`
	Format      string `yaml:"format,omitempty"      json:"format,omitempty"`
}

type Settings struct {
	Unattended  string           `yaml:"unattended,omitempty"  json:"unattended,omitempty"`
	Concurrency *ConcurrencySpec `yaml:"concurrency,omitempty" json:"concurrency,omitempty"`
	Budget      *BudgetSpec      `yaml:"budget,omitempty"      json:"budget,omitempty"`
	Timeout     string           `yaml:"timeout,omitempty"     json:"timeout,omitempty"`
}

type ConcurrencySpec struct {
	Max    int    `yaml:"max,omitempty"    json:"max,omitempty"`
	Policy string `yaml:"policy,omitempty" json:"policy,omitempty"`
}

type BudgetSpec struct {
	MaxCostUSD float64 `yaml:"max_cost_usd,omitempty" json:"max_cost_usd,omitempty"`
	MaxTokens  int     `yaml:"max_tokens,omitempty"   json:"max_tokens,omitempty"`
}

type Defaults struct {
	Timeout string     `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Retry   *RetrySpec `yaml:"retry,omitempty"   json:"retry,omitempty"`
	Model   string     `yaml:"model,omitempty"   json:"model,omitempty"`
	// Role is accepted for older workflows and ignored. Use Model instead.
	Role string `yaml:"role,omitempty" json:"role,omitempty"`
}

type RetrySpec struct {
	Max     int    `yaml:"max"               json:"max"`
	Backoff string `yaml:"backoff,omitempty" json:"backoff,omitempty"`
	Delay   string `yaml:"delay,omitempty"   json:"delay,omitempty"`
}

type Step struct {
	ID         string     `yaml:"id"                    json:"id"`
	Name       string     `yaml:"name,omitempty"        json:"name,omitempty"`
	Needs      []string   `yaml:"needs,omitempty"       json:"needs,omitempty"`
	When       string     `yaml:"when,omitempty"        json:"when,omitempty"`
	Timeout    string     `yaml:"timeout,omitempty"     json:"timeout,omitempty"`
	Retry      *RetrySpec `yaml:"retry,omitempty"       json:"retry,omitempty"`
	OnError    string     `yaml:"on_error,omitempty"    json:"on_error,omitempty"`
	SideEffect string     `yaml:"side_effect,omitempty" json:"side_effect,omitempty"`

	Tool     string         `yaml:"tool,omitempty"     json:"tool,omitempty"`
	With     map[string]any `yaml:"with,omitempty"     json:"with,omitempty"`
	LLM      *LLMSpec       `yaml:"llm,omitempty"      json:"llm,omitempty"`
	Agent    *AgentSpec     `yaml:"agent,omitempty"    json:"agent,omitempty"`
	Set      map[string]any `yaml:"set,omitempty"      json:"set,omitempty"`
	Branch   *BranchSpec    `yaml:"branch,omitempty"   json:"branch,omitempty"`
	Map      *MapSpec       `yaml:"map,omitempty"      json:"map,omitempty"`
	Parallel *ParallelSpec  `yaml:"parallel,omitempty" json:"parallel,omitempty"`
	Approval *ApprovalSpec  `yaml:"approval,omitempty" json:"approval,omitempty"`
	Workflow string         `yaml:"workflow,omitempty" json:"workflow,omitempty"`
}

type LLMSpec struct {
	Model string `yaml:"model,omitempty" json:"model,omitempty"`
	// Role is accepted for older workflows and ignored. Use Model instead.
	Role         string         `yaml:"role,omitempty"          json:"role,omitempty"`
	Prompt       string         `yaml:"prompt"                  json:"prompt"`
	System       string         `yaml:"system,omitempty"        json:"system,omitempty"`
	OutputSchema map[string]any `yaml:"output_schema,omitempty" json:"output_schema,omitempty"`
	MaxTokens    int            `yaml:"max_tokens,omitempty"    json:"max_tokens,omitempty"`
	Temperature  *float64       `yaml:"temperature,omitempty"   json:"temperature,omitempty"`
}

type AgentSpec struct {
	Model string `yaml:"model,omitempty" json:"model,omitempty"`
	// Role is accepted for older workflows and ignored. Use Model instead.
	Role         string         `yaml:"role,omitempty"          json:"role,omitempty"`
	Goal         string         `yaml:"goal"                    json:"goal"`
	System       string         `yaml:"system,omitempty"        json:"system,omitempty"`
	Tools        []string       `yaml:"tools,omitempty"         json:"tools,omitempty"`
	MaxSteps     int            `yaml:"max_steps,omitempty"     json:"max_steps,omitempty"`
	OutputSchema map[string]any `yaml:"output_schema,omitempty" json:"output_schema,omitempty"`
}

type BranchSpec struct {
	Cases []BranchCase `yaml:"cases,omitempty" json:"cases,omitempty"`
	Else  []Step       `yaml:"else,omitempty"  json:"else,omitempty"`
}

type BranchCase struct {
	When string `yaml:"when,omitempty" json:"when"`
	Do   []Step `yaml:"do,omitempty"   json:"do"`
}

type MapSpec struct {
	Over        string `yaml:"over"                  json:"over"`
	As          string `yaml:"as,omitempty"          json:"as,omitempty"`
	Do          []Step `yaml:"do"                    json:"do"`
	Concurrency int    `yaml:"concurrency,omitempty" json:"concurrency,omitempty"`
}

type ParallelSpec struct {
	Do []Step `yaml:"do,omitempty" json:"do"`
}

type ApprovalSpec struct {
	Message string `yaml:"message"           json:"message"`
	Show    string `yaml:"show,omitempty"    json:"show,omitempty"`
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

func Parse(data []byte) (*Spec, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var spec Spec
	if err := dec.Decode(&spec); err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("workflow is empty")
		}
		return nil, err
	}
	// Reject a second YAML document; workflows are always exactly one document.
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("workflow must contain exactly one YAML document")
	}
	return &spec, nil
}

func (s *Spec) Marshal() ([]byte, error) {
	return yaml.Marshal(s)
}

func (s *Spec) Hash() string {
	raw, _ := json.Marshal(s)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *Spec) StepByID(id string) *Step {
	for i := range s.Steps {
		if s.Steps[i].ID == id {
			return &s.Steps[i]
		}
	}
	return nil
}

func (s *Spec) EffectiveTimeout(step *Step) string {
	if step != nil && step.Timeout != "" {
		return step.Timeout
	}
	return s.Defaults.Timeout
}

func (s *Spec) EffectiveRetry(step *Step) *RetrySpec {
	if step != nil && step.Retry != nil {
		return step.Retry
	}
	return s.Defaults.Retry
}

func (s *Spec) EffectiveModel(step *Step) string {
	if step != nil && step.LLM != nil && step.LLM.Model != "" {
		return step.LLM.Model
	}
	if step != nil && step.Agent != nil && step.Agent.Model != "" {
		return step.Agent.Model
	}
	if s.Defaults.Model != "" {
		return s.Defaults.Model
	}
	return ""
}

func (s *Spec) ValidateBasic() error {
	if s.Schema != 1 {
		return fmt.Errorf("schema must be 1")
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}

// InputSchema converts the compact workflow input declaration to JSON Schema.
func (s *Spec) InputSchema() (map[string]any, error) {
	props := map[string]any{}
	required := []any{}
	for name, in := range s.Inputs {
		if in.Type == "" {
			return nil, fmt.Errorf("input %s: type is required", name)
		}
		prop := map[string]any{"type": in.Type}
		if in.Description != "" {
			prop["description"] = in.Description
		}
		if in.Default != nil {
			prop["default"] = normalizeJSON(in.Default)
		} else {
			required = append(required, name)
		}
		if len(in.Enum) > 0 {
			prop["enum"] = normalizeJSON(in.Enum)
		}
		if in.Format != "" {
			prop["format"] = in.Format
		}
		props[name] = prop
	}
	out := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		sort.Slice(
			required,
			func(i, j int) bool { return required[i].(string) < required[j].(string) },
		)
		out["required"] = required
	}
	return out, nil
}

// StepKind identifies the step discriminator. Exactly one must be set.
func (st *Step) StepKind() string {
	kinds := []string{}
	if st.Tool != "" {
		kinds = append(kinds, "tool")
	}
	if st.LLM != nil {
		kinds = append(kinds, "llm")
	}
	if st.Agent != nil {
		kinds = append(kinds, "agent")
	}
	if st.Set != nil {
		kinds = append(kinds, "set")
	}
	if st.Branch != nil {
		kinds = append(kinds, "branch")
	}
	if st.Map != nil {
		kinds = append(kinds, "map")
	}
	if st.Parallel != nil {
		kinds = append(kinds, "parallel")
	}
	if st.Approval != nil {
		kinds = append(kinds, "approval")
	}
	if st.Workflow != "" {
		kinds = append(kinds, "workflow")
	}
	if len(kinds) == 1 {
		return kinds[0]
	}
	if len(kinds) == 0 {
		return ""
	}
	return "conflict"
}

func (st *Step) Summary() string {
	switch st.StepKind() {
	case "tool":
		return st.Tool
	case "llm":
		model := st.LLM.Model
		if model == "" {
			return "llm"
		}
		return "llm:" + model
	case "agent":
		if st.Agent.Model != "" {
			return "agent:" + st.Agent.Model
		}
		return "agent"
	case "set":
		return "set"
	case "branch":
		return "branch"
	case "map":
		return "map"
	case "parallel":
		return "parallel"
	case "approval":
		return "approval"
	case "workflow":
		return st.Workflow
	}
	return "invalid"
}

func normalizeJSON(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return v
	}
	return out
}

// FlattenSteps returns every step with its path and enclosing top-level step.
type FlatStep struct {
	Path       string
	TopLevelID string
	Step       Step
	Depth      int
	InMap      bool
}

func (s *Spec) FlattenSteps() []FlatStep {
	out := []FlatStep{}
	var walk func(steps []Step, prefix, top string, depth int, inMap bool)
	walk = func(steps []Step, prefix, top string, depth int, inMap bool) {
		for _, st := range steps {
			path := st.ID
			if prefix != "" {
				path = prefix + "." + st.ID
			}
			if top == "" {
				top = st.ID
			}
			out = append(
				out,
				FlatStep{Path: path, TopLevelID: top, Step: st, Depth: depth, InMap: inMap},
			)
			switch {
			case st.Branch != nil:
				for i := range st.Branch.Cases {
					walk(st.Branch.Cases[i].Do, path, top, depth+1, inMap)
				}
				walk(st.Branch.Else, path, top, depth+1, inMap)
			case st.Map != nil:
				walk(st.Map.Do, path, top, depth+1, true)
			case st.Parallel != nil:
				walk(st.Parallel.Do, path, top, depth+1, inMap)
			}
		}
	}
	walk(s.Steps, "", "", 0, false)
	return out
}

func (s *Spec) FlatChildren() map[string][]Step {
	out := map[string][]Step{}
	for _, st := range s.Steps {
		out[st.ID] = nil
	}
	for _, st := range s.Steps {
		if st.Branch != nil {
			for _, c := range st.Branch.Cases {
				out[st.ID] = append(out[st.ID], c.Do...)
			}
			out[st.ID] = append(out[st.ID], st.Branch.Else...)
		}
		if st.Map != nil {
			out[st.ID] = append(out[st.ID], st.Map.Do...)
		}
		if st.Parallel != nil {
			out[st.ID] = append(out[st.ID], st.Parallel.Do...)
		}
	}
	return out
}
