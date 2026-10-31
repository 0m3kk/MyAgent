package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"myagent/internal/schema"
)

type Manifest struct {
	Schema       int            `yaml:"schema" json:"schema"`
	Name         string         `yaml:"name" json:"name"`
	Description  string         `yaml:"description" json:"description"`
	Kind         string         `yaml:"kind" json:"kind"`
	Runtime      Runtime        `yaml:"runtime" json:"runtime"`
	SideEffect   string         `yaml:"side_effect" json:"side_effect"`
	InputSchema  map[string]any `yaml:"input_schema" json:"input_schema"`
	OutputSchema map[string]any `yaml:"output_schema" json:"output_schema"`
	Permissions  Permissions    `yaml:"permissions" json:"permissions"`
	Secrets      []SecretDecl   `yaml:"secrets" json:"secrets"`
	Limits       Limits         `yaml:"limits" json:"limits"`
	Tests        []TestCase     `yaml:"tests" json:"tests"`
	Metadata     map[string]any `yaml:"metadata" json:"metadata,omitempty"`
}

type Runtime struct {
	Language string `yaml:"language" json:"language,omitempty"`
	Version  string `yaml:"version" json:"version,omitempty"`
	Entry    string `yaml:"entry" json:"entry,omitempty"`
	Deps     string `yaml:"deps" json:"deps,omitempty"`
	Builtin  string `yaml:"builtin" json:"builtin,omitempty"`
}

type Permissions struct {
	Network NetworkPermissions `yaml:"network" json:"network"`
	FS      FSPermissions      `yaml:"fs" json:"fs"`
	Exec    []string           `yaml:"exec" json:"exec"`
}

type NetworkPermissions struct {
	Domains []string `yaml:"domains" json:"domains"`
}

type FSPermissions struct {
	Read  []string `yaml:"read" json:"read"`
	Write []string `yaml:"write" json:"write"`
}

type SecretDecl struct {
	Name        string      `yaml:"name" json:"name"`
	Description string      `yaml:"description" json:"description"`
	Inject      string      `yaml:"inject" json:"inject"`
	Header      string      `yaml:"header" json:"header,omitempty"`
	Scope       SecretScope `yaml:"scope" json:"scope"`
	Required    bool        `yaml:"required" json:"required"`
}

type SecretScope struct {
	Domains []string `yaml:"domains" json:"domains"`
}

type Limits struct {
	TimeoutS int `yaml:"timeout_s" json:"timeout_s"`
	MemoryMB int `yaml:"memory_mb" json:"memory_mb"`
	OutputKB int `yaml:"output_kb" json:"output_kb"`
}

type TestCase struct {
	Name        string         `yaml:"name" json:"name"`
	Input       map[string]any `yaml:"input" json:"input"`
	Mocks       []Mock         `yaml:"mocks" json:"mocks,omitempty"`
	Expect      []string       `yaml:"expect" json:"expect,omitempty"`
	ExpectError string         `yaml:"expect_error" json:"expect_error,omitempty"`
}

type Mock struct {
	Match   string         `yaml:"match" json:"match"`
	Respond map[string]any `yaml:"respond" json:"respond"`
}

type MockResponse struct {
	Status int
	Body   string
	JSON   any
	File   string
}

type Issue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func ParseManifest(data string) (*Manifest, error) {
	dec := yaml.NewDecoder(bytes.NewBufferString(data))
	dec.KnownFields(true)
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	normalizeManifest(&m)
	return &m, nil
}

func normalizeManifest(m *Manifest) {
	if m.Schema == 0 {
		m.Schema = 1
	}
	if m.Kind == "" {
		m.Kind = "starlark"
	}
	if m.SideEffect == "" {
		m.SideEffect = "none"
	}
	if m.InputSchema == nil {
		m.InputSchema = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	if m.Limits.TimeoutS == 0 {
		m.Limits.TimeoutS = 30
	}
	if m.Limits.OutputKB == 0 {
		m.Limits.OutputKB = 512
	}
	if m.Kind == "builtin" && m.Runtime.Builtin == "" {
		m.Runtime.Builtin = m.Name
	}
}

var toolNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)
var secretNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

