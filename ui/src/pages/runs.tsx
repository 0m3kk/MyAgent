import { useCallback, useEffect, useMemo, useState } from 'react'
import clsx from 'clsx'
import {
  Activity,
  ArrowLeft,
  ArrowRight,
  BarChart3,
  CheckCircle2,
  ShieldCheck,
  CirclePlay,
  GitCompareArrows,
  ListFilter,
  Pause,
  Play,
  RotateCcw,
  Save,
  SlidersHorizontal,
  Sparkles,
  Square,
  ThumbsDown,
  ThumbsUp,
  Trash2,
  X,
} from 'lucide-react'
import { api } from '../api'
import { navigate } from '../router'
import { useApp } from '../store'
import { useEventStream, usePolling } from '../hooks'
import { useToast } from '../components/toast'
import type { Approval, EventEnvelope, Feedback, Run, RunDetail, StepRun, WorkflowSpec } from '../types'
import { formatCost, formatDate, formatDuration, formatRelative, safeJSON, shortId, statusTone } from '../format'
import { Badge, Button, EmptyState, IconButton, InlineNotice, Modal, PageHeader, Select, Spinner, StatusBadge, Tabs, TextArea } from '../components/ui'
import { WorkflowCanvas } from '../components/workflow-canvas'
import { RunInspector, StepStatusList } from '../components/run-inspector'
import { Markdown, ToolCard, type ToolView } from '../components/chat-bits'

function mergeEvents(prev: EventEnvelope[], next: EventEnvelope[]): EventEnvelope[] {
  const byID = new Map<number, EventEnvelope>()
  const ephemeral: EventEnvelope[] = []
  for (const e of [...prev, ...next]) {
    if (e.id > 0) byID.set(e.id, e)
    else ephemeral.push(e)
  }
  return [...byID.values()].sort((a, b) => (a.id || 0) - (b.id || 0)).concat(ephemeral.slice(-300))
}

