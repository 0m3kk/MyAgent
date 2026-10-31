import { useEffect, useMemo, useState } from 'react'
import clsx from 'clsx'
import {
  Activity,
  Banknote,
  ChevronRight,
  CircleDot,
  FileJson,
  ListTree,
  ScrollText,
  TerminalSquare,
  Timer,
} from 'lucide-react'
import type { EventEnvelope, Run, StepSpec, WorkflowSpec } from '../types'
import { formatCost, formatDate, formatDuration, safeJSON, shortId, titleCase } from '../format'
import { Badge, CopyButton, StatusBadge, Tabs } from './ui'
import { ResultBlock } from './result-view'

type Tab = 'overview' | 'input' | 'output' | 'logs' | 'trace' | 'cost'

export function RunInspector({
  spec,
  run,
  events,
  stepOutputs,
  outputs,
  selectedStep,
}: {
  spec?: WorkflowSpec
  run: Run
  events: EventEnvelope[]
  stepOutputs: Record<string, unknown>
  outputs: Record<string, unknown>
  selectedStep?: string
}) {
  const [tab, setTab] = useState<Tab>(selectedStep ? 'input' : 'overview')
  useEffect(() => {
    setTab(selectedStep ? 'input' : 'overview')
  }, [selectedStep])
  const step = spec?.steps?.find((s) => s.id === selectedStep)
  const stepEvents = useMemo(() => {
    if (!selectedStep) return []
    return events.filter((e) => e.step_id === selectedStep || e.step_id?.startsWith(`${selectedStep}.`) || e.step_id?.startsWith(`${selectedStep}[`))
  }, [events, selectedStep])
  const status = selectedStep ? latestStepStatus(run, selectedStep) : undefined

  const tabs: { id: Tab; label: React.ReactNode }[] = selectedStep
    ? [
        { id: 'input', label: 'Input' },
        { id: 'output', label: 'Output' },
        { id: 'logs', label: <><TerminalSquare size={12} /> Logs</> },
        { id: 'trace', label: 'Trace' },
        { id: 'cost', label: 'Cost' },
      ]
    : [
        { id: 'overview', label: 'Overview' },
        { id: 'logs', label: <><TerminalSquare size={12} /> Logs</> },
        { id: 'trace', label: 'Trace' },
        { id: 'cost', label: 'Cost' },
      ]

  return (
    <aside className="run-inspector">
      <div className="inspector-head">
        <div>
          <div className="eyebrow">{selectedStep ? 'Step inspector' : 'Run inspector'}</div>
          <h2>{selectedStep || run.target_name || run.target_kind}</h2>
        </div>
        {selectedStep && <StatusBadge status={status} />}
        {!selectedStep && <StatusBadge status={run.status} />}
      </div>
      <Tabs tabs={tabs} value={tab} onChange={setTab} className="inspector-tabs" />
      <div className="inspector-body">
        {tab === 'overview' && <RunOverview run={run} outputs={outputs} events={events} />}
        {tab === 'input' && (
          <StepInput step={step} stepEvents={stepEvents} run={run} />
        )}
        {tab === 'output' && (
          <div className="inspector-section">
            <InspectorLabel icon={<FileJson size={13} />} title="Output" />
            {selectedStep && stepOutputs[selectedStep] !== undefined ? (
              <ResultBlock value={stepOutputs[selectedStep]} rawMax={20000} />
            ) : selectedStep ? (
              <div className="muted-block">No output yet for this step.</div>
            ) : (
              <ResultBlock value={outputs} rawMax={30000} />
            )}
          </div>
        )}
        {tab === 'logs' && <StepLogs events={selectedStep ? stepEvents : events} />}
        {tab === 'trace' && <Trace events={selectedStep ? stepEvents : events} />}
        {tab === 'cost' && <Cost events={selectedStep ? stepEvents : events} run={run} />}
      </div>
    </aside>
  )
}

