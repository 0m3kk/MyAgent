import { useEffect, useMemo, useState, type ReactNode } from 'react'
import clsx from 'clsx'
import {
  AlertTriangle,
  Check,
  Cloud,
  Cpu,
  FolderOpen,
  KeyRound,
  Laptop,
  Lock,
  Monitor,
  Moon,
  Plus,
  RefreshCw,
  Save,
  Server,
  Settings2,
  ShieldAlert,
  Sparkles,
  Sun,
  Trash2,
  Wand2,
  X,
  Zap,
} from 'lucide-react'
import { api } from '../api'
import { navigate } from '../router'
import { useApp } from '../store'
import { useToast } from '../components/toast'
import { useConfirm } from '../components/confirm'
import type { ModelProfile, SecretMeta, Settings } from '../types'
import { formatDate, formatRelative, safeJSON, statusTone } from '../format'
import { Badge, Button, CopyButton, EmptyState, Field, InlineNotice, Modal, PageHeader, SectionTitle, Select, Spinner, StatusBadge, Tabs, TextArea, TextInput, Toggle } from '../components/ui'
import { FontSizeControl } from '../components/font-size'

type Tab = 'general' | 'models' | 'secrets'

const UNLIMITED_STEPS = -1

export function SettingsPage({ sub }: { sub?: string[] }) {
  const { data, refresh } = useApp()
  const toast = useToast()
  const confirm = useConfirm()
  const tab: Tab = sub?.[0] === 'models' ? 'models' : sub?.[0] === 'secrets' ? 'secrets' : 'general'
  const wantsNewModel = tab === 'models' && sub?.[1] === 'new'
  const wantsNewSecret = tab === 'secrets' && sub?.[1] === 'new'
  const [modelModal, setModelModal] = useState<ModelProfile | 'new' | null>(wantsNewModel ? 'new' : null)
  const [secretModal, setSecretModal] = useState<SecretMeta | 'new' | null>(wantsNewSecret ? 'new' : null)
  const [secrets, setSecrets] = useState<SecretMeta[]>([])
  const [probe, setProbe] = useState<Record<string, unknown> | null>(null)

  const loadSecrets = async () => {
    try {
      const res = await api.secrets()
      setSecrets(res.secrets)
    } catch {
      // The settings page remains usable if the secret list fails once.
    }
  }
  useEffect(() => { void loadSecrets() }, [])
  useEffect(() => { if (wantsNewModel) setModelModal('new') }, [wantsNewModel])
  useEffect(() => { if (wantsNewSecret) setSecretModal('new') }, [wantsNewSecret])

  if (!data) return null

  function closeModel() {
    setModelModal(null)
    if (wantsNewModel) navigate('/settings/models')
  }
  function closeSecret() {
    setSecretModal(null)
    if (wantsNewSecret) navigate('/settings/secrets')
  }

  return (
    <div className="page settings-page">
      <PageHeader
        eyebrow="Configuration"
        title="Settings"
        description="Models, secrets, budgets and the local security boundary."
        actions={<span className="muted tiny">MyAgent {data.version}</span>}
      />
      <Tabs
        tabs={[
          { id: 'general', label: <><Settings2 size={13} /> General</> },
          { id: 'models', label: <><Cpu size={13} /> Models {data.model_profiles.length ? <span className="tab-count">{data.model_profiles.length}</span> : null}</> },
          { id: 'secrets', label: <><KeyRound size={13} /> Secrets {secrets.length ? <span className="tab-count">{secrets.length}</span> : null}</> },
        ]}
        value={tab}
        onChange={(t) => navigate(`/settings/${t}`)}
      />

      {tab === 'general' && <GeneralTab onNeedModels={() => navigate('/settings/models')} />}
      {tab === 'models' && (
        <ModelsTab
          onAdd={() => setModelModal('new')}
          onEdit={(m) => setModelModal(m)}
          onProbe={async (m) => {
            try {
              const caps = await api.probeModel(m.id)
              setProbe(caps)
              toast(caps.tool_calling ? 'Probe passed' : 'Probe finished with limits', caps.tool_calling ? 'Tool calling and streaming detected.' : String(caps.error || 'Tool calling was not detected. The model can still be used as an executor.'), caps.tool_calling ? 'success' : 'info')
              void refresh()
            } catch (err) {
              toast('Probe failed', err instanceof Error ? err.message : undefined, 'error')
            }
          }}
        />
      )}
      {tab === 'secrets' && (
        <SecretsTab
          secrets={secrets}
          onAdd={() => setSecretModal('new')}
          onOverwrite={(s) => setSecretModal(s)}
          onDelete={async (s) => {
            const ok = await confirm({
              title: 'Delete secret',
              message: `Delete ${s.name}? Tools using it will fail immediately.`,
              confirmLabel: 'Delete secret',
            })
            if (!ok) return
            try {
              await api.deleteSecret(s.id)
              toast('Secret deleted', undefined, 'success')
              await loadSecrets()
              void refresh()
            } catch (err) {
              toast('Could not delete the secret', err instanceof Error ? err.message : undefined, 'error')
            }
          }}
          keyKind={data.key_kind}
          keyDescription={data.key_description}
        />
      )}

      <ModelModal
        open={modelModal !== null}
        model={modelModal === 'new' ? null : modelModal}
        onClose={closeModel}
        onSaved={async () => { closeModel(); await refresh(); toast('Model profile saved', undefined, 'success') }}
      />
      <SecretModal
        open={secretModal !== null}
        secret={secretModal === 'new' ? null : secretModal}
        onClose={closeSecret}
        onSaved={async () => { closeSecret(); await loadSecrets(); await refresh(); toast('Secret saved', 'The value is encrypted and never shown again.', 'success') }}
      />
      <Modal open={!!probe} onClose={() => setProbe(null)} title="Model capabilities" subtitle="Probe results are stored on the profile." width={560}
        footer={<Button variant="ghost" onClick={() => setProbe(null)}>Close</Button>}>
        <div className="probe-grid">
          {probe && Object.entries(probe).map(([k, v]) => (
            <div key={k} className="probe-row"><span>{k.replace(/_/g, ' ')}</span><strong>{typeof v === 'boolean' ? (v ? 'yes' : 'no') : String(v)}</strong></div>
          ))}
        </div>
      </Modal>
    </div>
  )
}

