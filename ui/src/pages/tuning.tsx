import { useCallback, useEffect, useMemo, useState } from 'react'
import clsx from 'clsx'
import {
  AlertTriangle,
  ArrowRight,
  BarChart3,
  Check,
  ClipboardList,
  FlaskConical,
  GitCompareArrows,
  Play,
  RotateCcw,
  Sparkles,
  ThumbsDown,
  Trash2,
  Wrench,
  X,
} from 'lucide-react'
import { api } from '../api'
import { navigate } from '../router'
import { useApp } from '../store'
import { useToast } from '../components/toast'
import { useConfirm } from '../components/confirm'
import type { EvalRun, EvalSuite, Run, TuningProposal, WorkflowDetail } from '../types'
import { formatCost, formatDate, formatRelative, safeJSON, shortId, statusTone } from '../format'
import { Badge, Button, EmptyState, Field, InlineNotice, Modal, PageHeader, SectionTitle, Select, Spinner, StatusBadge, Tabs } from '../components/ui'

type Tab = 'proposals' | 'evals' | 'signals'

export function TuningPage() {
  const { data, refresh } = useApp()
  const toast = useToast()
  const confirm = useConfirm()
  const [tab, setTab] = useState<Tab>('proposals')
  const [proposals, setProposals] = useState<TuningProposal[]>([])
  const [suites, setSuites] = useState<EvalSuite[]>([])
  const [failed, setFailed] = useState<Run[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState<string | null>(null)
  const [evalResult, setEvalResult] = useState<EvalRun | null>(null)
  const [evalOpen, setEvalOpen] = useState(false)
  const [runEvalSuite, setRunEvalSuite] = useState<EvalSuite | null>(null)
  const [versions, setVersions] = useState<{ id: string; version: number; status: string }[]>([])
  const [targetVersion, setTargetVersion] = useState('')

  const load = useCallback(async () => {
    try {
      const res = await api.tuning()
      setProposals(res.proposals)
      setSuites(res.suites)
      setFailed(res.failed_runs)
    } catch (err) {
      toast('Could not load tuning data', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setLoading(false)
    }
  }, [toast])

  useEffect(() => { void load() }, [load])

  async function decide(p: TuningProposal, action: 'accept' | 'reject' | 'rollback') {
    if (action === 'accept') {
      const ok = await confirm({
        title: 'Apply tuning patch',
        message: 'Apply this patch as a new approved version?',
        confirmLabel: 'Apply patch',
        tone: 'primary',
      })
      if (!ok) return
    }
    setBusy(p.id + action)
    try {
      await api.decideProposal(p.id, action)
      toast(action === 'accept' ? 'Patch applied' : action === 'reject' ? 'Proposal rejected' : 'Rolled back', undefined, 'success')
      await load()
      void refresh()
    } catch (err) {
      toast('Could not update the proposal', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(null)
    }
  }

  async function analyze(run: Run) {
    setBusy('analyze-' + run.id)
    try {
      await api.analyzeRun(run.id)
      toast('Proposal created', 'Review the diagnosis and diff.', 'success')
      await load()
      setTab('proposals')
    } catch (err) {
      toast('Analysis failed', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(null)
    }
  }

  async function openEval(suite: EvalSuite) {
    setRunEvalSuite(suite)
    setTargetVersion('')
    try {
      const detail = await api.workflow(suite.target_id)
      setVersions(detail.versions.map((v) => ({ id: v.id, version: v.version, status: v.status })))
      setTargetVersion(detail.workflow.version?.id || detail.versions[0]?.id || '')
    } catch {
      toast('Could not load the workflow for this suite', undefined, 'error')
    }
  }

  async function runEval() {
    if (!runEvalSuite || !targetVersion) return
    setBusy('eval')
    try {
      const res = await api.runEval(runEvalSuite.id, targetVersion)
      setEvalResult(res)
      setEvalOpen(true)
      setRunEvalSuite(null)
      toast('Eval finished', `${res.results?.filter((r) => r.passed).length || 0}/${res.results?.length || 0} cases passed.`, 'success')
      await load()
    } catch (err) {
      toast('Eval failed', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(null)
    }
  }

  const workflowName = (id: string) => data?.workflows.find((w) => w.id === id)?.name || shortId(id)

  if (loading) return <div className="page center-page"><Spinner size={26} /></div>

  return (
    <div className="page">
      <PageHeader
        eyebrow="Improvement loop"
        title="Tuning"
        description="Diagnose failed runs, replay eval suites, and accept only patches that measurably improve the workflow."
        actions={<Button variant="subtle" icon={<RefreshIcon />} onClick={load}>Refresh</Button>}
      />
      <div className="tuning-stats">
        <div><strong>{proposals.filter((p) => p.status === 'proposed').length}</strong><span>proposals</span></div>
        <div><strong>{suites.length}</strong><span>eval suites</span></div>
        <div><strong>{failed.length}</strong><span>failed runs</span></div>
        <div><strong>{data?.counts?.runs || 0}</strong><span>total runs</span></div>
      </div>
      <Tabs
        tabs={[
          { id: 'proposals', label: 'Proposals', count: proposals.length },
          { id: 'evals', label: 'Eval suites', count: suites.length },
          { id: 'signals', label: 'Signals', count: failed.length },
        ]}
        value={tab}
        onChange={setTab}
      />

      {tab === 'proposals' && (
        <div className="proposal-list">
          {proposals.map((p) => (
            <ProposalCard key={p.id} proposal={p} workflow={workflowName(p.target_id)} busy={busy} onDecide={decide} />
          ))}
          {proposals.length === 0 && (
            <EmptyState icon={<Sparkles size={26} />} title="No proposals yet" description="Run a workflow, rate it 👎 or open a failed run, then ask the tuner to diagnose it." action={<Button variant="subtle" onClick={() => setTab('signals')}>View failed runs</Button>} />
          )}
        </div>
      )}

      {tab === 'evals' && (
        <div className="eval-grid">
          {suites.map((s) => (
            <div key={s.id} className="eval-card">
              <div className="eval-head">
                <span className="eval-icon"><FlaskConical size={17} /></span>
                <div><strong>{s.name}</strong><span>{workflowName(s.target_id)} · {s.cases?.length || 0} cases</span></div>
                <Badge tone="neutral">{s.cases?.filter((c) => c.split === 'holdout').length || 0} holdout</Badge>
              </div>
              <div className="eval-cases">
                {(s.cases || []).slice(0, 4).map((c) => (
                  <button key={c.id} className="eval-case" onClick={() => navigate(`/runs/${c.source_run_id}`)}>
                    <span className="mono tiny">{shortId(c.source_run_id)}</span>
                    <span>{c.assertions?.length ? `${c.assertions.length} assertions` : 'no assertions'}</span>
                    <Badge tone={c.split === 'holdout' ? 'warn' : 'neutral'}>{c.split}</Badge>
                  </button>
                ))}
                {(!s.cases || s.cases.length === 0) && <div className="muted small">No cases. Open a run and click “Save as eval case”.</div>}
              </div>
              <div className="eval-foot">
                <span>{formatRelative(s.created_at)}</span>
                <Button size="sm" variant="primary" icon={<Play size={13} />} onClick={() => openEval(s)} disabled={!s.cases?.length}>Run eval</Button>
              </div>
            </div>
          ))}
          {suites.length === 0 && (
            <EmptyState icon={<ClipboardList size={26} />} title="No eval suites" description="Open a successful run and click “Save as eval case”. The suite is created automatically." action={<Button variant="subtle" onClick={() => navigate('/runs')}>Open runs</Button>} />
          )}
        </div>
      )}

      {tab === 'signals' && (
        <div className="signals-list">
          <div className="signals-head">
            <div><h2>Failed runs</h2><p>The tuner reads the trace, classifies the root cause and proposes one small patch.</p></div>
          </div>
          {failed.map((r) => (
            <div key={r.id} className="signal-row">
              <span className="signal-icon"><AlertTriangle size={16} /></span>
              <div className="signal-main">
                <strong>{r.target_name || r.target_kind}</strong>
                <span className="muted tiny">{r.error || 'run failed'}</span>
              </div>
              <span className="muted tiny">{formatRelative(r.created_at)}</span>
              <Button size="sm" variant="subtle" icon={<Sparkles size={13} />} onClick={() => analyze(r)} loading={busy === 'analyze-' + r.id} disabled={r.target_kind !== 'workflow'}>Analyze</Button>
              <Button size="sm" variant="ghost" onClick={() => navigate(`/runs/${r.id}`)}>Open</Button>
            </div>
          ))}
          {failed.length === 0 && <EmptyState compact icon={<BarChart3 size={24} />} title="No failed runs" description="The agent is having a good week." />}
        </div>
      )}

      <Modal open={!!runEvalSuite} onClose={() => setRunEvalSuite(null)} title="Run eval suite" subtitle={runEvalSuite?.name} width={520}
        footer={<><Button variant="ghost" onClick={() => setRunEvalSuite(null)}>Cancel</Button><Button variant="primary" onClick={runEval} loading={busy === 'eval'} disabled={!targetVersion}>Run replay</Button></>}>
        <Field label="Target version" hint="Tool side effects are replayed from recorded outputs; LLM steps run live.">
          <Select value={targetVersion} onChange={(e) => setTargetVersion(e.target.value)}>
            {versions.map((v) => <option key={v.id} value={v.id}>v{v.version} ({v.status})</option>)}
          </Select>
        </Field>
        <InlineNotice tone="info" title="Deterministic tools">The current eval cases pin the original run inputs and recorded tool outputs.</InlineNotice>
      </Modal>

      <Modal open={evalOpen} onClose={() => setEvalOpen(false)} title="Eval result" subtitle={evalResult ? `${evalResult.results?.filter((r) => r.passed).length}/${evalResult.results?.length} passed` : ''} width={760}
        footer={<Button variant="ghost" onClick={() => setEvalOpen(false)}>Close</Button>}>
        {evalResult && (
          <div className="eval-results">
            {(evalResult.results || []).map((r) => (
              <div key={r.id} className={clsx('eval-result-row', r.passed ? 'pass' : 'fail')}>
                <span>{r.passed ? <Check size={15} /> : <X size={15} />}</span>
                <span className="mono tiny">{shortId(r.case_id)}</span>
                <span>{formatCost(r.cost_micro)}</span>
                <span>{r.latency_ms}ms</span>
                <span className="muted tiny">{r.error || (r.passed ? 'passed' : 'failed')}</span>
              </div>
            ))}
            {(!evalResult.results || evalResult.results.length === 0) && <div className="muted">No case results.</div>}
          </div>
        )}
      </Modal>
    </div>
  )
}

function ProposalCard({ proposal, workflow, busy, onDecide }: { proposal: TuningProposal; workflow: string; busy: string | null; onDecide: (p: TuningProposal, a: 'accept' | 'reject' | 'rollback') => void }) {
  const [showYaml, setShowYaml] = useState(false)
  const status = proposal.status
  return (
    <div className={`proposal-card proposal-${status}`}>
      <div className="proposal-head">
        <span className={clsx('proposal-icon', status === 'proposed' && 'pulse')}><Sparkles size={17} /></span>
        <div className="proposal-title">
          <div><strong>{workflow}</strong><Badge tone={statusTone(status)}>{status}</Badge></div>
          <span className="muted tiny">proposal {shortId(proposal.id)} · {formatRelative(proposal.created_at)}</span>
        </div>
        <div className="proposal-actions">
          {status === 'proposed' && (
            <>
              <Button size="sm" variant="ghost" icon={<X size={13} />} onClick={() => onDecide(proposal, 'reject')} loading={busy === proposal.id + 'reject'}>Reject</Button>
              <Button size="sm" variant="primary" icon={<Check size={13} />} onClick={() => onDecide(proposal, 'accept')} loading={busy === proposal.id + 'accept'}>Accept</Button>
            </>
          )}
          {status === 'accepted' && <Button size="sm" variant="subtle" icon={<RotateCcw size={13} />} onClick={() => onDecide(proposal, 'rollback')} loading={busy === proposal.id + 'rollback'}>Rollback</Button>}
          {status === 'rejected' && <Badge tone="muted">rejected</Badge>}
          {status === 'rolled_back' && <Badge tone="muted">rolled back</Badge>}
        </div>
      </div>
      <div className="proposal-body">
        <div className="proposal-diagnosis">
          <span className="proposal-label">Diagnosis</span>
          <p>{proposal.diagnosis}</p>
        </div>
        <div className="proposal-diff">
          <div className="proposal-label">Patch</div>
          <pre className="diff-block">{proposal.patch || 'No diff recorded.'}</pre>
        </div>
        <div className="proposal-yaml">
          <button className="link-btn" onClick={() => setShowYaml((v) => !v)}>{showYaml ? 'Hide' : 'Show'} proposed YAML</button>
          {showYaml && <pre className="json-block small">{proposal.proposed_yaml || '—'}</pre>}
        </div>
      </div>
      <div className="proposal-foot">
        <span>Base version <span className="mono">{shortId(proposal.base_version_id)}</span></span>
        <span className="muted tiny">Eval results are stored when the proposal is tested.</span>
      </div>
    </div>
  )
}

function RefreshIcon() {
  return <RotateCcw size={15} />
}