function latestStepStatus(run: Run, id: string): string | undefined {
  let status: string | undefined
  let attempt = 0
  for (const sr of run.steps || []) {
    if (sr.step_id.split(/[.[]/)[0] !== id && sr.step_id !== id) continue
    if (sr.attempt >= attempt) {
      attempt = sr.attempt
      status = sr.status
    }
  }
  if (!status && run.status === 'waiting_approval') return 'waiting_approval'
  return status || 'pending'
}

function RunOverview({ run, outputs, events }: { run: Run; outputs: Record<string, unknown>; events: EventEnvelope[] }) {
  return (
    <div className="overview">
      <div className="overview-grid">
        <Metric label="Status" value={<StatusBadge status={run.status} />} />
        <Metric label="Source" value={<span className="capitalize">{run.source}</span>} />
        <Metric label="Started" value={formatDate(run.started_at || run.created_at)} />
        <Metric label="Duration" value={formatDuration(run.started_at, run.finished_at)} />
        <Metric label="Tokens" value={`${(run.tokens_in || 0).toLocaleString()} in · ${(run.tokens_out || 0).toLocaleString()} out`} />
        <Metric label="Cost" value={formatCost(run.cost_micro)} />
      </div>
      {run.error && (
        <div className="run-error">
          <div className="run-error-head">Error</div>
          <pre>{run.error}</pre>
        </div>
      )}
      <InspectorLabel icon={<CircleDot size={13} />} title="Inputs" />
      <pre className="json-block">{safeJSON(run.inputs, 8000)}</pre>
      <InspectorLabel icon={<FileJson size={13} />} title="Outputs" />
      <ResultBlock value={outputs} rawMax={20000} />
      <div className="overview-foot">
        <span className="mono tiny">run {run.id}</span>
        <CopyButton text={run.id} />
      </div>
      <div className="overview-foot">
        <span className="muted tiny">{events.length} events</span>
      </div>
    </div>
  )
}

function Metric({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="metric">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  )
}

function StepInput({ step, stepEvents, run }: { step?: StepSpec; stepEvents: EventEnvelope[]; run: Run }) {
  const started = [...stepEvents].reverse().find((e) => e.type === 'tool.call.started')
  const llm = [...stepEvents].reverse().find((e) => e.type === 'llm.request')
  const approval = [...stepEvents].reverse().find((e) => e.type === 'approval.requested')
  return (
    <div className="inspector-section">
      {step ? (
        <>
          <InspectorLabel icon={<ListTree size={13} />} title="Configuration" />
          <pre className="json-block">{safeJSON(step, 12000)}</pre>
        </>
      ) : (
        <div className="muted-block">Select a step on the canvas to inspect it.</div>
      )}
      {started && (
        <>
          <InspectorLabel icon={<Activity size={13} />} title="Tool arguments" />
          <pre className="json-block">{safeJSON(started.payload?.args, 12000)}</pre>
        </>
      )}
      {llm && (
        <>
          <InspectorLabel icon={<ScrollText size={13} />} title="Prompt" />
          <pre className="json-block">{safeJSON(llm.payload?.prompt, 16000)}</pre>
        </>
      )}
      {approval && (
        <>
          <InspectorLabel icon={<ScrollText size={13} />} title="Approval" />
          <pre className="json-block">{safeJSON(approval.payload, 12000)}</pre>
        </>
      )}
    </div>
  )
}

function StepLogs({ events }: { events: EventEnvelope[] }) {
  const lines = events
    .filter((e) => e.type === 'step.log' || e.type === 'tool.call.log')
    .map((e) => `${new Date(e.ts).toLocaleTimeString()}  ${String(e.payload?.stream || 'log')}  ${String(e.payload?.text || '')}`)
  return (
    <div className="inspector-section">
      <InspectorLabel icon={<TerminalSquare size={13} />} title="Logs" />
      {lines.length ? <pre className="terminal-block">{lines.join('\n')}</pre> : <div className="muted-block">No logs for this step.</div>}
    </div>
  )
}

function Trace({ events }: { events: EventEnvelope[] }) {
  const filtered = events.filter((e) => !['llm.delta', 'step.output.delta'].includes(e.type))
  return (
    <div className="inspector-section">
      <InspectorLabel icon={<ListTree size={13} />} title={`Trace · ${filtered.length} events`} />
      <div className="trace-list">
        {filtered.slice(-200).reverse().map((e, i) => (
          <details key={`${e.id}-${i}`} className={clsx('trace-row', `trace-${e.type.split('.')[1] || 'event'}`)}>
            <summary>
              <span className="trace-time">{new Date(e.ts).toLocaleTimeString()}</span>
              <span className="trace-type">{e.type}</span>
              {e.step_id && <span className="trace-step">{e.step_id}</span>}
              <ChevronRight size={12} className="trace-chevron" />
            </summary>
            <pre className="json-block small">{safeJSON(e.payload, 6000)}</pre>
          </details>
        ))}
        {filtered.length === 0 && <div className="muted-block">No events yet.</div>}
      </div>
    </div>
  )
}

function Cost({ events, run }: { events: EventEnvelope[]; run: Run }) {
  const rows = events.filter((e) => e.type === 'llm.finished')
  let total = 0
  let input = 0
  let output = 0
  for (const e of rows) {
    const cost = Number(e.payload?.cost_micro || 0)
    total += cost
    const usage = e.payload?.usage as { in?: number; out?: number } | undefined
    input += Number(usage?.in || 0)
    output += Number(usage?.out || 0)
  }
  const showTotal = rows.length === 0
  return (
    <div className="inspector-section">
      <InspectorLabel icon={<Banknote size={13} />} title="LLM cost" />
      <div className="cost-grid">
        <Metric label="Cost" value={formatCost(showTotal ? run.cost_micro : total)} />
        <Metric label="Input" value={`${(showTotal ? run.tokens_in : input).toLocaleString()} tok`} />
        <Metric label="Output" value={`${(showTotal ? run.tokens_out : output).toLocaleString()} tok`} />
        <Metric label="Calls" value={rows.length || '—'} />
      </div>
      {rows.length > 0 && (
        <div className="cost-rows">
          {rows.map((e, i) => {
            const usage = e.payload?.usage as { in?: number; out?: number } | undefined
            return (
              <div key={i} className="cost-row">
                <span className="mono">{e.step_id || 'llm'}</span>
                <span>{String(e.payload?.model || 'model')}</span>
                <span>{Number(usage?.in || 0)} / {Number(usage?.out || 0)}</span>
                <span>{formatCost(Number(e.payload?.cost_micro || 0))}</span>
              </div>
            )
          })}
        </div>
      )}
      <div className="budget-note">
        <Timer size={13} />
        <span>Budget is enforced by the engine per run; a run stops before the next step if it exceeds the workflow budget.</span>
      </div>
    </div>
  )
}

function InspectorLabel({ icon, title }: { icon: React.ReactNode; title: string }) {
  return <div className="inspector-label">{icon} <span>{title}</span></div>
}

export function StepStatusList({ run }: { run: Run }) {
  return (
    <div className="step-status-list">
      {(run.steps || []).map((s) => (
        <div key={`${s.id}-${s.attempt}`} className="step-status-row">
          <StatusBadge status={s.status} />
          <span className="mono">{s.step_id}</span>
          <span className="muted tiny">{shortId(s.id)}</span>
          <span>{formatDuration(s.started_at, s.finished_at)}</span>
        </div>
      ))}
    </div>
  )
}
