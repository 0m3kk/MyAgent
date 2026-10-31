package model

import (
	"encoding/json"
	"time"
)

// Event is the append-only contract between the Go core, the UI, trace and eval.
// Persisted events are written to SQLite before they are broadcast.
type Event struct {
	ID        int64           `json:"id"`
	Stream    string          `json:"stream"`
	Seq       int64           `json:"seq"`
	Type      string          `json:"type"`
	TS        int64           `json:"ts"`
	RunID     string          `json:"run_id,omitempty"`
	StepID    string          `json:"step_id,omitempty"`
	SchemaVer int             `json:"schema_ver"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Ephemeral bool            `json:"-"`
}

const (
	StreamSys        = "sys"
	StreamConvPrefix = "conv:"
	StreamRunPrefix  = "run:"
)

func ConvStream(id string) string { return StreamConvPrefix + id }
func RunStream(id string) string  { return StreamRunPrefix + id }

// Run statuses.
const (
	RunQueued          = "queued"
	RunRunning         = "running"
	RunPaused          = "paused"
	RunWaitingApproval = "waiting_approval"
	RunWaitingSecret   = "waiting_secret"
	RunSucceeded       = "succeeded"
	RunFailed          = "failed"
	RunCancelled       = "cancelled"
	RunExpired         = "expired"
)

func RunTerminal(status string) bool {
	switch status {
	case RunSucceeded, RunFailed, RunCancelled, RunExpired:
		return true
	}
	return false
}

type Workspace struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	RootPath  string `json:"root_path"`
	CreatedAt int64  `json:"created_at"`
}

type ModelProfile struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	BaseURL      string          `json:"base_url"`
	Model        string          `json:"model"`
	Role         string          `json:"role"`
	Params       json.RawMessage `json:"params"`
	Capabilities json.RawMessage `json:"capabilities"`
	SecretID     string          `json:"secret_id,omitempty"`
	CreatedAt    int64           `json:"created_at"`
	UpdatedAt    int64           `json:"updated_at"`
}

type Conversation struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Title       string `json:"title"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

type Part struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
	Args       json.RawMessage `json:"args,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	Status     string          `json:"status,omitempty"`
	Error      string          `json:"error,omitempty"`
	DurationMS int64           `json:"duration_ms,omitempty"`
	Logs       []string        `json:"logs,omitempty"`
}

type MessageContent struct {
	Text  string `json:"text,omitempty"`
	Parts []Part `json:"parts,omitempty"`
}

type Message struct {
	ID             string          `json:"id"`
	ConversationID string          `json:"conversation_id"`
	Seq            int64           `json:"seq"`
	Role           string          `json:"role"`
	Content        json.RawMessage `json:"content"`
	ModelProfileID string          `json:"model_profile_id,omitempty"`
	TokensIn       int             `json:"tokens_in"`
	TokensOut      int             `json:"tokens_out"`
	CostMicro      int64           `json:"cost_micro"`
	CreatedAt      int64           `json:"created_at"`
}

func (m Message) ContentStruct() MessageContent {
	var c MessageContent
	_ = json.Unmarshal(m.Content, &c)
	return c
}

const (
	ToolDraft      = "draft"
	ToolTested     = "tested"
	ToolApproved   = "approved"
	ToolActive     = "active"
	ToolDeprecated = "deprecated"
)

type Tool struct {
	ID               string `json:"id"`
	WorkspaceID      string `json:"workspace_id"`
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	CurrentVersionID string `json:"current_version_id,omitempty"`
	DraftVersionID   string `json:"draft_version_id,omitempty"`
	CreatedAt        int64  `json:"created_at"`
}

type ToolVersion struct {
	ID          string `json:"id"`
	ToolID      string `json:"tool_id"`
	Version     int    `json:"version"`
	Manifest    string `json:"manifest"`
	CodePath    string `json:"code_path,omitempty"`
	ContentHash string `json:"content_hash"`
	Status      string `json:"status"`
	CreatedBy   string `json:"created_by"`
	ParentID    string `json:"parent_id,omitempty"`
	ApprovedAt  int64  `json:"approved_at,omitempty"`
	CreatedAt   int64  `json:"created_at"`
}

type ToolWithVersion struct {
	Tool
	Version      *ToolVersion    `json:"version,omitempty"`
	Versions     []ToolVersion   `json:"versions,omitempty"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema,omitempty"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
	SideEffect   string          `json:"side_effect,omitempty"`
	Permissions  json.RawMessage `json:"permissions,omitempty"`
	Secrets      json.RawMessage `json:"secrets,omitempty"`
	Tests        json.RawMessage `json:"tests,omitempty"`
	Runtime      json.RawMessage `json:"runtime,omitempty"`
}

