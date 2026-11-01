package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"myagent/internal/eventbus"
	"myagent/internal/llm"
	"myagent/internal/store"
)

type ToolBinding struct {
	Name        string
	VersionID   string
	ContentHash string
	SideEffect  string
	Description string
	InputSchema map[string]any
	Secrets     []string
	Kind        string
}

type ToolExecOptions struct {
	RunID     string
	StepID    string
	StepRunID string
	IdemKey   string
	Source    string
	DryRun    bool
	OnLog     func(stream, text string)
}

type ToolRunner interface {
	Resolve(ctx context.Context, ref string) (ToolBinding, error)
	Execute(
		ctx context.Context,
		binding ToolBinding,
		input map[string]any,
		opts ToolExecOptions,
	) (any, error)
}

type ToolError struct {
	Kind      string // invalid_input | permission_denied | timeout | crashed | rate_limited | tool_error
	Message   string
	Retryable bool
}

func (e *ToolError) Error() string {
	if e.Kind != "" {
		return e.Kind + ": " + e.Message
	}
	return e.Message
}

type LLMChatRequest struct {
	RunID        string
	StepID       string
	SessionID    string
	Model        string
	Task         string
	System       string
	Messages     []llm.Message
	Tools        []llm.ToolDef
	OutputSchema map[string]any
	MaxTokens    int
	Temperature  *float64
	Stream       func(text string)
}

type LLMChatResult struct {
	Message   llm.Message
	Text      string
	Data      any
	TokensIn  int
	TokensOut int
	CostMicro int64
	Model     string
	Finish    string
}

type LLMRunner interface {
	ChatWithTools(ctx context.Context, req LLMChatRequest) (LLMChatResult, error)
}

type ApprovalRequest struct {
	RunID          string
	StepID         string
	StepRunID      string
	ConversationID string
	Kind           string
	Title          string
	Description    string
	Message        string
	Show           any
	Source         string
	Unattended     string
	ExpiresAt      time.Time
}

type ApprovalDecision struct {
	Approved  bool
	Comment   string
	DecidedBy string
}

type ApprovalBroker interface {
	RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalDecision, error)
}

type WorkflowLoader interface {
	LoadWorkflow(ctx context.Context, ref string) (*Spec, string, error)
}

type RunControls interface {
	WaitIfPaused(ctx context.Context, runID string) error
}

type RunRequest struct {
	RunID          string
	Source         string
	ConversationID string
	Inputs         map[string]any
	Unattended     string
	DryRun         bool
	Resume         bool
	Replay         map[string]any
	Bindings       map[string]ToolBinding
	Depth          int
}

type RunResult struct {
	Status       string            `json:"status"`
	Outputs      map[string]any    `json:"outputs"`
	TokensIn     int               `json:"tokens_in"`
	TokensOut    int               `json:"tokens_out"`
	CostMicro    int64             `json:"cost_micro"`
	Error        string            `json:"error,omitempty"`
	StepStatuses map[string]string `json:"step_statuses,omitempty"`
}

type Engine struct {
	Store       *store.Store
	Bus         *eventbus.Bus
	Tools       ToolRunner
	LLM         LLMRunner
	Approvals   ApprovalBroker
	Controls    RunControls
	Loader      WorkflowLoader
	ArtifactDir string
	Redact      func(string) string
	Notifier    func(title, message string)
}

func (e *Engine) redact(s string) string {
	if e.Redact == nil {
		return s
	}
	return e.Redact(s)
}

func (e *Engine) redactRaw(raw []byte) []byte {
	if e.Redact == nil || len(raw) == 0 {
		return raw
	}
	return []byte(e.Redact(string(raw)))
}

func (e *Engine) redactPayload(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		raw = json.RawMessage(fmt.Sprintf(`{"error":%q}`, err.Error()))
	}
	return e.redactRaw(raw)
}

func (e *Engine) delay(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