func ValidateManifest(m *Manifest) []Issue {
	issues := []Issue{}
	add := func(path, msg string) { issues = append(issues, Issue{Path: path, Message: msg}) }
	if m == nil {
		return []Issue{{Path: "", Message: "manifest is empty"}}
	}
	if m.Schema != 1 {
		add("schema", "schema must be 1")
	}
	if !toolNamePattern.MatchString(m.Name) {
		add("name", "tool name must look like group.name using lower_snake_case")
	}
	if strings.TrimSpace(m.Description) == "" {
		add("description", "description is required")
	}
	if len([]rune(m.Description)) > 220 {
		add("description", "description must stay under 220 characters")
	}
	switch m.Kind {
	case "builtin", "starlark", "script", "mcp":
	default:
		add("kind", "kind must be builtin, starlark, script or mcp")
	}
	switch m.SideEffect {
	case "none", "read", "write", "external":
	default:
		add("side_effect", "side_effect must be none, read, write or external")
	}
	if m.Kind == "starlark" || m.Kind == "script" {
		if m.Runtime.Entry == "" {
			add("runtime.entry", "runtime.entry is required")
		}
		if m.Kind == "script" {
			switch m.Runtime.Language {
			case "python", "node", "shell", "binary":
			default:
				add("runtime.language", "runtime.language must be python, node, shell or binary")
			}
		}
	}
	if m.Kind == "builtin" && m.Runtime.Builtin == "" {
		add("runtime.builtin", "builtin runtime name is required")
	}
	if _, err := schema.CompileValue(m.InputSchema); err != nil {
		add("input_schema", err.Error())
	} else {
		if ap, ok := m.InputSchema["additionalProperties"].(bool); !ok || ap {
			add("input_schema.additionalProperties", "input_schema must set additionalProperties: false")
		}
		props, _ := m.InputSchema["properties"].(map[string]any)
		for name, raw := range props {
			pm, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if d, _ := pm["description"].(string); strings.TrimSpace(d) == "" {
				add("input_schema.properties."+name+".description", "every input field needs a description")
			}
		}
	}
	if m.OutputSchema != nil {
		if _, err := schema.CompileValue(m.OutputSchema); err != nil {
			add("output_schema", err.Error())
		}
	}
	for i, d := range m.Permissions.Network.Domains {
		if d == "*" || strings.Contains(d, "*") {
			add(fmt.Sprintf("permissions.network.domains[%d]", i), "wildcard domains are not allowed; list explicit domains")
		}
	}
	if m.Kind == "script" {
		for i, ex := range m.Permissions.Exec {
			if ex == "*" || strings.Contains(ex, "/") && strings.HasPrefix(ex, "/usr/bin/env") {
				add(fmt.Sprintf("permissions.exec[%d]", i), "list explicit binaries, not wildcards")
			}
		}
	}
	seenSecrets := map[string]bool{}
	for i, s := range m.Secrets {
		path := fmt.Sprintf("secrets[%d]", i)
		if !secretNamePattern.MatchString(s.Name) {
			add(path+".name", "secret name must be UPPER_SNAKE_CASE")
		}
		if seenSecrets[s.Name] {
			add(path+".name", "duplicate secret name")
		}
		seenSecrets[s.Name] = true
		switch s.Inject {
		case "env", "header", "file":
		default:
			add(path+".inject", "inject must be env, header or file")
		}
		if s.Inject == "header" {
			if !strings.Contains(s.Header, "${value}") {
				add(path+".header", "header must contain ${value}")
			}
			if len(s.Scope.Domains) == 0 {
				add(path+".scope.domains", "header injection requires explicit domains")
			}
			for _, d := range s.Scope.Domains {
				if !containsString(m.Permissions.Network.Domains, d) {
					add(path+".scope.domains", "secret domain "+d+" must be declared in permissions.network.domains")
				}
			}
		}
	}
	for i, t := range m.Tests {
		if strings.TrimSpace(t.Name) == "" {
			add(fmt.Sprintf("tests[%d].name", i), "test name is required")
		}
		for j, mk := range t.Mocks {
			if mk.Match == "" {
				add(fmt.Sprintf("tests[%d].mocks[%d].match", i, j), "mock match is required")
			}
		}
	}
	if m.Kind != "builtin" && len(m.Tests) > 0 {
		hasOK, hasErr := false, false
		for _, t := range m.Tests {
			if t.ExpectError != "" {
				hasErr = true
			} else {
				hasOK = true
			}
		}
		if !hasOK {
			add("tests", "at least one passing test is required")
		}
		if !hasErr {
			add("tests", "at least one invalid-input test is required")
		}
	}
	if m.Limits.TimeoutS < 1 || m.Limits.TimeoutS > 600 {
		add("limits.timeout_s", "timeout_s must be between 1 and 600")
	}
	if m.Limits.OutputKB < 1 || m.Limits.OutputKB > 10240 {
		add("limits.output_kb", "output_kb must be between 1 and 10240")
	}
	return issues
}

