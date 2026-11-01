export interface Workspace {
  id: string
  name: string
  root_path: string
  created_at: number
}

export interface WorkspaceProfile {
  id: string
  name: string
  root_path: string
  data_dir: string
  created_at: number
  last_opened_at: number
}

export interface WorkspaceList {
  enabled: boolean
  active: string
  workspaces: WorkspaceProfile[]
}

export interface Settings {
  global_concurrency: number
  agent_max_steps: number
  agent_budget_tokens: number
  notifications: boolean
  default_model_id: string
  theme: string
  font_scale: number
  retention_days: number
  auto_approve_reads: boolean
}

export interface ModelProfile {
  id: string
  name: string
  base_url: string
  model: string
  role: string
  params: Record<string, unknown>
  capabilities: Record<string, unknown>
  secret_id?: string
  created_at: number
  updated_at: number
}

export interface Conversation {
  id: string
  workspace_id: string
  title: string
  created_at: number
  updated_at: number
}

export interface ChatProgress {
  step: number
  max_steps: number
  used_tokens: number
  budget_tokens: number
  started_at: number
}

export interface Part {
  type: string
  text?: string
  tool_call_id?: string
  name?: string
  args?: unknown
  result?: unknown
  status?: string
  error?: string
  duration_ms?: number
  logs?: string[]
}

export interface MessageContent {
  text?: string
  parts?: Part[]
}

export interface Message {
  id: string
  conversation_id: string
  seq: number
  role: string
  content: MessageContent
  model_profile_id?: string
  tokens_in: number
  tokens_out: number
  cost_micro: number
  created_at: number
}

export interface WorkflowVersion {
  id: string
  workflow_id: string
  version: number
  yaml: string
  input_schema: Record<string, unknown>
  content_hash: string
  status: string
  change_note: string
  parent_id?: string
  created_by: string
  created_at: number
}

export interface WorkflowStepView {
  id: string
  kind: string
  needs: string[]
  tool?: string
  summary?: string
}

export interface Workflow {
  id: string
  workspace_id: string
  name: string
  current_version_id?: string
  folder_id?: string
  pinned?: boolean
  created_at: number
  version?: WorkflowVersion
  versions?: WorkflowVersion[]
  steps?: WorkflowStepView[]
}

export interface Folder {
  id: string
  workspace_id: string
  kind: 'tool' | 'workflow'
  name: string
  parent_id?: string
  created_at: number
}

export interface InputSpec {
  type: string
  description?: string
  default?: unknown
  enum?: unknown[]
  format?: string
}

export interface StepSpec {
  id: string
  name?: string
  needs?: string[]
  when?: string
  timeout?: string
  retry?: { max: number; backoff?: string; delay?: string }
  on_error?: string
  side_effect?: string
  tool?: string
  with?: Record<string, unknown>
  llm?: { role?: string; prompt: string; system?: string; output_schema?: Record<string, unknown>; max_tokens?: number; temperature?: number }
  agent?: { role?: string; goal: string; tools?: string[]; max_steps?: number; output_schema?: Record<string, unknown> }
  set?: Record<string, unknown>
  branch?: { cases?: { when: string; do: StepSpec[] }[]; else?: StepSpec[] }
  map?: { over: string; as?: string; do: StepSpec[]; concurrency?: number }
  parallel?: { do: StepSpec[] }
  approval?: { message: string; show?: string; timeout?: string }
  workflow?: string
}

export interface WorkflowSpec {
  schema: number
  name: string
  description?: string
  inputs?: Record<string, InputSpec>
  outputs?: Record<string, string>
  settings?: {
    unattended?: string
    concurrency?: { max?: number; policy?: string }
    budget?: { max_cost_usd?: number; max_tokens?: number }
    timeout?: string
  }
  defaults?: { timeout?: string; retry?: { max: number; backoff?: string; delay?: string }; role?: string }
  steps: StepSpec[]
}

export interface Trigger {
  id: string
  workflow_id: string
  kind: string
  config: Record<string, unknown>
  inputs: Record<string, unknown>
  enabled: boolean
  unattended: string
  concurrency: string
  last_fired_at?: number
  approved_at?: number
  created_at: number
}

export interface StepRun {
  id: string
  run_id: string
  step_id: string
  attempt: number
  status: string
  input_ref?: string
  output_ref?: string
  error?: string
  idem_key?: string
  started_at: number
  finished_at: number
}

export interface Run {
  id: string
  target_kind: 'workflow' | 'tool'
  target_id?: string
  target_ver: string
  target_name?: string
  source: string
  trigger_id?: string
  conversation_id?: string
  parent_run_id?: string
  inputs: Record<string, unknown>
  status: string
  started_at: number
  finished_at: number
  tokens_in: number
  tokens_out: number
  cost_micro: number
  error?: string
  created_at: number
  steps?: StepRun[]
  summary?: string
}

export interface EventEnvelope {
  id: number
  stream: string
  seq: number
  type: string
  ts: number
  run_id?: string
  step_id?: string
  schema_ver: number
  payload: Record<string, unknown>
  ephemeral?: boolean
}

