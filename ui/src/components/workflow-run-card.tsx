import { useCallback, useEffect, useState } from 'react'
import { CirclePlay, ShieldCheck, Square, Workflow as WorkflowIcon } from 'lucide-react'
import { api } from '../api'
import { navigate } from '../router'
import { useApp } from '../store'
import { useEventStream, usePolling } from '../hooks'
import { formatCost, formatDuration, safeJSON, shortId } from '../format'
import { RUN_TERMINAL, type EventEnvelope, type RunDetail } from '../types'
import { Badge, Button, Spinner, StatusBadge } from './ui'
import { WorkflowMiniGraph } from './workflow-canvas'
import { useToast } from './toast'

export function WorkflowRunCard({
  runId,
  name,
  version,
}: {
  runId: string
  name: string
  version?: number
}) {
  const { refresh, refreshApprovals } = useApp()
  const toast = useToast()
  const [detail, setDetail] = useState<RunDetail | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    try {
      const next = await api.run(runId)
      setDetail(next)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not load the run')
    }
  }, [runId])

  useEffect(() => {
    setDetail(null)
    setError('')
    void load()
  }, [runId, load])

  const active = !!detail && !RUN_TERMINAL.includes(detail.run.status)
  const onEvent = useCallback(
    (event: EventEnvelope) => {
      if (event.type === 'approval.requested' || event.type === 'approval.resolved') {
        void refreshApprovals()
        void load()
        return
      }
      if (event.type === 'run.finished') {
        void load()
        void refresh()
        return
      }
      if (
        event.type.startsWith('run.') ||
        event.type === 'step.started' ||
        event.type === 'step.finished' ||
        event.type === 'step.skipped'
      ) {
        void load()
      }
    },
    [load, refresh, refreshApprovals],
  )
  useEventStream(`run:${runId}`, onEvent, { live: true, enabled: active })
  usePolling(() => void load(), 2500, !detail || active)

  async function cancel() {
    setBusy(true)
    try {
      await api.cancelRun(runId)
      await load()
    } catch (err) {
      toast('Could not cancel the run', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(false)
    }
  }

  const run = detail?.run
  const status = run?.status || 'queued'
  const running = !!run && !RUN_TERMINAL.includes(run.status)
  const workflowName = detail?.workflow?.name || name
  const workflowVersion = detail?.version?.version ?? version
  const currentStep =
    run?.steps?.find((s) => s.status === 'running') ||
    run?.steps?.find((s) => s.status === 'waiting_approval')
  const inputCount = run ? Object.keys(run.inputs || {}).length : 0

  return (
    <div className={`workflow-run-card st-${status}`}>
      <div className="workflow-run-head">
        <span className="workflow-run-icon"><WorkflowIcon size={15} /></span>
        <div className="workflow-run-copy">
          <div className="workflow-run-kicker">
            <Badge tone="neutral">workflow</Badge>
            {workflowVersion !== undefined && <span>v{workflowVersion}</span>}
            <span className="mono">{shortId(runId)}</span>
          </div>
          <strong>{workflowName}</strong>
        </div>
        <StatusBadge status={status} />
      </div>

      <div className="workflow-run-body">
        {detail?.spec ? (
          <div className="workflow-run-graph">
            <WorkflowMiniGraph spec={detail.spec} run={run} />
            <span className="workflow-run-stepcount">
              {detail.spec.steps?.length || 0} steps
            </span>
          </div>
        ) : !detail && !error ? (
          <span className="workflow-run-loading"><Spinner size={13} /> Loading run…</span>
        ) : null}

        <div className="workflow-run-meta">
          {run?.started_at ? <span>{formatDuration(run.started_at, run.finished_at)}</span> : <span>queued</span>}
          {currentStep && <span>at <code>{currentStep.step_id}</code></span>}
          {run && run.cost_micro > 0 && <span>{formatCost(run.cost_micro)}</span>}
          {inputCount > 0 && (
            <details className="workflow-run-inputs">
              <summary>{inputCount} input{inputCount === 1 ? '' : 's'}</summary>
              <pre className="json-block small">{safeJSON(run?.inputs, 4000)}</pre>
            </details>
          )}
        </div>

        {status === 'waiting_approval' && (
          <div className="workflow-run-waiting">
            <ShieldCheck size={14} />
            <span>Waiting for your approval. Review the card below to continue.</span>
          </div>
        )}
        {(error || run?.error) && (
          <pre className="error-block small">{run?.error || error}</pre>
        )}

        <div className="workflow-run-actions">
          <button className="run-link" onClick={() => navigate(`/runs/${runId}`)}>
            <CirclePlay size={13} /> Open run
          </button>
          {running && (
            <Button variant="ghost" size="sm" icon={<Square size={12} />} onClick={cancel} loading={busy}>
              Cancel
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}
