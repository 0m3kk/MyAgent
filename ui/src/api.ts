import type {
  Approval,
  Bootstrap,
  Conversation,
  EvalRun,
  EvalSuite,
  EventEnvelope,
  Message,
  ModelProfile,
  Run,
  RunDetail,
  SecretMeta,
  Settings,
  TestResult,
  TuningProposal,
  ValidationResult,
  Workflow,
  WorkflowDetail,
} from './types'

const TOKEN_KEY = 'myagent_token'
let token = ''
let tokenPromise: Promise<string> | null = null

function readTokenFromURL() {
  const params = new URLSearchParams(window.location.search)
  const t = params.get('token')
  if (t) {
    token = t
    localStorage.setItem(TOKEN_KEY, t)
    params.delete('token')
    const qs = params.toString()
    window.history.replaceState(null, '', window.location.pathname + (qs ? `?${qs}` : '') + window.location.hash)
  } else {
    token = localStorage.getItem(TOKEN_KEY) ?? ''
  }
}
readTokenFromURL()

async function ensureToken(): Promise<string> {
  if (token) return token
  if (tokenPromise) return tokenPromise
  tokenPromise = (async () => {
    try {
      const res = await fetch('/api/dev-token', { credentials: 'same-origin' })
      if (res.ok) {
        const data = (await res.json()) as { token: string }
        token = data.token
        localStorage.setItem(TOKEN_KEY, token)
      }
    } catch {
      // Release builds use a SameSite cookie instead of a readable token.
    }
    return token
  })()
  return tokenPromise
}

export function getToken() {
  return token
}

async function request<T>(path: string, init: RequestInit = {}, retry = true): Promise<T> {
  await ensureToken()
  const headers = new Headers(init.headers)
  if (init.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json')
  if (token) headers.set('Authorization', `Bearer ${token}`)
  const res = await fetch(path, { ...init, headers, credentials: 'same-origin' })
  if (res.status === 401 && retry) {
    token = ''
    localStorage.removeItem(TOKEN_KEY)
    tokenPromise = null
    await ensureToken()
    return request<T>(path, init, false)
  }
  const text = await res.text()
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = { error: text }
    }
  }
  if (!res.ok) {
    const error = (data as { error?: string } | null)?.error ?? `Request failed (${res.status})`
    const err = new Error(error) as Error & { status?: number; details?: unknown }
    err.status = res.status
    err.details = data
    throw err
  }
  return data as T
}

function json(body: unknown): RequestInit {
  return { method: 'POST', body: JSON.stringify(body) }
}

