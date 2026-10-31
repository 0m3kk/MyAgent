import { useEffect, useMemo, useState } from 'react'
import { Check, Clock3, KeyRound, ShieldAlert, ShieldCheck, Trash2, X } from 'lucide-react'
import { api } from '../api'
import type { Approval } from '../types'
import { formatRelative, safeJSON } from '../format'
import { useNow } from '../hooks'
import { Badge, Button, Drawer, EmptyState, TextArea } from './ui'
import { useToast } from './toast'
import { useApp } from '../store'

export function ApprovalCard({
  approval,
  onDecided,
  compact,
}: {
  approval: Approval
  onDecided?: (approval: Approval) => void
  compact?: boolean
}) {
  const req = approval.request || {}
  const toast = useToast()
  const { refreshApprovals, refresh } = useApp()
  const [comment, setComment] = useState('')
  const [busy, setBusy] = useState<'approve' | 'reject' | null>(null)
  const [error, setError] = useState<string | null>(null)
  const now = useNow(1000)
  const remaining = approval.expires_at ? approval.expires_at - now : 0
  const urgent = remaining > 0 && remaining < 5 * 60_000

  const kind = approval.kind
  const isQuestion = kind === 'question'
  const isTool = kind === 'tool'
  const isTrigger = kind === 'trigger'
  const isSecret = kind === 'secret'
  const isDelete = kind === 'delete'
  const options = useMemo(() => {
    const raw = req.options
    return Array.isArray(raw) ? raw.map(String) : []
  }, [req.options])

  const title = req.title || req.question || req.message || approval.request?.description || 'Approval required'
  const description = req.description || req.summary || req.message || ''
  const show = req.show

  async function decide(decision: 'approve' | 'reject') {
    setBusy(decision)
    setError(null)
    try {
      const updated = await api.decideApproval(approval.id, decision, comment)
      toast(decision === 'approve' ? (isQuestion ? 'Answer sent' : 'Approved') : 'Rejected', undefined, 'success')
      onDecided?.(updated)
      void refreshApprovals()
      void refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not submit the decision')
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className={`approval-card ${compact ? 'compact' : ''} ${urgent ? 'urgent' : ''}`}>
      <div className="approval-head">
        <div className="approval-icon">
          {isQuestion ? <Clock3 size={17} /> : isDelete ? <Trash2 size={17} /> : isTool || isTrigger || isSecret ? <ShieldAlert size={17} /> : <ShieldCheck size={17} />}
        </div>
        <div className="approval-title">
          <div className="approval-kicker">
            <Badge tone="warn">{kind}</Badge>
            {approval.run_id && <span className="muted tiny">run {approval.run_id.slice(0, 8)}</span>}
            {approval.expires_at && <span className={`muted tiny ${urgent ? 'text-warn' : ''}`}>expires {formatRelative(approval.expires_at)}</span>}
          </div>
          <h3>{title}</h3>
          {description && description !== title && <p>{description}</p>}
        </div>
      </div>

      <div className="approval-content">
        {isTool && (
          <>
            <div className="approval-meta">
              <span>Tool <strong>{String(req.tool || '—')}</strong></span>
              <span>Effect <Badge tone={req.side_effect === 'external' ? 'bad' : 'warn'}>{String(req.side_effect || 'write')}</Badge></span>
              {req.tool_version_id && <span className="mono tiny">{String(req.tool_version_id).slice(0, 8)}</span>}
            </div>
            {req.args !== undefined && (
              <details className="approval-details">
                <summary>Arguments</summary>
                <pre>{safeJSON(req.args, 6000)}</pre>
              </details>
            )}
            {req.secret_names && req.secret_names.length > 0 && (
              <div className="secret-list">
                {req.secret_names.map((name) => {
                  const value = String((req.secret_status as Record<string, string> | undefined)?.[String(name)] || 'missing')
                  return (
                    <span key={String(name)} className="secret-pill">
                      <KeyRound size={12} /> {String(name)} <Badge tone={value === 'present' ? 'ok' : 'bad'}>{value}</Badge>
                    </span>
                  )
                })}
              </div>
            )}
            {req.permissions && (
              <details className="approval-details">
                <summary>Permissions</summary>
                <pre>{safeJSON(req.permissions, 4000)}</pre>
              </details>
            )}
          </>
        )}
        {isTrigger && (
          <div className="approval-meta vertical">
            <span>Workflow <strong>{String(req.workflow || '—')}</strong></span>
            <span>Kind <strong>{String(req.trigger_kind || '')}</strong></span>
            <pre>{safeJSON(req.config, 3000)}</pre>
          </div>
        )}
        {isDelete && (
          <div className="approval-meta vertical">
            <span>Object <strong className="capitalize">{String(req.object || 'item')}</strong></span>
            <span>Name <strong>{String(req.workflow || req.tool || '—')}</strong></span>
            <span>Versions <strong>{String(req.versions ?? '—')}</strong></span>
            {req.reason !== undefined && <span>Reason <strong>{String(req.reason)}</strong></span>}
          </div>
        )}
        {!isTool && show !== undefined && show !== null && (
          <div className="approval-show">
            {typeof show === 'string' ? <pre>{show}</pre> : <pre>{safeJSON(show, 8000)}</pre>}
          </div>
        )}
        {isQuestion && (
          <div className="question-box">
            {options.length > 0 && (
              <div className="option-row">
                {options.map((o) => (
                  <button
                    key={o}
                    type="button"
                    className={`option-chip ${comment === o ? 'active' : ''}`}
                    onClick={() => setComment(o)}
                  >
                    {o}
                  </button>
                ))}
              </div>
            )}
            <TextArea
              rows={2}
              value={comment}
              onChange={(e) => setComment(e.target.value)}
              placeholder="Type your answer…"
              autoFocus
            />
          </div>
        )}
        {!isQuestion && (
          <TextArea
            rows={2}
            value={comment}
            onChange={(e) => setComment(e.target.value)}
            placeholder="Optional note for the trace…"
          />
        )}
        {error && <div className="form-error">{error}</div>}
      </div>

      <div className="approval-actions">
        {isQuestion ? (
          <>
            <Button variant="ghost" onClick={() => decide('reject')} loading={busy === 'reject'}>
              Skip
            </Button>
            <Button variant="primary" icon={<Check size={15} />} onClick={() => decide('approve')} loading={busy === 'approve'} disabled={!comment.trim()}>
              Send answer
            </Button>
          </>
        ) : (
          <>
            <Button variant="ghost" icon={<X size={15} />} onClick={() => decide('reject')} loading={busy === 'reject'}>
              {isDelete ? 'Cancel' : 'Deny'}
            </Button>
            <Button variant="primary" icon={isDelete ? <Trash2 size={15} /> : <Check size={15} />} onClick={() => decide('approve')} loading={busy === 'approve'}>
              {isDelete ? 'Delete' : 'Allow'}
            </Button>
          </>
        )}
      </div>
    </div>
  )
}

export function ApprovalCenter({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { approvals, refreshApprovals } = useApp()
  const [local, setLocal] = useState(approvals)
  useEffect(() => setLocal(approvals), [approvals])
  function decide(ap: Approval) {
    setLocal((prev) => prev.filter((x) => x.id !== ap.id))
    void refreshApprovals()
  }
  return (
    <Drawer
      open={open}
      onClose={onClose}
      title="Approvals"
      subtitle="Risky actions wait here. Nothing runs until you allow it."
      width={520}
    >
      {local.length === 0 ? (
        <EmptyState icon={<ShieldCheck size={26} />} title="No pending approvals" description="The agent is working safely." />
      ) : (
        <div className="approval-list">
          {local.map((ap) => (
            <ApprovalCard key={ap.id} approval={ap} onDecided={decide} />
          ))}
        </div>
      )}
    </Drawer>
  )
}