export function RunsPage() {
  const { data, refresh } = useApp()
  const toast = useToast()
  const [runs, setRuns] = useState<Run[]>([])
  const [loading, setLoading] = useState(true)
  const [status, setStatus] = useState('')
  const [source, setSource] = useState('')
  const [workflow, setWorkflow] = useState('')
  const [selected, setSelected] = useState<string[]>([])
  const [compareOpen, setCompareOpen] = useState(false)

  const load = useCallback(async () => {
    try {
      const res = await api.runs({ status, source, workflow, limit: 100 })
      setRuns(res.runs)
    } catch (err) {
      toast('Could not load runs', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setLoading(false)
    }
  }, [status, source, workflow, toast])

  useEffect(() => { void load() }, [load])
  usePolling(() => void load(), 5000, runs.some((r) => !['succeeded', 'failed', 'cancelled', 'expired'].includes(r.status)))

  function toggleSelect(id: string) {
    setSelected((prev) => prev.includes(id) ? prev.filter((x) => x !== id) : prev.length >= 2 ? [prev[1], id] : [...prev, id])
  }

  async function rate(run: Run, rating: 'up' | 'down') {
    try {
      await api.feedback(run.id, rating)
      toast(rating === 'up' ? 'Marked as helpful' : 'Feedback saved', 'This run is now a tuning signal.', 'success')
      void load()
    } catch (err) {
      toast('Could not save feedback', err instanceof Error ? err.message : undefined, 'error')
    }
  }

  return (
    <div className="page">
      <PageHeader
        eyebrow="Observability"
        title="Runs"
        description="Every workflow and tool run, with source, cost, trace and feedback."
        actions={
          <>
            {selected.length === 2 && (
              <Button variant="subtle" icon={<GitCompareArrows size={15} />} onClick={() => setCompareOpen(true)}>Compare</Button>
            )}
            <Button variant="subtle" icon={<Activity size={15} />} onClick={() => { void load(); void refresh() }}>Refresh</Button>
          </>
        }
      />
      <div className="filter-bar">
        <div className="filter-group">
          <ListFilter size={14} />
          <Select value={status} onChange={(e) => setStatus(e.target.value)}>
            <option value="">All statuses</option>
            {['running', 'waiting_approval', 'paused', 'succeeded', 'failed', 'cancelled'].map((s) => <option key={s} value={s}>{s.replace(/_/g, ' ')}</option>)}
          </Select>
          <Select value={source} onChange={(e) => setSource(e.target.value)}>
            <option value="">All sources</option>
            {['manual', 'chat', 'schedule', 'watcher', 'webhook', 'cli', 'replay'].map((s) => <option key={s} value={s}>{s}</option>)}
          </Select>
          <Select value={workflow} onChange={(e) => setWorkflow(e.target.value)}>
            <option value="">All workflows</option>
            {data?.workflows.map((w) => <option key={w.id} value={w.id}>{w.name}</option>)}
          </Select>
        </div>
        <span className="muted tiny">{runs.length} runs</span>
      </div>

      {loading ? (
        <div className="center-page"><Spinner size={24} /></div>
      ) : runs.length === 0 ? (
        <EmptyState icon={<Activity size={26} />} title="No runs match these filters" description="Run a workflow or tool, or clear the filters." />
      ) : (
        <div className="runs-table">
          <div className="runs-head">
            <span />
            <span>Status</span>
            <span>Target</span>
            <span>Source</span>
            <span>Started</span>
            <span>Duration</span>
            <span>Cost</span>
            <span>Signal</span>
            <span />
          </div>
          {runs.map((r) => (
            <div key={r.id} className={clsx('runs-row', selected.includes(r.id) && 'selected')} onClick={() => navigate(`/runs/${r.id}`)}>
              <span onClick={(e) => e.stopPropagation()}>
                <input type="checkbox" checked={selected.includes(r.id)} onChange={() => toggleSelect(r.id)} aria-label="Select run for comparison" />
              </span>
              <StatusBadge status={r.status} />
              <span className="run-target">
                <strong>{r.target_name || r.target_kind}</strong>
                <em className="mono tiny">{shortId(r.id)}</em>
              </span>
              <span className="capitalize muted">{r.source}</span>
              <span className="muted tiny">{formatRelative(r.created_at)}</span>
              <span className="muted tiny">{formatDuration(r.started_at, r.finished_at)}</span>
              <span className="mono tiny">{formatCost(r.cost_micro)}</span>
              <span className="feedback-cell" onClick={(e) => e.stopPropagation()}>
                <button className={clsx('feedback-btn', r.id && '')} onClick={() => rate(r, 'up')} aria-label="Thumbs up"><ThumbsUp size={13} /></button>
                <button className="feedback-btn" onClick={() => rate(r, 'down')} aria-label="Thumbs down"><ThumbsDown size={13} /></button>
              </span>
              <span className="runs-row-arrow"><ArrowRight size={14} /></span>
            </div>
          ))}
        </div>
      )}

      <CompareModal open={compareOpen} onClose={() => setCompareOpen(false)} ids={selected} />
    </div>
  )
}

function CompareModal({ open, onClose, ids }: { open: boolean; onClose: () => void; ids: string[] }) {
  const [a, setA] = useState<RunDetail | null>(null)
  const [b, setB] = useState<RunDetail | null>(null)
  const [loading, setLoading] = useState(false)
  useEffect(() => {
    if (!open || ids.length !== 2) return
    setLoading(true)
    Promise.all([api.run(ids[0]), api.run(ids[1])])
      .then(([x, y]) => { setA(x); setB(y) })
      .finally(() => setLoading(false))
  }, [open, ids])
  return (
    <Modal open={open} onClose={onClose} title="Compare runs" subtitle="Status, cost, latency and step outcomes." width={920}
      footer={<Button variant="ghost" onClick={onClose}>Close</Button>}>
      {loading || !a || !b ? <div className="center-page"><Spinner /></div> : (
        <div className="compare-grid">
          {[a, b].map((d) => (
            <div key={d.run.id} className="compare-col">
              <div className="compare-head">
                <StatusBadge status={d.run.status} />
                <strong>{d.run.target_name}</strong>
                <span className="mono tiny">{shortId(d.run.id)}</span>
              </div>
              <div className="compare-metrics">
                <span>Source <strong>{d.run.source}</strong></span>
                <span>Cost <strong>{formatCost(d.run.cost_micro)}</strong></span>
                <span>Tokens <strong>{d.run.tokens_in + d.run.tokens_out}</strong></span>
                <span>Duration <strong>{formatDuration(d.run.started_at, d.run.finished_at)}</strong></span>
              </div>
              {d.run.error && <pre className="error-block small">{d.run.error}</pre>}
              <StepStatusList run={d.run} />
            </div>
          ))}
        </div>
      )}
    </Modal>
  )
}

export function RunPage({ runId }: { runId: string }) {
  const { approvals, refresh, refreshApprovals } = useApp()
  const toast = useToast()
  const [detail, setDetail] = useState<RunDetail | null>(null)
  const [events, setEvents] = useState<EventEnvelope[]>([])
  const [selectedStep, setSelectedStep] = useState<string | undefined>()
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState<string | null>(null)
  const [feedback, setFeedback] = useState<Feedback | undefined>()
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async (silent = false) => {
    if (!silent) setLoading(true)
    try {
      const d = await api.run(runId)
      setDetail(d)
      setEvents((prev) => mergeEvents(prev, d.events || []))
      setFeedback(d.feedback)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not load the run')
    } finally {
      setLoading(false)
    }
  }, [runId])

  useEffect(() => {
    setDetail(null)
    setEvents([])
    setFeedback(undefined)
    setSelectedStep(undefined)
    void load()
    void refreshApprovals()
  }, [runId, load, refreshApprovals])

  const onEvent = useCallback((event: EventEnvelope) => {
    setEvents((prev) => mergeEvents(prev, [event]))
    const p = event.payload as Record<string, unknown>
    if (event.type === 'run.finished') {
      void load(true)
      void refresh()
    }
    if (event.type === 'approval.requested' || event.type === 'approval.resolved') {
      void refreshApprovals()
      void load(true)
    }
    setDetail((prev) => {
      if (!prev) return prev
      const run = { ...prev.run }
      let stepOutputs = prev.step_outputs
      let outputs = prev.outputs
      if (event.type.startsWith('run.')) {
        const status = typeof p.status === 'string' ? p.status : undefined
        if (event.type === 'run.started') run.status = 'running'
        if (event.type === 'run.paused') run.status = 'paused'
        if (event.type === 'run.resumed') run.status = 'running'
        if (event.type === 'run.finished') {
          if (status) run.status = status
          run.finished_at = event.ts
          if (typeof p.error === 'string') run.error = p.error
          if (p.outputs && typeof p.outputs === 'object') outputs = p.outputs as Record<string, unknown>
        }
      }
      if (event.type === 'step.started' || event.type === 'step.finished' || event.type === 'step.skipped') {
        const stepId = String(event.step_id || p.step_id || '')
        const attempt = Number(p.attempt || 1)
        const status = event.type === 'step.started' ? 'running' : String(p.status || 'succeeded')
        const steps = [...(run.steps || [])]
        const idx = steps.findIndex((s) => s.step_id === stepId && s.attempt === attempt)
        const next: StepRun = { id: `${stepId}-${attempt}`, run_id: run.id, step_id: stepId, attempt, status,
          error: p.error ? String(p.error) : undefined, started_at: event.ts, finished_at: event.type === 'step.started' ? 0 : event.ts }
        if (idx >= 0) steps[idx] = { ...steps[idx], ...next }
        else steps.push(next)
        run.steps = steps
        if (event.type === 'step.finished' && p.output !== undefined) {
          stepOutputs = { ...stepOutputs, [stepId]: p.output }
        }
      }
      return { ...prev, run, step_outputs: stepOutputs, outputs }
    })
  }, [load, refresh, refreshApprovals])

  useEventStream(`run:${runId}`, onEvent, { enabled: !!detail })
  usePolling(() => void load(true), 4000, !!detail && !['succeeded', 'failed', 'cancelled', 'expired'].includes(detail.run.status))

  const pending = useMemo(() => approvals.filter((a) => a.run_id === runId && a.status === 'pending'), [approvals, runId])

  if (loading && !detail) return <div className="page center-page"><Spinner size={26} /></div>
  if (error || !detail) {
    return <div className="page"><EmptyState icon={<Activity size={26} />} title="Run not found" description={error || undefined} action={<Button onClick={() => navigate('/runs')}>Back to runs</Button>} /></div>
  }

  const run = detail.run
  const spec = detail.spec
  const running = !['succeeded', 'failed', 'cancelled', 'expired'].includes(run.status)
  const paused = run.status === 'paused'

  async function control(action: 'pause' | 'resume' | 'cancel') {
    setBusy(action)
    try {
      if (action === 'pause') await api.pauseRun(run.id)
      if (action === 'resume') await api.resumeRun(run.id)
      if (action === 'cancel') await api.cancelRun(run.id)
      await load(true)
    } catch (err) {
      toast('Could not update the run', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(null)
    }
  }

  async function rerun() {
    setBusy('rerun')
    try {
      const res = await api.rerun(run.id)
      toast('New run started', undefined, 'success')
      navigate(`/runs/${res.run.id}`)
    } catch (err) {
      toast('Could not rerun', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(null)
    }
  }

  async function rate(rating: 'up' | 'down') {
    try {
      const f = await api.feedback(run.id, rating)
      setFeedback(f)
      toast(rating === 'up' ? 'Marked helpful' : 'Feedback saved', undefined, 'success')
    } catch (err) {
      toast('Could not save feedback', err instanceof Error ? err.message : undefined, 'error')
    }
  }

  async function makeEvalCase() {
    setBusy('eval')
    try {
      await api.evalCase(run.id)
      toast('Eval case created', 'Pinned inputs and recorded tool outputs are now a test case.', 'success')
    } catch (err) {
      toast('Could not create an eval case', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(null)
    }
  }

  async function analyze() {
    setBusy('analyze')
    try {
      await api.analyzeRun(run.id)
      toast('Tuning proposal created', 'Review the diagnosis and diff in Tuning.', 'success')
      navigate('/tuning')
    } catch (err) {
      toast('Analysis failed', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="run-page">
      <header className="run-head">
        <div className="run-head-main">
          <button className="back-link" onClick={() => navigate('/runs')}><ArrowLeft size={14} /> Runs</button>
          <div className="run-title-row">
            <span className={clsx('run-orb', `st-${run.status}`)}><Activity size={17} /></span>
            <div>
              <h1>{run.target_name || run.target_kind}</h1>
              <div className="run-subline">
                <StatusBadge status={run.status} />
                <span className="capitalize">{run.source}</span>
                <span className="dot-sep" />
                <span className="mono tiny">{run.id}</span>
                <span className="dot-sep" />
                <span>{formatDate(run.created_at)}</span>
                <span className="dot-sep" />
                <span>{formatCost(run.cost_micro)}</span>
              </div>
            </div>
          </div>
        </div>
        <div className="run-actions">
          {run.status === 'running' && <Button variant="subtle" size="sm" icon={<Pause size={14} />} onClick={() => control('pause')} loading={busy === 'pause'}>Pause</Button>}
          {paused && <Button variant="subtle" size="sm" icon={<Play size={14} />} onClick={() => control('resume')} loading={busy === 'resume'}>Resume</Button>}
          {running && <Button variant="ghost" size="sm" icon={<Square size={13} />} onClick={() => control('cancel')} loading={busy === 'cancel'}>Cancel</Button>}
          <div className="feedback-actions">
            <IconButton label="Thumbs up" onClick={() => rate('up')} className={feedback?.rating === 'up' ? 'active' : ''}><ThumbsUp size={15} /></IconButton>
            <IconButton label="Thumbs down" onClick={() => rate('down')} className={feedback?.rating === 'down' ? 'active' : ''}><ThumbsDown size={15} /></IconButton>
          </div>
          <Button variant="subtle" size="sm" icon={<RotateCcw size={14} />} onClick={rerun} loading={busy === 'rerun'}>Rerun</Button>
          {run.target_kind === 'workflow' && (
            <Button variant="ghost" size="sm" icon={<Save size={14} />} onClick={makeEvalCase} loading={busy === 'eval'}>Eval case</Button>
          )}
          {run.target_kind === 'workflow' && <Button variant="ghost" size="sm" icon={<BarChart3 size={14} />} onClick={analyze} loading={busy === 'analyze'}>Analyze</Button>}
        </div>
      </header>

      {run.status === 'waiting_approval' && (
        <div className="run-approval-banner">
          <span className="run-approval-icon"><ShieldCheck size={16} /></span>
          <div>
            <strong>{pending[0]?.request?.title || pending[0]?.request?.description || 'Approval required'}</strong>
            <span>
              {pending[0]?.request?.step_id ? `Step ${String(pending[0].request.step_id)} · ` : ''}
              The run is paused. Review the content and decide; the engine continues from this step.
            </span>
          </div>
          <Button variant="primary" size="sm" onClick={() => window.dispatchEvent(new CustomEvent('myagent:open-approvals'))}>
            Review approval
          </Button>
        </div>
      )}

      {spec ? (
        <div className="run-workspace">
          <div className="run-canvas">
            <WorkflowCanvas spec={spec} run={run} approvals={pending} selectedStep={selectedStep} onSelectStep={setSelectedStep} />
          </div>
          <RunInspector spec={spec} run={run} events={events} stepOutputs={detail.step_outputs || {}} outputs={detail.outputs || {}} selectedStep={selectedStep} />
        </div>
      ) : (
        <div className="run-workspace">
          <ToolRunView run={run} events={events} outputs={detail.outputs || {}} stepOutputs={detail.step_outputs || {}} />
          <RunInspector run={run} events={events} stepOutputs={detail.step_outputs || {}} outputs={detail.outputs || {}} />
        </div>
      )}
    </div>
  )
}

function ToolRunView({
  run,
  events,
  outputs,
  stepOutputs,
}: {
  run: Run
  events: EventEnvelope[]
  outputs: Record<string, unknown>
  stepOutputs: Record<string, unknown>
}) {
  const lines = events
    .filter((e) => e.type === 'step.log')
    .map((e) => `${new Date(e.ts).toLocaleTimeString()}  ${String(e.payload?.stream || 'log')}  ${String(e.payload?.text || '')}`)
  const result = stepOutputs.tool ?? outputs.result
  const toolFinished = [...events].reverse().find((e) => e.type === 'tool.call.finished')
  const toolView: ToolView | null = toolFinished ? {
    key: run.id,
    name: run.target_name || 'tool',
    args: run.inputs,
    status: toolFinished.payload?.status === 'failed' ? 'failed' : 'succeeded',
    result,
    error: typeof toolFinished.payload?.error === 'string' ? toolFinished.payload.error : undefined,
  } : null
  return (
    <div className="tool-run-main">
      <div className="tool-run-head">
        <div><div className="eyebrow">Tool run</div><h2>{run.target_name}</h2></div>
      </div>
      {toolView && <ToolCard tool={toolView} />}
      <div className="tool-run-terminal">
        <div className="terminal-head"><span>Trace output</span><span className="muted tiny">{lines.length} log lines</span></div>
        <pre className="terminal-block">{lines.length ? lines.join('\n') : 'No subprocess logs. The tool result is shown above.'}</pre>
      </div>
      <div className="tool-run-json">
        <div className="terminal-head"><span>Result</span><span className="mono tiny">{shortId(run.id)}</span></div>
        <pre className="json-block">{safeJSON(result ?? {}, 30000)}</pre>
      </div>
      {run.error && <InlineNotice tone="bad" title="Run failed">{run.error}</InlineNotice>}
    </div>
  )
}