export const api = {
  bootstrap: () => request<Bootstrap>('/api/bootstrap'),
  health: () => request<{ ok: boolean; version: string }>('/api/health'),

  conversations: () => request<{ conversations: Conversation[] }>('/api/conversations'),
  createConversation: (title = 'New chat') => request<Conversation>('/api/conversations', json({ title })),
  conversation: (id: string) => request<{ conversation: Conversation; messages: Message[] }>(`/api/conversations/${id}`),
  deleteConversation: (id: string) => request<{ ok: boolean }>(`/api/conversations/${id}`, { method: 'DELETE' }),
  sendChat: (conversationId: string, text: string) =>
    request<{ conversation: Conversation; message: Message }>('/api/chat', json({ conversation_id: conversationId, text })),
  stopChat: (id: string) => request<{ ok: boolean }>(`/api/conversations/${id}/stop`, { method: 'POST' }),
  saveAsWorkflow: (id: string, name?: string) =>
    request<{ workflow: Workflow; version: unknown; validation: ValidationResult }>(`/api/conversations/${id}/save-workflow`, json({ name })),

  workflows: () => request<{ workflows: Workflow[] }>('/api/workflows'),
  workflow: (id: string) => request<WorkflowDetail>(`/api/workflows/${id}`),
  validateWorkflow: (yaml: string, forRun = false) => request<ValidationResult>('/api/workflows/validate', json({ yaml, for_run: forRun })),
  saveWorkflow: (body: { yaml: string; change_note?: string; base_version?: number; approve?: boolean }) =>
    request<{ workflow: Workflow; version: { id: string; version: number }; validation: ValidationResult }>('/api/workflows', json(body)),
  approveWorkflow: (id: string, versionId: string) =>
    request<{ ok: boolean }>(`/api/workflows/${id}/versions/${versionId}/approve`, { method: 'POST' }),
  deleteWorkflow: (id: string) => request<{ ok: boolean }>(`/api/workflows/${id}`, { method: 'DELETE' }),
  workflowEditChat: (id: string) => request<Conversation>(`/api/workflows/${id}/edit-chat`, { method: 'POST' }),
  runWorkflow: (id: string, inputs: Record<string, unknown>, versionId?: string, source = 'manual', unattended?: string) =>
    request<{ run: Run }>(`/api/workflows/${id}/run`, json({ inputs, version_id: versionId, source, unattended })),
  triggers: (workflowId: string) => request<{ triggers: import('./types').Trigger[] }>(`/api/workflows/${workflowId}/triggers`),
  createTrigger: (workflowId: string, body: Record<string, unknown>) =>
    request<import('./types').Trigger>(`/api/workflows/${workflowId}/triggers`, json(body)),
  updateTrigger: (id: string, body: Record<string, unknown>) =>
    request<import('./types').Trigger>(`/api/triggers/${id}`, { method: 'PATCH', body: JSON.stringify(body) }),
  deleteTrigger: (id: string) => request<{ ok: boolean }>(`/api/triggers/${id}`, { method: 'DELETE' }),

  runs: (params: Record<string, string | number | undefined> = {}) => {
    const q = new URLSearchParams()
    for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== '') q.set(k, String(v))
    return request<{ runs: Run[] }>(`/api/runs?${q.toString()}`)
  },
  run: (id: string) => request<RunDetail>(`/api/runs/${id}`),
  runEvents: (id: string, after = 0) => request<{ events: EventEnvelope[] }>(`/api/runs/${id}/events?after=${after}`),
  pauseRun: (id: string) => request<{ ok: boolean }>(`/api/runs/${id}/pause`, { method: 'POST' }),
  resumeRun: (id: string) => request<{ ok: boolean }>(`/api/runs/${id}/resume`, { method: 'POST' }),
  cancelRun: (id: string) => request<{ ok: boolean }>(`/api/runs/${id}/cancel`, { method: 'POST' }),
  rerun: (id: string) => request<{ run: Run }>(`/api/runs/${id}/rerun`, { method: 'POST' }),
  feedback: (id: string, rating: 'up' | 'down', note = '') =>
    request<import('./types').Feedback>(`/api/runs/${id}/feedback`, json({ rating, note })),
  evalCase: (id: string) => request<import('./types').EvalCase>(`/api/runs/${id}/eval-case`, { method: 'POST' }),

  tools: () => request<{ tools: import('./types').Tool[] }>('/api/tools'),
  tool: (id: string) => request<import('./types').ToolDetail>(`/api/tools/${id}`),
  createTool: (manifest_yaml: string, files?: Record<string, string>) =>
    request<import('./types').ToolDetailResponse>('/api/tools', json({ manifest_yaml, files })),
  writeToolFile: (id: string, vid: string, path: string, content: string) =>
    request<import('./types').ToolVersion>(`/api/tools/${id}/versions/${vid}/files`, { method: 'PUT', body: JSON.stringify({ path, content }) }),
  readToolFile: (id: string, vid: string, path: string) =>
    request<{ path: string; content: string }>(`/api/tools/${id}/versions/${vid}/files?path=${encodeURIComponent(path)}`),
  testTool: (id: string, vid: string) =>
    request<{ passed: boolean; tests: TestResult[] }>(`/api/tools/${id}/versions/${vid}/test`, { method: 'POST' }),
  approveTool: (id: string, vid: string) =>
    request<{ ok: boolean }>(`/api/tools/${id}/versions/${vid}/approve`, { method: 'POST' }),
  runTool: (id: string, inputs: Record<string, unknown>, versionId?: string) =>
    request<{ run: Run }>(`/api/tools/${id}/run`, json({ inputs, version_id: versionId })),
  bindSecret: (id: string, logical_name: string, secret_id: string) =>
    request<{ ok: boolean }>(`/api/tools/${id}/secrets`, json({ logical_name, secret_id })),
  deleteTool: (id: string) => request<{ ok: boolean }>(`/api/tools/${id}`, { method: 'DELETE' }),

  approvals: () => request<{ approvals: Approval[] }>('/api/approvals'),
  decideApproval: (id: string, decision: 'approve' | 'reject', comment = '') =>
    request<Approval>(`/api/approvals/${id}/decision`, json({ decision, comment })),

  secrets: () => request<{ secrets: SecretMeta[]; key_kind: string; key_description: string }>('/api/secrets'),
  putSecret: (body: { name: string; kind?: string; description?: string; value: string; expires_at?: number; ask_each_run?: boolean }) =>
    request<SecretMeta>('/api/secrets', json(body)),
  deleteSecret: (id: string) => request<{ ok: boolean }>(`/api/secrets/${id}`, { method: 'DELETE' }),

  models: () => request<{ models: ModelProfile[] }>('/api/models'),
  saveModel: (body: Record<string, unknown>) => request<ModelProfile>('/api/models', json(body)),
  deleteModel: (id: string) => request<{ ok: boolean }>(`/api/models/${id}`, { method: 'DELETE' }),
  probeModel: (id: string) => request<Record<string, unknown>>(`/api/models/${id}/probe`, { method: 'POST' }),

  settings: () => request<Settings>('/api/settings'),
  updateSettings: (patch: Partial<Settings>) => request<Settings>('/api/settings', { method: 'PATCH', body: JSON.stringify(patch) }),

  tuning: () =>
    request<{ proposals: TuningProposal[]; suites: EvalSuite[]; failed_runs: Run[] }>('/api/tuning'),
  analyzeRun: (runId: string) => request<TuningProposal>('/api/tuning/analyze', json({ run_id: runId })),
  decideProposal: (id: string, action: 'accept' | 'reject' | 'rollback', comment = '') =>
    request<TuningProposal>(`/api/tuning/${id}/decision`, json({ action, comment })),
  evalSuites: () => request<{ suites: EvalSuite[] }>('/api/eval/suites'),
  runEval: (suiteId: string, targetVersion: string) =>
    request<EvalRun>(`/api/eval/suites/${suiteId}/run`, json({ target_version: targetVersion })),
  evalRun: (id: string) => request<EvalRun>(`/api/eval/runs/${id}`),
}

export function eventURL(stream: string, opts: { live?: boolean; after?: number } = {}) {
  const q = new URLSearchParams({ stream })
  if (opts.live) q.set('live', '1')
  if (opts.after) q.set('after', String(opts.after))
  if (token) q.set('token', token)
  return `/api/events?${q.toString()}`
}
