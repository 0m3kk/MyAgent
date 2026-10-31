import { useState, type ReactNode } from 'react'
import clsx from 'clsx'
import {
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Circle,
  CircleAlert,
  Clock3,
  Loader2,
  Play,
  TerminalSquare,
  Wrench,
  XCircle,
} from 'lucide-react'
import { navigate } from '../router'
import { formatDuration, safeJSON, titleCase } from '../format'
import { Badge, CopyButton } from './ui'
import { ResultBlock } from './result-view'

export { Markdown } from './markdown'

export interface ToolView {
  key: string
  name: string
  args?: unknown
  status: string
  result?: unknown
  error?: string
  duration_ms?: number
  logs?: string[]
}

export function ToolCard({ tool, live }: { tool: ToolView; live?: boolean }) {
  const [open, setOpen] = useState(!!tool.error)
  const status = tool.status || (live ? 'running' : 'succeeded')
  const result = tool.result
  const runId = result && typeof result === 'object' && 'run_id' in (result as Record<string, unknown>)
    ? String((result as Record<string, unknown>).run_id)
    : undefined
  const name = tool.name.includes('.') ? tool.name : titleCase(tool.name)
  return (
    <div className={clsx('tool-card', `tool-${status}`, open && 'open', live && 'live')}>
      <button className="tool-head" onClick={() => setOpen((o) => !o)}>
        <span className="tool-status">
          {status === 'running' ? <Loader2 size={14} className="spin" /> : status === 'failed' ? <XCircle size={14} /> : <CheckCircle2 size={14} />}
        </span>
        <Wrench size={14} className="tool-wrench" />
        <span className="tool-name">{name}</span>
        <span className="tool-meta">
          {tool.duration_ms !== undefined && <span>{formatDuration(0, tool.duration_ms)}</span>}
          {status === 'running' ? <span className="pulse-label">running</span> : <Badge tone={status === 'failed' ? 'bad' : 'ok'}>{status}</Badge>}
        </span>
        {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
      </button>
      {open && (
        <div className="tool-body">
          {tool.args !== undefined && (
            <div className="tool-section">
              <div className="tool-section-head">Arguments <CopyButton text={safeJSON(tool.args, 10000)} /></div>
              <pre className="json-block">{safeJSON(tool.args, 10000)}</pre>
            </div>
          )}
          {tool.logs && tool.logs.length > 0 && (
            <div className="tool-section">
              <div className="tool-section-head"><TerminalSquare size={13} /> Logs</div>
              <pre className="terminal-block small">{tool.logs.join('\n')}</pre>
            </div>
          )}
          {tool.error ? (
            <div className="tool-section">
              <div className="tool-section-head error"><CircleAlert size={13} /> Error</div>
              <pre className="error-block">{tool.error}</pre>
            </div>
          ) : tool.result !== undefined ? (
            <div className="tool-section">
              <div className="tool-section-head">Result <CopyButton text={safeJSON(tool.result, 10000)} /></div>
              <ResultBlock value={result} rawMax={12000} />
            </div>
          ) : status === 'running' ? (
            <div className="tool-running"><Loader2 size={14} className="spin" /> Waiting for the tool…</div>
          ) : null}
          {runId && (
            <button className="run-link" onClick={() => navigate(`/runs/${runId}`)}>
              <Play size={13} /> Open run <span className="mono">{runId.slice(0, 8)}</span>
            </button>
          )}
        </div>
      )}
    </div>
  )
}

export function ToolPill({ children, icon }: { children: ReactNode; icon?: ReactNode }) {
  return <span className="tool-pill">{icon}{children}</span>
}

export function MessageMeta({ time, tokens, cost }: { time: number; tokens?: number; cost?: number }) {
  return (
    <div className="message-meta">
      <Clock3 size={11} />
      <span>{new Date(time).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })}</span>
      {!!tokens && <span>{tokens} tok</span>}
      {!!cost && <span>${(cost / 1_000_000).toFixed(4)}</span>}
    </div>
  )
}

export function TypingIndicator() {
  return (
    <span className="typing" aria-label="Agent is thinking">
      <span /><span /><span />
    </span>
  )
}

export function StreamCursor() {
  return <span className="stream-cursor" />
}

export function statusIcon(status: string) {
  switch (status) {
    case 'succeeded':
      return <CheckCircle2 size={14} className="ok-text" />
    case 'failed':
      return <XCircle size={14} className="bad-text" />
    case 'running':
      return <Loader2 size={14} className="spin" />
    default:
      return <Circle size={12} />
  }
}