type Workflow struct {
	ID               string `json:"id"`
	WorkspaceID      string `json:"workspace_id"`
	Name             string `json:"name"`
	CurrentVersionID string `json:"current_version_id,omitempty"`
	CreatedAt        int64  `json:"created_at"`
}

type WorkflowVersion struct {
	ID          string          `json:"id"`
	WorkflowID  string          `json:"workflow_id"`
	Version     int             `json:"version"`
	YAML        string          `json:"yaml"`
	InputSchema json.RawMessage `json:"input_schema"`
	ContentHash string          `json:"content_hash"`
	Status      string          `json:"status"`
	ChangeNote  string          `json:"change_note"`
	ParentID    string          `json:"parent_id,omitempty"`
	CreatedBy   string          `json:"created_by"`
	CreatedAt   int64           `json:"created_at"`
}

type WorkflowWithVersion struct {
	Workflow
	Version  *WorkflowVersion   `json:"version,omitempty"`
	Versions []WorkflowVersion  `json:"versions,omitempty"`
	Steps    []WorkflowStepView `json:"steps,omitempty"`
}

type WorkflowStepView struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Needs   []string `json:"needs"`
	Tool    string   `json:"tool,omitempty"`
	Summary string   `json:"summary,omitempty"`
}

type Trigger struct {
	ID          string          `json:"id"`
	WorkflowID  string          `json:"workflow_id"`
	Kind        string          `json:"kind"`
	Config      json.RawMessage `json:"config"`
	Inputs      json.RawMessage `json:"inputs"`
	Enabled     bool            `json:"enabled"`
	Unattended  string          `json:"unattended"`
	Concurrency string          `json:"concurrency"`
	LastFiredAt int64           `json:"last_fired_at,omitempty"`
	ApprovedAt  int64           `json:"approved_at,omitempty"`
	CreatedAt   int64           `json:"created_at"`
}

type Run struct {
	ID             string          `json:"id"`
	TargetKind     string          `json:"target_kind"`
	TargetID       string          `json:"target_id,omitempty"`
	TargetVer      string          `json:"target_ver"`
	TargetName     string          `json:"target_name,omitempty"`
	Source         string          `json:"source"`
	TriggerID      string          `json:"trigger_id,omitempty"`
	ConversationID string          `json:"conversation_id,omitempty"`
	ParentRunID    string          `json:"parent_run_id,omitempty"`
	Inputs         json.RawMessage `json:"inputs"`
	Status         string          `json:"status"`
	StartedAt      int64           `json:"started_at"`
	FinishedAt     int64           `json:"finished_at"`
	TokensIn       int             `json:"tokens_in"`
	TokensOut      int             `json:"tokens_out"`
	CostMicro      int64           `json:"cost_micro"`
	Error          string          `json:"error,omitempty"`
	CreatedAt      int64           `json:"created_at"`
	Steps          []StepRun       `json:"steps,omitempty"`
	Summary        string          `json:"summary,omitempty"`
}

type StepRun struct {
	ID         string `json:"id"`
	RunID      string `json:"run_id"`
	StepID     string `json:"step_id"`
	Attempt    int    `json:"attempt"`
	Status     string `json:"status"`
	InputRef   string `json:"input_ref,omitempty"`
	OutputRef  string `json:"output_ref,omitempty"`
	Error      string `json:"error,omitempty"`
	IdemKey    string `json:"idem_key,omitempty"`
	StartedAt  int64  `json:"started_at"`
	FinishedAt int64  `json:"finished_at"`
}

type Approval struct {
	ID             string          `json:"id"`
	RunID          string          `json:"run_id,omitempty"`
	StepRunID      string          `json:"step_run_id,omitempty"`
	ConversationID string          `json:"conversation_id,omitempty"`
	Kind           string          `json:"kind"`
	Status         string          `json:"status"`
	Request        json.RawMessage `json:"request"`
	Decision       string          `json:"decision,omitempty"`
	Comment        string          `json:"comment,omitempty"`
	ExpiresAt      int64           `json:"expires_at,omitempty"`
	DecidedAt      int64           `json:"decided_at,omitempty"`
	CreatedAt      int64           `json:"created_at"`
}

type ApprovalRequest struct {
	ID          string          `json:"id"`
	RunID       string          `json:"run_id,omitempty"`
	StepRunID   string          `json:"step_run_id,omitempty"`
	Kind        string          `json:"kind"` // tool | step | secret | trigger
	Title       string          `json:"title"`
	Description string          `json:"description,omitempty"`
	ToolName    string          `json:"tool_name,omitempty"`
	ToolVersion string          `json:"tool_version_id,omitempty"`
	SideEffect  string          `json:"side_effect,omitempty"`
	SecretNames []string        `json:"secret_names,omitempty"`
	Domains     []string        `json:"domains,omitempty"`
	Preview     json.RawMessage `json:"preview,omitempty"`
	ExpiresAt   int64           `json:"expires_at,omitempty"`
}