func (m *Manifest) InputSchemaJSON() json.RawMessage {
	raw, _ := json.Marshal(m.InputSchema)
	return raw
}

func (m *Manifest) OutputSchemaJSON() json.RawMessage {
	if m.OutputSchema == nil {
		return json.RawMessage(`{}`)
	}
	raw, _ := json.Marshal(m.OutputSchema)
	return raw
}

func (m *Manifest) PermissionsJSON() json.RawMessage {
	raw, _ := json.Marshal(m.Permissions)
	return raw
}

func (m *Manifest) SecretsJSON() json.RawMessage {
	raw, _ := json.Marshal(m.Secrets)
	return raw
}

func (m *Manifest) TestsJSON() json.RawMessage {
	raw, _ := json.Marshal(m.Tests)
	return raw
}

func (m *Manifest) RuntimeJSON() json.RawMessage {
	raw, _ := json.Marshal(m.Runtime)
	return raw
}

func (m *Manifest) Marshal() string {
	raw, _ := yaml.Marshal(m)
	return string(raw)
}

func (m *Manifest) MockMap(test *TestCase) map[string]MockResponse {
	out := map[string]MockResponse{}
	if test == nil {
		return out
	}
	for _, mk := range test.Mocks {
		resp := MockResponse{Status: 200}
		if v, ok := mk.Respond["status"]; ok {
			switch n := v.(type) {
			case int:
				resp.Status = n
			case float64:
				resp.Status = int(n)
			}
		}
		if v, ok := mk.Respond["file"]; ok {
			resp.File = fmt.Sprint(v)
		}
		if v, ok := mk.Respond["body"]; ok {
			resp.Body = fmt.Sprint(v)
		}
		if v, ok := mk.Respond["json"]; ok {
			resp.JSON = v
		} else if resp.Body == "" && resp.File == "" {
			// A shorthand `respond: {issues: [...]}` is convenient and common.
			resp.JSON = mk.Respond
		}
		out[mk.Match] = resp
	}
	return out
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func ValidateInput(m *Manifest, input map[string]any) []schema.Issue {
	s, err := schema.CompileValue(m.InputSchema)
	if err != nil {
		return []schema.Issue{{Path: "", Message: err.Error()}}
	}
	normalized := s.ApplyDefaults(input)
	vm, ok := normalized.(map[string]any)
	if !ok {
		return []schema.Issue{{Path: "", Message: "input must be an object"}}
	}
	// Copy defaults back so callers see them.
	for k, v := range vm {
		input[k] = v
	}
	return s.Validate(normalized)
}

func ValidateOutput(m *Manifest, output any) []schema.Issue {
	if m.OutputSchema == nil {
		return nil
	}
	s, err := schema.CompileValue(m.OutputSchema)
	if err != nil {
		return []schema.Issue{{Path: "", Message: err.Error()}}
	}
	return s.Validate(output)
}

func ManifestFromModel(modelYAML string) (*Manifest, error) {
	m, err := ParseManifest(modelYAML)
	if err != nil {
		return nil, err
	}
	if len(ValidateManifest(m)) > 0 {
		msgs := []string{}
		for _, i := range ValidateManifest(m) {
			msgs = append(msgs, i.Path+": "+i.Message)
		}
		return m, errors.New(strings.Join(msgs, "; "))
	}
	return m, nil
}