function GeneralTab({ onNeedModels }: { onNeedModels: () => void }) {
  const { data, refresh, theme, setTheme } = useApp()
  const toast = useToast()
  const [form, setForm] = useState<Settings>(data!.settings)
  const [busy, setBusy] = useState(false)
  const dirty = JSON.stringify(form) !== JSON.stringify(data!.settings)
  useEffect(() => setForm(data!.settings), [data!.settings])

  async function save() {
    setBusy(true)
    try {
      // Appearance (theme, font scale) persists immediately when changed.
      const { theme: _theme, font_scale: _fontScale, ...patch } = form
      await api.updateSettings(patch)
      toast('Settings saved', undefined, 'success')
      await refresh()
    } catch (err) {
      toast('Could not save settings', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="settings-grid">
      <div className="settings-main">
        <div className="card">
          <SectionTitle hint="The only directory tools can read or write. Symlinks are checked.">Workspace</SectionTitle>
          <div className="path-row">
            <FolderOpen size={15} />
            <code>{data!.workspace.root_path}</code>
            <CopyButton text={data!.workspace.root_path} label="Copy path" />
          </div>
          <div className="settings-inline-note">
            <strong>{data!.counts?.workflows || 0} workflows</strong> · {data!.counts?.tools || 0} tools · {data!.counts?.runs || 0} runs
          </div>
        </div>

        <div className="card">
          <SectionTitle hint="Applied to new runs. The agent loop is hard-capped in code.">Agent limits</SectionTitle>
          <div className="settings-form-grid">
            <Field label="Global concurrent runs" hint="Across all workflows and tools.">
              <TextInput type="number" min={1} max={16} value={form.global_concurrency} onChange={(e) => setForm({ ...form, global_concurrency: Number(e.target.value) })} />
            </Field>
            <Field
              label="Agent steps per turn"
              hint={form.agent_max_steps === UNLIMITED_STEPS
                ? 'Unlimited. The token budget or Stop still ends the run.'
                : 'Maximum LLM ↔ tool loop iterations. Use Unlimited to remove the cap.'}
            >
              <div className="inline-number-toggle">
                <TextInput
                  type="number"
                  min={1}
                  value={form.agent_max_steps === UNLIMITED_STEPS ? '' : form.agent_max_steps}
                  disabled={form.agent_max_steps === UNLIMITED_STEPS}
                  onChange={(e) => setForm({ ...form, agent_max_steps: Math.max(1, Number(e.target.value) || 1) })}
                />
                <label className="inline-check">
                  <input
                    type="checkbox"
                    checked={form.agent_max_steps === UNLIMITED_STEPS}
                    onChange={(e) => setForm({ ...form, agent_max_steps: e.target.checked ? UNLIMITED_STEPS : 12 })}
                  />
                  Unlimited
                </label>
              </div>
            </Field>
            <Field label="Chat context budget" hint="Maximum prompt + completion tokens for one model call. The agent stops when a call exceeds this.">
              <TextInput type="number" min={1000} step={1000} value={form.agent_budget_tokens} onChange={(e) => setForm({ ...form, agent_budget_tokens: Number(e.target.value) })} />
            </Field>
            <Field label="Run retention (days)" hint="Feedback and eval runs are kept longer.">
              <TextInput type="number" min={7} max={3650} value={form.retention_days} onChange={(e) => setForm({ ...form, retention_days: Number(e.target.value) })} />
            </Field>
          </div>
          <div className="settings-toggles">
            <Toggle checked={form.notifications} onChange={(v) => setForm({ ...form, notifications: v })} label="macOS notifications" description="Notify when a run needs approval or a scheduled run stops." />
            <div className="settings-appearance-row">
              <div className="toggle-copy">
                <span className="toggle-label">Appearance</span>
                <span className="toggle-desc">Light, dark, or follow the macOS setting.</span>
              </div>
              <div className="theme-switch" role="radiogroup" aria-label="Appearance">
                {([
                  { id: 'light', label: 'Light', icon: <Sun size={13} /> },
                  { id: 'dark', label: 'Dark', icon: <Moon size={13} /> },
                  { id: 'system', label: 'System', icon: <Monitor size={13} /> },
                ] as const).map((mode) => (
                  <button
                    key={mode.id}
                    type="button"
                    role="radio"
                    aria-checked={theme === mode.id}
                    className={`theme-option ${theme === mode.id ? 'active' : ''}`}
                    onClick={() => setTheme(mode.id)}
                  >
                    {mode.icon}{mode.label}
                  </button>
                ))}
              </div>
            </div>
            <div className="settings-appearance-row">
              <div className="toggle-copy">
                <span className="toggle-label">Text size</span>
                <span className="toggle-desc">Make every label, message and trace easier to read.</span>
              </div>
              <FontSizeControl bare />
            </div>
          </div>
          <div className="form-actions">
            <Button variant="primary" icon={<Save size={14} />} onClick={save} loading={busy} disabled={!dirty}>Save settings</Button>
            {dirty && <span className="muted tiny">unsaved changes</span>}
          </div>
        </div>

        <div className="card">
          <SectionTitle>Models and roles</SectionTitle>
          <p className="muted small">The default profile handles chat. Role-specific profiles are selected for workflow authoring, execution, judging and cheap summarization.</p>
          <div className="model-mini-list">
            {data!.model_profiles.map((m) => (
              <div key={m.id} className="model-mini">
                <span className="model-orb small" />
                <div><strong>{m.name}</strong><span>{m.model} · {m.role}</span></div>
                {m.id === data!.settings.default_model_id && <Badge tone="ok">default</Badge>}
              </div>
            ))}
          </div>
          <Button variant="subtle" size="sm" icon={<Cpu size={14} />} onClick={onNeedModels}>Manage models</Button>
        </div>
      </div>

      <aside className="settings-side">
        <div className={clsx('card', data!.key_kind === 'file' && 'card-warn')}>
          <SectionTitle>Master key</SectionTitle>
          <div className="key-state">
            <span className="key-icon"><Lock size={18} /></span>
            <div><strong>{data!.key_kind === 'keychain' ? 'macOS Keychain' : 'Development file'}</strong><span>{data!.key_description}</span></div>
          </div>
          {data!.key_kind === 'file' && (
            <InlineNotice tone="warn" title="Development key">
              The AES master key lives in a 0600 file. Release builds use the macOS Keychain. Back up the key or you lose every secret.
            </InlineNotice>
          )}
          <p className="muted tiny">Secrets are AES-256-GCM encrypted in SQLite. The database alone is useless without this key.</p>
        </div>
        <div className="card">
          <SectionTitle>Local boundary</SectionTitle>
          <ul className="check-list">
            <li><Check size={13} /> API only listens on loopback</li>
            <li><Check size={13} /> Session token or SameSite cookie</li>
            <li><Check size={13} /> Secrets redacted from logs and events</li>
            <li><Check size={13} /> Tool network is deny-by-default</li>
            <li><Check size={13} /> Approval tied to content hash</li>
          </ul>
        </div>
        <div className="card about-card">
          <Sparkles size={19} />
          <strong>MyAgent {data!.version}</strong>
          <span>Go core · SQLite · React</span>
          <span className="muted tiny">All data stays on this Mac.</span>
        </div>
      </aside>
    </div>
  )
}

function ModelsTab({ onAdd, onEdit, onProbe }: { onAdd: () => void; onEdit: (m: ModelProfile) => void; onProbe: (m: ModelProfile) => void }) {
  const { data, refresh } = useApp()
  const toast = useToast()
  const confirm = useConfirm()
  const [busy, setBusy] = useState<string | null>(null)
  if (!data) return null
  async function setDefault(m: ModelProfile) {
    setBusy('default-' + m.id)
    try {
      await api.updateSettings({ default_model_id: m.id })
      toast('Default model changed', m.name, 'success')
      await refresh()
    } catch (err) {
      toast('Could not change the default model', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(null)
    }
  }
  async function remove(m: ModelProfile) {
    const ok = await confirm({
      title: 'Delete model profile',
      message: `Delete ${m.name}?`,
      confirmLabel: 'Delete model',
    })
    if (!ok) return
    try {
      await api.deleteModel(m.id)
      toast('Model deleted', undefined, 'success')
      await refresh()
    } catch (err) {
      toast('Could not delete the model', err instanceof Error ? err.message : undefined, 'error')
    }
  }
  return (
    <div className="models-tab">
      <div className="models-top">
        <div><h2>Model profiles</h2><p>Any OpenAI-compatible endpoint works: OpenAI, OpenRouter, Ollama, LM Studio, vLLM, Groq, DeepSeek and others.</p></div>
        <Button variant="primary" icon={<Plus size={15} />} onClick={onAdd}>Add model</Button>
      </div>
      <div className="model-grid">
        {data.model_profiles.map((m) => {
          const caps = m.capabilities || {}
          return (
            <div key={m.id} className="profile-card">
              <div className="profile-card-head">
                <span className="model-orb" />
                <div><strong>{m.name}</strong><span>{m.base_url}</span></div>
                {m.id === data.settings.default_model_id && <Badge tone="ok">default</Badge>}
              </div>
              <div className="profile-card-body">
                <div className="model-field"><span>Model</span><code>{m.model}</code></div>
                <div className="model-field"><span>Role</span><Badge>{m.role}</Badge></div>
                <div className="model-caps">
                  {caps.streaming ? <Badge tone="ok">streaming</Badge> : <Badge tone="muted">no stream probe</Badge>}
                  {caps.tool_calling ? <Badge tone="ok">tool calls</Badge> : <Badge tone="warn">tools not verified</Badge>}
                  {caps.json_mode ? <Badge tone="neutral">JSON</Badge> : null}
                </div>
                {m.secret_id ? <div className="secret-bound"><KeyRound size={12} /> API key in the secret store</div> : <div className="muted tiny">No API key (local model or keyless endpoint)</div>}
              </div>
              <div className="profile-card-actions">
                <Button size="sm" variant="subtle" icon={<Zap size={13} />} onClick={() => onProbe(m)}>Probe</Button>
                {m.id !== data.settings.default_model_id && <Button size="sm" variant="ghost" onClick={() => setDefault(m)} loading={busy === 'default-' + m.id}>Make default</Button>}
                <div className="spacer" />
                <button className="link-btn" onClick={() => onEdit(m)}>Edit</button>
                <button className="link-btn danger" onClick={() => remove(m)}>Delete</button>
              </div>
            </div>
          )
        })}
        <button className="model-add-card" onClick={onAdd}>
          <Plus size={22} />
          <strong>Add an OpenAI-compatible model</strong>
          <span>Cloud or local. Probe it before assigning workflow authoring.</span>
        </button>
      </div>
    </div>
  )
}

function SecretsTab({
  secrets,
  onAdd,
  onOverwrite,
  onDelete,
  keyKind,
  keyDescription,
}: {
  secrets: SecretMeta[]
  onAdd: () => void
  onOverwrite: (s: SecretMeta) => void
  onDelete: (s: SecretMeta) => void
  keyKind: string
  keyDescription: string
}) {
  return (
    <div className="secrets-tab">
      <div className="models-top">
        <div><h2>Secrets</h2><p>Values are encrypted at rest, injected only at the tool boundary, and never sent to the model.</p></div>
        <Button variant="primary" icon={<Plus size={15} />} onClick={onAdd}>Add secret</Button>
      </div>
      {keyKind === 'file' && <InlineNotice tone="warn" title="Development master key"><span>{keyDescription}</span></InlineNotice>}
      {secrets.length === 0 ? (
        <EmptyState icon={<KeyRound size={26} />} title="No secrets stored" description="Add API keys and tokens here. Tools declare secret names; you bind them to values." action={<Button variant="primary" onClick={onAdd}>Add secret</Button>} />
      ) : (
        <div className="secrets-table">
          <div className="secrets-head"><span>Name</span><span>Kind</span><span>Status</span><span>Used by</span><span>Last used</span><span>Expires</span><span /></div>
          {secrets.map((s) => (
            <div key={s.id} className="secrets-row">
              <span className="secret-name"><KeyRound size={13} /><strong>{s.name}</strong>{s.ask_each_run && <Badge tone="warn">ask each run</Badge>}</span>
              <span className="muted">{s.kind}</span>
              <StatusBadge status={s.status} />
              <span className="muted tiny">{s.used_by?.length ? s.used_by.join(', ') : '—'}</span>
              <span className="muted tiny">{s.last_used_at ? formatRelative(s.last_used_at) : 'never'}</span>
              <span className="muted tiny">{s.expires_at ? formatDate(s.expires_at) : 'no expiry'}</span>
              <span className="secret-actions">
                <button className="link-btn" onClick={() => onOverwrite(s)}>Overwrite</button>
                <button className="link-btn danger" onClick={() => onDelete(s)}>Delete</button>
              </span>
            </div>
          ))}
        </div>
      )}
      <div className="secrets-note">
        <ShieldAlert size={15} />
        <span>Overwriting a secret affects every tool immediately. Deleting it blocks those tools. Losing the master key means losing every value.</span>
      </div>
    </div>
  )
}

interface ModelPreset {
  id: string
  label: string
  icon: ReactNode
  base_url: string
  model: string
  session_header?: string
}

const PRESETS: ModelPreset[] = [
  { id: 'openai', label: 'OpenAI', icon: <Cloud size={15} />, base_url: 'https://api.openai.com/v1', model: 'gpt-4.1' },
  { id: 'openrouter', label: 'OpenRouter', icon: <Cloud size={15} />, base_url: 'https://openrouter.ai/api/v1', model: 'openai/gpt-4.1' },
  { id: 'opencode', label: 'OpenCode Go', icon: <Zap size={15} />, base_url: 'https://opencode.ai/zen/go/v1', model: 'deepseek-v4.1-flash', session_header: 'x-opencode-session' },
  { id: 'ollama', label: 'Ollama (local)', icon: <Laptop size={15} />, base_url: 'http://127.0.0.1:11434/v1', model: 'llama3.2' },
  { id: 'lmstudio', label: 'LM Studio (local)', icon: <Laptop size={15} />, base_url: 'http://127.0.0.1:1234/v1', model: 'local-model' },
  { id: 'deepseek', label: 'DeepSeek', icon: <Server size={15} />, base_url: 'https://api.deepseek.com/v1', model: 'deepseek-chat' },
  { id: 'groq', label: 'Groq', icon: <Zap size={15} />, base_url: 'https://api.groq.com/openai/v1', model: 'llama-3.3-70b-versatile' },
]

function ModelModal({ open, model, onClose, onSaved }: { open: boolean; model: ModelProfile | null; onClose: () => void; onSaved: () => void }) {
  const toast = useToast()
  const [preset, setPreset] = useState('openai')
  const [name, setName] = useState('')
  const [baseURL, setBaseURL] = useState(PRESETS[0].base_url)
  const [modelName, setModelName] = useState(PRESETS[0].model)
  const [role, setRole] = useState('executor')
  const [apiKey, setAPIKey] = useState('')
  const [sessionHeader, setSessionHeader] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    setError(null)
    setAPIKey('')
    if (model) {
      setName(model.name)
      setBaseURL(model.base_url)
      setModelName(model.model)
      setRole(model.role)
      const header = (model.params || {}).session_header
      setSessionHeader(typeof header === 'string' ? header : '')
      setPreset('custom')
    } else {
      setName('')
      setBaseURL(PRESETS[0].base_url)
      setModelName(PRESETS[0].model)
      setRole('executor')
      setSessionHeader('')
      setPreset('openai')
    }
  }, [open, model])

  function applyPreset(id: string) {
    setPreset(id)
    const p = PRESETS.find((x) => x.id === id)
    if (p) {
      setBaseURL(p.base_url)
      setModelName(p.model)
      setSessionHeader(p.session_header || '')
      if (!name) setName(p.label)
    }
  }

  async function save() {
    setBusy(true)
    setError(null)
    try {
      const params: Record<string, unknown> = { ...(model?.params || {}) }
      if (sessionHeader.trim()) params.session_header = sessionHeader.trim()
      else delete params.session_header
      await api.saveModel({
        id: model?.id,
        name: name || modelName,
        base_url: baseURL,
        model: modelName,
        role,
        api_key: apiKey,
        params,
      })
      onSaved()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not save the model')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open={open} onClose={onClose} title={model ? 'Edit model profile' : 'Add a model'} subtitle="OpenAI Chat Completions compatible. API keys go straight into the encrypted secret store." width={700}
      footer={<><Button variant="ghost" onClick={onClose}>Cancel</Button><Button variant="primary" onClick={save} loading={busy} disabled={!baseURL || !modelName}>Save profile</Button></>}>
      {!model && (
        <div className="preset-grid">
          {PRESETS.map((p) => (
            <button key={p.id} className={clsx('preset', preset === p.id && 'active')} onClick={() => applyPreset(p.id)}>
              {p.icon}<span>{p.label}</span>
            </button>
          ))}
          <button className={clsx('preset', preset === 'custom' && 'active')} onClick={() => setPreset('custom')}><Wand2 size={15} /><span>Custom</span></button>
        </div>
      )}
      <div className="settings-form-grid">
        <Field label="Profile name"><TextInput value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. GPT-4.1" /></Field>
        <Field label="Role" hint="Planner authors workflows; executor runs steps."><Select value={role} onChange={(e) => setRole(e.target.value)}>{['planner', 'executor', 'judge', 'cheap'].map((r) => <option key={r} value={r}>{r}</option>)}</Select></Field>
        <Field label="Base URL" hint="Include /v1 when the provider expects it."><TextInput value={baseURL} onChange={(e) => setBaseURL(e.target.value)} className="mono" /></Field>
        <Field label="Model"><TextInput value={modelName} onChange={(e) => setModelName(e.target.value)} className="mono" /></Field>
        <Field label="API key" hint={model?.secret_id ? 'Leave blank to keep the current key. A new value overwrites the stored secret.' : 'Stored encrypted; never appears in prompts.'}><TextInput type="password" value={apiKey} onChange={(e) => setAPIKey(e.target.value)} placeholder={model?.secret_id ? '•••••••• (unchanged)' : 'sk-…'} autoComplete="off" /></Field>
        <Field label="Session header" hint="Optional. Use x-opencode-session for OpenCode Go; the app sends a stable per-conversation/run ID."><TextInput value={sessionHeader} onChange={(e) => setSessionHeader(e.target.value)} placeholder="x-opencode-session" className="mono" /></Field>
      </div>
      {error && <div className="form-error">{error}</div>}
      <InlineNotice tone="info" title="Probe after saving">Use Probe to verify streaming and tool calling before assigning this profile to workflow authoring.</InlineNotice>
    </Modal>
  )
}

function SecretModal({ open, secret, onClose, onSaved }: { open: boolean; secret: SecretMeta | null; onClose: () => void; onSaved: () => void }) {
  const [name, setName] = useState('')
  const [kind, setKind] = useState('api_key')
  const [description, setDescription] = useState('')
  const [value, setValue] = useState('')
  const [expires, setExpires] = useState('')
  const [askEach, setAskEach] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    setError(null)
    setValue('')
    setName(secret?.name || '')
    setKind(secret?.kind || 'api_key')
    setDescription(secret?.description || '')
    setAskEach(secret?.ask_each_run || false)
    setExpires(secret?.expires_at ? new Date(secret.expires_at).toISOString().slice(0, 16) : '')
  }, [open, secret])

  async function save() {
    setBusy(true)
    setError(null)
    try {
      await api.putSecret({
        name,
        kind,
        description,
        value,
        ask_each_run: askEach,
        expires_at: expires ? new Date(expires).getTime() : 0,
      })
      onSaved()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not save the secret')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open={open} onClose={onClose} title={secret ? `Overwrite ${secret.name}` : 'Add a secret'} subtitle="The value is encrypted with AES-256-GCM before it touches SQLite. You cannot read it back." width={560}
      footer={<><Button variant="ghost" onClick={onClose}>Cancel</Button><Button variant="primary" onClick={save} loading={busy} disabled={!name || !value}>Save secret</Button></>}>
      <div className="settings-form-grid">
        <Field label="Name" hint="UPPER_SNAKE_CASE; tools declare this logical name."><TextInput value={name} onChange={(e) => setName(e.target.value.toUpperCase().replace(/[^A-Z0-9_]/g, '_'))} placeholder="GITHUB_TOKEN" className="mono" disabled={!!secret} /></Field>
        <Field label="Kind"><Select value={kind} onChange={(e) => setKind(e.target.value)}><option value="api_key">API key</option><option value="token">Token</option><option value="password">Password</option><option value="other">Other</option></Select></Field>
        <Field label="Value" hint="Never sent to the model; injected at runtime."><TextInput type="password" value={value} onChange={(e) => setValue(e.target.value)} autoComplete="new-password" /></Field>
        <Field label="Description"><TextInput value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Read-only GitHub token" /></Field>
        <Field label="Expires at" hint="Optional. The UI warns before expiry."><TextInput type="datetime-local" value={expires} onChange={(e) => setExpires(e.target.value)} /></Field>
        <Toggle checked={askEach} onChange={setAskEach} label="Ask before every run" description="Sensitive secrets can require a fresh approval each time a tool uses them." />
      </div>
      {error && <div className="form-error">{error}</div>}
    </Modal>
  )
}