export interface Approval {
  id: string
  run_id?: string
  step_run_id?: string
  conversation_id?: string
  kind: string
  status: string
  request: ApprovalRequest
  decision?: string
  comment?: string
  expires_at?: number
  decided_at?: number
  created_at: number
}

export interface ApprovalRequest {
  id?: string
  kind?: string
  title?: string
  description?: string
  message?: string
  show?: unknown
  run_id?: string
  step_id?: string
  step_run_id?: string
  conversation_id?: string
  source?: string
  unattended?: string
  expires_at?: number
  tool?: string
  tool_version_id?: string
  side_effect?: string
  args?: unknown
  secret_names?: string[]
  secret_status?: Record<string, string>
  permissions?: unknown
  secrets?: unknown
  tests?: unknown
  summary?: string
  workflow?: string
  config?: unknown
  inputs?: unknown
  question?: string
  options?: string[]
  [key: string]: unknown
}

export interface ToolVersion {
  id: string
  tool_id: string
  version: number
  manifest: string
  code_path?: string
  content_hash: string
  status: string
  created_by: string
  parent_id?: string
  approved_at?: number
  created_at: number
}

export interface Tool {
  id: string
  workspace_id: string
  name: string
  kind: string
  current_version_id?: string
  draft_version_id?: string
  folder_id?: string
  pinned?: boolean
  created_at: number
  version?: ToolVersion
  versions?: ToolVersion[]
  description?: string
  input_schema?: Record<string, unknown>
  output_schema?: Record<string, unknown>
  side_effect?: string
  permissions?: Record<string, unknown>
  secrets?: unknown[]
  tests?: unknown[]
  runtime?: Record<string, unknown>
}

export interface ToolFile {
  path: string
  size: number
}

export interface ToolDetail {
  tool: Tool
  versions: ToolVersion[]
  files: ToolFile[]
  bindings: { tool_id: string; logical_name: string; secret_id: string; scope_hash: string; approved_at: number }[]
  manifest_yaml: string
}

export interface TestResult {
  name: string
  passed: boolean
  error?: string
  output?: unknown
  logs?: string[]
  duration_ms: number
}

export interface SecretMeta {
  id: string
  name: string
  kind: string
  description: string
  expires_at?: number
  last_used_at?: number
  ask_each_run: boolean
  created_at: number
  status: 'present' | 'missing' | 'expired'
  used_by?: string[]
}

export interface Feedback {
  id: string
  target_kind: string
  target_id: string
  rating: 'up' | 'down'
  note: string
  created_at: number
}

export interface EvalCase {
  id: string
  suite_id: string
  source_run_id?: string
  inputs: Record<string, unknown>
  assertions: string[]
  recorded_io_ref?: string
  split: string
  created_at: number
}

export interface EvalSuite {
  id: string
  name: string
  target_kind: string
  target_id: string
  created_at: number
  cases?: EvalCase[]
}

export interface EvalResult {
  id: string
  eval_run_id: string
  case_id: string
  target_ver: string
  passed: boolean
  score: number
  judge: string
  cost_micro: number
  latency_ms: number
  output?: unknown
  error?: string
  created_at: number
}

export interface EvalRun {
  id: string
  suite_id: string
  target_ver: string
  status: string
  started_at: number
  finished_at: number
  results?: EvalResult[]
}

export interface TuningProposal {
  id: string
  target_kind: string
  target_id: string
  base_version_id: string
  diagnosis: string
  patch: string
  proposed_yaml?: string
  status: string
  eval_before?: unknown
  eval_after?: unknown
  created_at: number
}

export interface Bootstrap {
  workspace: Workspace
  settings: Settings
  model_profiles: ModelProfile[]
  key_kind: string
  key_description: string
  conversations: Conversation[]
  workflows: Workflow[]
  tools: Tool[]
  folders: Folder[]
  runs: Run[]
  approvals: Approval[]
  secret_count: number
  version: string
  pending_count: number
  counts: Record<string, number>
}

export interface ValidationDiagnostic {
  code: string
  path: string
  message: string
  hint?: string
  suggest?: string[]
}

export interface ValidationResult {
  errors: ValidationDiagnostic[]
  warnings: ValidationDiagnostic[]
  input_schema?: Record<string, unknown>
}

export interface WorkflowDetail {
  workflow: Workflow
  versions: WorkflowVersion[]
  triggers: Trigger[]
  runs: Run[]
  validation: ValidationResult
  spec?: WorkflowSpec
  input_schema?: Record<string, unknown>
}

export interface RunDetail {
  run: Run
  events: EventEnvelope[]
  approvals: Approval[]
  outputs: Record<string, unknown>
  step_outputs: Record<string, unknown>
  feedback?: Feedback
  version?: WorkflowVersion
  workflow?: Workflow
  spec?: WorkflowSpec
  spec_stale?: boolean
}

export interface ToolDetailResponse {
  result: { tool: Tool; version: ToolVersion; issues: unknown[] }
  detail: ToolDetail
}

export const RUN_TERMINAL = ['succeeded', 'failed', 'cancelled', 'expired']