type Artifact struct {
	ID        string `json:"id"`
	RunID     string `json:"run_id,omitempty"`
	StepRunID string `json:"step_run_id,omitempty"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Mime      string `json:"mime"`
	CreatedAt int64  `json:"created_at"`
}

type Feedback struct {
	ID         string `json:"id"`
	TargetKind string `json:"target_kind"`
	TargetID   string `json:"target_id"`
	Rating     string `json:"rating"`
	Note       string `json:"note"`
	CreatedAt  int64  `json:"created_at"`
}

type EvalSuite struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	TargetKind string     `json:"target_kind"`
	TargetID   string     `json:"target_id"`
	CreatedAt  int64      `json:"created_at"`
	Cases      []EvalCase `json:"cases,omitempty"`
}

type EvalCase struct {
	ID            string          `json:"id"`
	SuiteID       string          `json:"suite_id"`
	SourceRunID   string          `json:"source_run_id,omitempty"`
	Inputs        json.RawMessage `json:"inputs"`
	Assertions    json.RawMessage `json:"assertions"`
	RecordedIORef string          `json:"recorded_io_ref,omitempty"`
	Split         string          `json:"split"`
	CreatedAt     int64           `json:"created_at"`
}

type EvalRun struct {
	ID         string       `json:"id"`
	SuiteID    string       `json:"suite_id"`
	TargetVer  string       `json:"target_ver"`
	Status     string       `json:"status"`
	StartedAt  int64        `json:"started_at"`
	FinishedAt int64        `json:"finished_at"`
	Results    []EvalResult `json:"results,omitempty"`
}

type EvalResult struct {
	ID        string          `json:"id"`
	EvalRunID string          `json:"eval_run_id"`
	CaseID    string          `json:"case_id"`
	TargetVer string          `json:"target_ver"`
	Passed    bool            `json:"passed"`
	Score     float64         `json:"score"`
	Judge     string          `json:"judge,omitempty"`
	CostMicro int64           `json:"cost_micro"`
	LatencyMS int64           `json:"latency_ms"`
	Output    json.RawMessage `json:"output,omitempty"`
	Error     string          `json:"error,omitempty"`
	CreatedAt int64           `json:"created_at"`
}

type TuningProposal struct {
	ID            string          `json:"id"`
	TargetKind    string          `json:"target_kind"`
	TargetID      string          `json:"target_id"`
	BaseVersionID string          `json:"base_version_id"`
	Diagnosis     string          `json:"diagnosis"`
	Patch         string          `json:"patch"`
	ProposedYAML  string          `json:"proposed_yaml,omitempty"`
	Status        string          `json:"status"`
	EvalBefore    json.RawMessage `json:"eval_before,omitempty"`
	EvalAfter     json.RawMessage `json:"eval_after,omitempty"`
	CreatedAt     int64           `json:"created_at"`
}

type PromptVersion struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Version   int    `json:"version"`
	Content   string `json:"content"`
	Status    string `json:"status"`
	CreatedBy string `json:"created_by"`
	CreatedAt int64  `json:"created_at"`
}

type Secret struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	ExpiresAt   int64  `json:"expires_at,omitempty"`
	LastUsedAt  int64  `json:"last_used_at,omitempty"`
	AskEachRun  bool   `json:"ask_each_run"`
	CreatedAt   int64  `json:"created_at"`
	Ciphertext  []byte `json:"-"`
	Nonce       []byte `json:"-"`
	KeyVersion  int    `json:"-"`
}

type SecretMeta struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Description string   `json:"description"`
	ExpiresAt   int64    `json:"expires_at,omitempty"`
	LastUsedAt  int64    `json:"last_used_at,omitempty"`
	AskEachRun  bool     `json:"ask_each_run"`
	CreatedAt   int64    `json:"created_at"`
	Status      string   `json:"status"` // present | missing | expired
	UsedBy      []string `json:"used_by,omitempty"`
}

type SecretBinding struct {
	ToolID      string `json:"tool_id"`
	LogicalName string `json:"logical_name"`
	SecretID    string `json:"secret_id"`
	ScopeHash   string `json:"scope_hash"`
	ApprovedAt  int64  `json:"approved_at"`
}

type RunFilter struct {
	TargetID string
	Status   string
	Source   string
	Limit    int
	Offset   int
}

type ToolFilter struct {
	Kind   string
	Status string
	Query  string
}

type EventFilter struct {
	Stream string
	After  int64
	Limit  int
	Type   string
}

func Now() int64 { return time.Now().UnixMilli() }
