import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  ArrowLeft,
  CalendarClock,
  Check,
  ChevronRight,
  CirclePlay,
  Clock3,
  Code2,
  FileCode2,
  Folder,
  FolderInput,
  GitBranch,
  History,
  Info,
  MoreHorizontal,
  Play,
  Plus,
  RefreshCw,
  Save,
  ShieldCheck,
  Sparkles,
  Star,
  Trash2,
  Webhook,
  Workflow as WorkflowIcon,
  X,
} from 'lucide-react'
import clsx from 'clsx'
import { api } from '../api'
import { navigate } from '../router'
import { useApp } from '../store'
import { useToast } from '../components/toast'
import { useConfirm } from '../components/confirm'
import type { Folder as FolderRecord, Run, StepSpec, Trigger, ValidationResult, Workflow, WorkflowDetail, WorkflowSpec, WorkflowVersion } from '../types'
import { formatDate, formatRelative, shortId, statusTone, versionLabel } from '../format'
import { Badge, Button, EmptyState, Field, IconButton, InlineNotice, Modal, PageHeader, SectionTitle, Select, Spinner, StatusBadge, Tabs, TextArea, TextInput, Toggle, CopyButton } from '../components/ui'
import { DiagnosticsList } from '../components/diagnostics'
import { InputForm, InputSpecList, schemaDefaults, validateInputs, type JSONSchema } from '../components/input-form'
import { WorkflowCanvas } from '../components/workflow-canvas'
import { Markdown } from '../components/chat-bits'
import {
  buildLibraryCounts,
  folderDescendantIDs,
  folderMap,
  folderPath,
  FolderDialog,
  LibraryNav,
  MoveToFolderDialog,
  topLevelFolders,
  type LibrarySelection,
} from '../components/library'

const STARTER_YAML = `schema: 1
name: my-workflow
description: Describe what this workflow does.

inputs:
  folder:
    type: string
    format: path
    default: "."
    description: Folder to work with

outputs:
  result: \${steps.work.result}

settings:
  unattended: ask

steps:
  - id: list
    tool: fs.list
    with: {path: "\${inputs.folder}", limit: 20}

  - id: work
    needs: [list]
    set:
      count: \${steps.list.output.count}
`

type Tab = 'graph' | 'yaml' | 'versions' | 'triggers'

function workflowNeedsAttention(workflow: Workflow) {
  const status = workflow.version?.status
  return !workflow.version || status === 'draft'
}

export function WorkflowsPage() {
  const { data, refresh } = useApp()
  const toast = useToast()
  const confirm = useConfirm()
  const [query, setQuery] = useState('')
  const [selection, setSelection] = useState<LibrarySelection>({ kind: 'all' })
  const [statusFilter, setStatusFilter] = useState('all')
  const [sort, setSort] = useState<'name' | 'newest' | 'updated'>('name')
  const [folderDialog, setFolderDialog] = useState<{ mode: 'create' | 'edit'; folder?: FolderRecord } | null>(null)
  const [moveWorkflow, setMoveWorkflow] = useState<Workflow | null>(null)
  const [busy, setBusy] = useState<string | null>(null)

  const allWorkflows = useMemo(() => data?.workflows ?? [], [data?.workflows])
  const folders = useMemo(() => (data?.folders ?? []).filter((f) => f.kind === 'workflow'), [data?.folders])
  const counts = useMemo(() => buildLibraryCounts(folders, allWorkflows, workflowNeedsAttention), [folders, allWorkflows])

  useEffect(() => {
    if (selection.kind === 'folder' && !folders.some((folder) => folder.id === selection.id)) {
      setSelection({ kind: 'all' })
    }
  }, [folders, selection])

  const filtered = useMemo(() => {
    const known = folderMap(folders)
    const needle = query.trim().toLowerCase()
    const selectedFolderIDs = selection.kind === 'folder' ? folderDescendantIDs(folders, selection.id) : null
    const items = allWorkflows.filter((workflow) => {
      if (selection.kind === 'pinned' && !workflow.pinned) return false
      if (selection.kind === 'attention' && !workflowNeedsAttention(workflow)) return false
      if (selection.kind === 'unfiled' && workflow.folder_id && known.has(workflow.folder_id)) return false
      if (selectedFolderIDs && (!workflow.folder_id || !selectedFolderIDs.has(workflow.folder_id))) return false
      if (needle && !workflow.name.toLowerCase().includes(needle)) return false
      const status = workflow.version?.status || ''
      if (statusFilter === 'approved' && status !== 'approved') return false
      if (statusFilter === 'draft' && status !== 'draft') return false
      return true
    })
    return items.sort((a, b) => {
      const pinned = Number(!!b.pinned) - Number(!!a.pinned)
      if (pinned !== 0) return pinned
      if (sort === 'newest') return b.created_at - a.created_at
      if (sort === 'updated') {
        return (b.version?.created_at || b.created_at) - (a.version?.created_at || a.created_at)
      }
      return a.name.localeCompare(b.name)
    })
  }, [allWorkflows, folders, selection, query, statusFilter, sort])

  const groups = useMemo(() => {
    const known = folderMap(folders)
    if (selection.kind !== 'all' || query.trim()) {
      return [{ key: 'view', title: workflowSelectionTitle(selection, folders, query), items: filtered }]
    }
    const out: { key: string; title: string; items: Workflow[] }[] = []
    for (const folder of topLevelFolders(folders)) {
      const IDs = folderDescendantIDs(folders, folder.id)
      const items = filtered.filter((workflow) => workflow.folder_id && IDs.has(workflow.folder_id))
      if (items.length) out.push({ key: folder.id, title: folder.name, items })
    }
    const unfiled = filtered.filter((workflow) => !workflow.folder_id || !known.has(workflow.folder_id))
    if (unfiled.length) out.push({ key: 'unfiled', title: 'Unfiled', items: unfiled })
    return out
  }, [filtered, folders, selection, query])

  if (!data) return null

  const title = selection.kind === 'folder' ? folderPath(folders, selection.id) : workflowSelectionTitle(selection, folders, query)
  const filtersActive = !!query || statusFilter !== 'all'
  const emptyCopy = workflowEmptyState(selection, query)

  async function saveFolder(name: string, parentID: string) {
    try {
      if (folderDialog?.mode === 'edit' && folderDialog.folder) {
        await api.updateFolder(folderDialog.folder.id, { name, parent_id: parentID })
        await refresh()
        toast('Folder updated', name, 'success')
      } else {
        const folder = await api.createFolder({ kind: 'workflow', name, parent_id: parentID || undefined })
        await refresh()
        setSelection({ kind: 'folder', id: folder.id })
        toast('Folder created', name, 'success')
      }
      return true
    } catch (err) {
      toast('Could not save the folder', err instanceof Error ? err.message : undefined, 'error')
      return false
    }
  }

  async function removeFolder(folder: FolderRecord) {
    const IDs = folderDescendantIDs(folders, folder.id)
    const ok = await confirm({
      title: 'Delete folder',
      message: `Delete “${folder.name}”? Nested folders are removed and their workflows move to the parent folder. The workflows themselves are not deleted.`,
      confirmLabel: 'Delete folder',
    })
    if (!ok) return
    try {
      await api.deleteFolder(folder.id)
      if (selection.kind === 'folder' && IDs.has(selection.id)) setSelection({ kind: 'all' })
      toast('Folder deleted', 'Its workflows were moved to the parent folder.', 'success')
      await refresh()
    } catch (err) {
      toast('Could not delete the folder', err instanceof Error ? err.message : undefined, 'error')
    }
  }

  async function togglePin(workflow: Workflow) {
    setBusy(`pin-${workflow.id}`)
    try {
      await api.updateWorkflow(workflow.id, { pinned: !workflow.pinned })
      await refresh()
      toast(workflow.pinned ? 'Workflow unpinned' : 'Workflow pinned', workflow.name, 'success')
    } catch (err) {
      toast('Could not update the workflow', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(null)
    }
  }

  async function moveWorkflowTo(workflow: Workflow, folderID: string) {
    try {
      await api.updateWorkflow(workflow.id, { folder_id: folderID })
      await refresh()
      toast('Workflow moved', folderID ? folderPath(folders, folderID) : 'No folder', 'success')
      return true
    } catch (err) {
      toast('Could not move the workflow', err instanceof Error ? err.message : undefined, 'error')
      return false
    }
  }

  return (
    <div className="page">
      <PageHeader
        eyebrow="Automations"
        title="Workflows"
        description="Reusable, versioned graphs. Group them into folders, pin the ones you run often, and keep drafts visible until they are approved."
        actions={
          <>
            <div className="search-box">
              <GitBranch size={14} />
              <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Filter workflows…" />
            </div>
            <Button variant="primary" icon={<Plus size={15} />} onClick={() => navigate('/workflows/new')}>New workflow</Button>
          </>
        }
      />
      <div className="library-layout">
        <LibraryNav
          folders={folders}
          counts={counts}
          selection={selection}
          onSelect={setSelection}
          onCreateFolder={() => setFolderDialog({ mode: 'create' })}
          onRenameFolder={(folder) => setFolderDialog({ mode: 'edit', folder })}
          onDeleteFolder={removeFolder}
        />
        <div className="library-main">
          <div className="library-toolbar">
            <div className="library-title">
              <h2>{title}</h2>
              <span>{filtered.length === allWorkflows.length ? `${allWorkflows.length} workflows` : `${filtered.length} of ${allWorkflows.length} workflows`}</span>
            </div>
            <div className="library-filters">
              <Select className="compact" value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)} aria-label="Filter by status">
                <option value="all">Any status</option>
                <option value="approved">Approved</option>
                <option value="draft">Draft</option>
              </Select>
              <Select className="compact" value={sort} onChange={(e) => setSort(e.target.value as typeof sort)} aria-label="Sort workflows">
                <option value="name">Name</option>
                <option value="newest">Newest</option>
                <option value="updated">Recently updated</option>
              </Select>
              {filtersActive && (
                <button className="link-btn tiny" onClick={() => { setQuery(''); setStatusFilter('all') }}>Reset</button>
              )}
            </div>
          </div>

          {groups.map((group) => (
            <section key={group.key} className="library-group">
              {selection.kind === 'all' && !query.trim() && (
                <div className="library-group-head">
                  <Folder size={14} />
                  <h3>{group.title}</h3>
                  <span>{group.items.length}</span>
                </div>
              )}
              <div className="workflow-list">
                {group.items.map((workflow) => (
                  <WorkflowRow
                    key={workflow.id}
                    workflow={workflow}
                    runs={data.runs.filter((run) => run.target_id === workflow.id).slice(0, 3)}
                    folderLabel={folderPath(folders, workflow.folder_id)}
                    pinning={busy === `pin-${workflow.id}`}
                    onPin={() => void togglePin(workflow)}
                    onMove={() => setMoveWorkflow(workflow)}
                  />
                ))}
              </div>
            </section>
          ))}

          {filtered.length === 0 && (
            <EmptyState
              icon={<GitBranch size={28} />}
              title={emptyCopy.title}
              description={emptyCopy.description}
              action={filtersActive
                ? <Button variant="subtle" onClick={() => { setQuery(''); setStatusFilter('all') }}>Clear filters</Button>
                : <Button variant="primary" icon={<Plus size={15} />} onClick={() => navigate('/workflows/new')}>New workflow</Button>}
            />
          )}
        </div>
      </div>

      <FolderDialog
        open={!!folderDialog}
        onClose={() => setFolderDialog(null)}
        mode={folderDialog?.mode || 'create'}
        folder={folderDialog?.folder}
        folders={folders}
        defaultParentID={selection.kind === 'folder' ? selection.id : undefined}
        onSubmit={saveFolder}
      />
      <MoveToFolderDialog
        open={!!moveWorkflow}
        onClose={() => setMoveWorkflow(null)}
        itemName={moveWorkflow?.name || ''}
        folders={folders}
        currentFolderID={moveWorkflow?.folder_id}
        onMove={(folderID) => moveWorkflow ? moveWorkflowTo(moveWorkflow, folderID) : Promise.resolve(false)}
      />
    </div>
  )
}

function workflowSelectionTitle(selection: LibrarySelection, folders: FolderRecord[], query: string) {
  if (query.trim()) return `Results for “${query.trim()}”`
  if (selection.kind === 'pinned') return 'Pinned workflows'
  if (selection.kind === 'attention') return 'Needs attention'
  if (selection.kind === 'unfiled') return 'Unfiled'
  if (selection.kind === 'folder') return folderPath(folders, selection.id) || 'Folder'
  return 'All workflows'
}

function workflowEmptyState(selection: LibrarySelection, query: string) {
  if (query.trim()) return { title: 'No matching workflows', description: 'Try another name or clear the current filters.' }
  if (selection.kind === 'pinned') return { title: 'Nothing pinned yet', description: 'Pin workflows you run often and they will stay at the top of every list.' }
  if (selection.kind === 'attention') return { title: 'Nothing needs attention', description: 'Every workflow is approved and ready to run.' }
  if (selection.kind === 'unfiled') return { title: 'No unfiled workflows', description: 'Every workflow belongs to a folder. Create another folder to keep the library tidy.' }
  if (selection.kind === 'folder') return { title: 'This folder is empty', description: 'Move a workflow here with the folder button on any row, or create something new.' }
  return { title: 'Build your first workflow', description: 'Ask the agent to turn a successful chat into a workflow, or start from YAML.' }
}

function WorkflowRow({
  workflow,
  runs,
  folderLabel,
  pinning,
  onPin,
  onMove,
}: {
  workflow: Workflow
  runs: Run[]
  folderLabel: string
  pinning: boolean
  onPin: () => void
  onMove: () => void
}) {
  const v = workflow.version
  const steps = workflow.steps || []
  const last = runs[0]
  return (
    <div className={clsx('workflow-row', workflow.pinned && 'pinned')} onClick={() => navigate(`/workflows/${workflow.id}`)}>
      <div className="workflow-row-icon"><WorkflowIcon size={18} /></div>
      <div className="workflow-row-main">
        <div className="workflow-row-title">
          <strong>{workflow.name}</strong>
          {v && <Badge tone={statusTone(v.status)}>{v.status}</Badge>}
          {v && v.status !== 'draft' && <span className="muted tiny">{versionLabel(v)}</span>}
        </div>
        <div className="workflow-row-meta">
          <span>{steps.length} steps</span>
          <span className="dot-sep" />
          <span>{steps.slice(0, 3).map((s) => s.summary || s.id).join(' → ') || 'No graph yet'}</span>
          {folderLabel && <><span className="dot-sep" /><span className="folder-inline"><Folder size={11} />{folderLabel}</span></>}
        </div>
      </div>
      <div className="workflow-row-last">
        {last ? (
          <>
            <StatusBadge status={last.status} />
            <span className="muted tiny">{formatRelative(last.created_at)}</span>
          </>
        ) : (
          <span className="muted tiny">Never run</span>
        )}
      </div>
      <div className="workflow-row-actions">
        <IconButton className={clsx('tile-icon-btn', workflow.pinned && 'active')} label={workflow.pinned ? `Unpin ${workflow.name}` : `Pin ${workflow.name}`} onClick={(e) => { e.stopPropagation(); onPin() }} disabled={pinning}>
          <Star size={13} fill={workflow.pinned ? 'currentColor' : 'none'} />
        </IconButton>
        <IconButton className="tile-icon-btn" label={`Move ${workflow.name} to folder`} onClick={(e) => { e.stopPropagation(); onMove() }}><FolderInput size={13} /></IconButton>
        <Button size="sm" variant="subtle" icon={<Play size={13} />} onClick={(e) => { e.stopPropagation(); navigate(`/workflows/${workflow.id}`) }}>
          Open
        </Button>
        <ChevronRight size={16} className="muted" />
      </div>
    </div>
  )
}

export function WorkflowPage({ workflowId }: { workflowId: string }) {
  const isNew = workflowId === 'new'
  const { refresh, data } = useApp()
  const toast = useToast()
  const confirm = useConfirm()
  const [detail, setDetail] = useState<WorkflowDetail | null>(null)
  const [yaml, setYaml] = useState(isNew ? STARTER_YAML : '')
  const [savedYaml, setSavedYaml] = useState(isNew ? STARTER_YAML : '')
  const [tab, setTab] = useState<Tab>(isNew ? 'yaml' : 'graph')
  const [validation, setValidation] = useState<ValidationResult | null>(null)
  const [selectedStep, setSelectedStep] = useState<string | undefined>()
  const [loading, setLoading] = useState(!isNew)
  const [busy, setBusy] = useState<string | null>(null)
  const [runOpen, setRunOpen] = useState(false)
  const [menuOpen, setMenuOpen] = useState(false)
  const [yamlDirty, setYamlDirty] = useState(false)

  const load = useCallback(async () => {
    if (isNew) {
      setLoading(false)
      return
    }
    try {
      const d = await api.workflow(workflowId)
      setDetail(d)
      const currentYaml = d.workflow.version?.yaml || d.versions[0]?.yaml || ''
      setYaml(currentYaml)
      setSavedYaml(currentYaml)
      setYamlDirty(false)
      if (d.workflow.version) {
        setTab((t) => (t === 'yaml' && !yamlDirty ? 'graph' : t))
      }
    } catch (err) {
      toast('Could not load the workflow', err instanceof Error ? err.message : undefined, 'error')
      navigate('/workflows')
    } finally {
      setLoading(false)
    }
  }, [workflowId, isNew, toast, yamlDirty])

  useEffect(() => {
    setLoading(!isNew)
    setDetail(null)
    setSelectedStep(undefined)
    void load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workflowId])

  if (loading) return <div className="page center-page"><Spinner size={26} /></div>

  const spec = isNew ? undefined : detail?.spec
  const current = detail?.workflow.version
  const canApprove = current?.status === 'draft'
  const workflowName = current ? detail?.workflow.name : isNew ? 'New workflow' : detail?.workflow.name
  const folderLabel = folderPath(data?.folders || [], detail?.workflow.folder_id)

  async function validate() {
    setBusy('validate')
    try {
      const res = await api.validateWorkflow(yaml, false)
      setValidation(res)
      toast(res.errors.length ? `${res.errors.length} validation error(s)` : 'Workflow is valid', res.errors.length ? 'Fix the E-codes before saving.' : 'Warnings are advice; errors block saving.', res.errors.length ? 'error' : 'success')
      return res
    } catch (err) {
      toast('Validation failed', err instanceof Error ? err.message : undefined, 'error')
      return null
    } finally {
      setBusy(null)
    }
  }

  async function save(approve = false) {
    setBusy(approve ? 'save-approve' : 'save')
    try {
      const res = await api.saveWorkflow({
        yaml,
        change_note: isNew ? 'Initial draft' : 'Edited in the workflow canvas',
        base_version: !isNew && current ? current.version : undefined,
        approve,
      })
      setSavedYaml(yaml)
      setYamlDirty(false)
      setDetail((d) => d)
      toast(approve ? 'Workflow saved and approved' : 'Draft saved', undefined, 'success')
      void refresh()
      if (isNew) {
        navigate(`/workflows/${res.workflow.id}`)
      } else {
        await load()
        if (approve) setTab('graph')
      }
      return res
    } catch (err) {
      const e = err as Error & { details?: { validation?: ValidationResult } }
      if (e.details?.validation) setValidation(e.details.validation)
      toast('Could not save the workflow', e.message, 'error')
      return null
    } finally {
      setBusy(null)
    }
  }

  async function approve() {
    if (!detail || !current) return
    setBusy('approve')
    try {
      await api.approveWorkflow(detail.workflow.id, current.id)
      toast('Workflow approved', 'Triggers can now run it.', 'success')
      await load()
      void refresh()
    } catch (err) {
      toast('Could not approve', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(null)
    }
  }

  async function remove() {
    if (!detail) return
    const ok = await confirm({
      title: 'Delete workflow',
      message: `Delete “${detail.workflow.name}” and its runs? This cannot be undone.`,
      confirmLabel: 'Delete workflow',
    })
    if (!ok) return
    try {
      await api.deleteWorkflow(detail.workflow.id)
      toast('Workflow deleted', undefined, 'success')
      void refresh()
      navigate('/workflows')
    } catch (err) {
      toast('Could not delete', err instanceof Error ? err.message : undefined, 'error')
    }
  }

  return (
    <div className="workflow-page">
      <header className="workflow-head">
        <div className="workflow-head-main">
          <button className="back-link" onClick={() => navigate('/workflows')}><ArrowLeft size={14} /> Workflows</button>
          <div className="workflow-title-row">
            <span className="workflow-icon"><WorkflowIcon size={20} /></span>
            <div>
              <h1>{workflowName}</h1>
              <div className="workflow-subline">
                {current && <Badge tone={statusTone(current.status)}>{current.status}</Badge>}
                {current && current.status !== 'draft' && <span>{versionLabel(current)}</span>}
                {current && <span className="dot-sep" />}
                <span>{current?.created_by || 'you'}</span>
                {current && <span className="dot-sep" />}
                <span>{current ? formatRelative(current.created_at) : 'not saved'}</span>
                {folderLabel && <><span className="dot-sep" /><span className="folder-inline"><Folder size={12} />{folderLabel}</span></>}
                {detail?.validation?.warnings?.length ? <span className="warn-text">{detail.validation.warnings.length} warning(s)</span> : null}
              </div>
            </div>
          </div>
        </div>
        <div className="workflow-actions">
          {canApprove && <Button variant="subtle" size="sm" icon={<ShieldCheck size={15} />} onClick={approve} loading={busy === 'approve'}>Approve draft</Button>}
          {!isNew && (
            <Button variant="ghost" size="sm" icon={<Sparkles size={14} />} onClick={async () => {
              setBusy('chat')
              try {
                const conv = await api.workflowEditChat(workflowId)
                void refresh()
                navigate(`/chats/${conv.id}`)
              } catch (err) {
                toast('Could not open the edit chat', err instanceof Error ? err.message : undefined, 'error')
              } finally { setBusy(null) }
            }} loading={busy === 'chat'}>Edit with agent</Button>
          )}
          {!isNew && <Button variant="primary" size="sm" icon={<Play size={14} />} onClick={() => setRunOpen(true)}>Run</Button>}
          {!isNew && (
            <div className="menu-wrap">
              <IconButton label="Workflow menu" onClick={() => setMenuOpen((o) => !o)}><MoreHorizontal size={17} /></IconButton>
              {menuOpen && (
                <div className="menu-pop">
                  <button onClick={() => { setTab('yaml'); setMenuOpen(false) }}><Code2 size={14} /> Edit YAML</button>
                  <button onClick={() => { setRunOpen(true); setMenuOpen(false) }}><Play size={14} /> Run with inputs</button>
                  <button className="danger" onClick={remove}><Trash2 size={14} /> Delete workflow</button>
                </div>
              )}
            </div>
          )}
        </div>
      </header>

      {isNew ? (
        <NewWorkflowEditor
          yaml={yaml}
          setYaml={(v) => { setYaml(v); setYamlDirty(true) }}
          validation={validation}
          validate={validate}
          save={save}
          busy={busy}
        />
      ) : (
        <>
          <div className="workflow-tabs-row">
            <Tabs
              tabs={[
                { id: 'graph', label: <><GitBranch size={13} /> Graph</> },
                { id: 'yaml', label: <><FileCode2 size={13} /> YAML {yamlDirty && <span className="dirty-dot" />}</> },
                { id: 'versions', label: <><History size={13} /> Versions {detail?.versions.length ? <span className="tab-count">{detail.versions.length}</span> : null}</> },
                { id: 'triggers', label: <><CalendarClock size={13} /> Triggers {detail?.triggers.length ? <span className="tab-count">{detail.triggers.length}</span> : null}</> },
              ]}
              value={tab}
              onChange={setTab}
            />
            {tab === 'graph' && spec && (
              <div className="workflow-meta-actions">
                <span className="muted tiny">{spec.steps.length} steps</span>
                <Button size="sm" variant="ghost" icon={<Code2 size={13} />} onClick={() => setTab('yaml')}>Edit YAML</Button>
              </div>
            )}
          </div>

          {tab === 'graph' && (
            <div className="workflow-workspace">
              <div className="workflow-canvas-panel">
                {spec ? (
                  <WorkflowCanvas spec={spec} selectedStep={selectedStep} onSelectStep={setSelectedStep} />
                ) : (
                  <div className="muted-block">No runnable graph.</div>
                )}
              </div>
              <WorkflowSidePanel
                detail={detail!}
                selectedStepId={selectedStep}
                onClose={() => setSelectedStep(undefined)}
              />
            </div>
          )}

          {tab === 'yaml' && (
            <div className="yaml-workspace">
              <div className="yaml-editor-panel">
                <div className="yaml-toolbar">
                  <div>
                    <strong>Workflow YAML</strong>
                    <span className={clsx('muted tiny', yamlDirty && 'warn-text')}>{yamlDirty ? 'unsaved changes' : 'saved'}</span>
                  </div>
                  <div className="toolbar-actions">
                    <Button size="sm" variant="ghost" onClick={() => { setYaml(savedYaml); setYamlDirty(false); setValidation(null) }} disabled={!yamlDirty}>Reset</Button>
                    <Button size="sm" variant="subtle" icon={<Check size={14} />} onClick={validate} loading={busy === 'validate'}>Validate</Button>
                    <Button size="sm" variant="subtle" icon={<Save size={14} />} onClick={() => save(false)} loading={busy === 'save'} disabled={!yamlDirty || !current}>Save draft</Button>
                    <Button size="sm" variant="primary" icon={<ShieldCheck size={14} />} onClick={() => save(true)} loading={busy === 'save-approve'} disabled={!yamlDirty}>Save & approve</Button>
                  </div>
                </div>
                <CodeEditor value={yaml} onChange={(v) => { setYaml(v); setYamlDirty(v !== savedYaml) }} />
                <div className="yaml-foot">
                  <span>{yaml.split('\n').length} lines</span>
                  <span>{yaml.length.toLocaleString()} chars</span>
                  {validation && <span>{validation.errors.length} errors · {validation.warnings.length} warnings</span>}
                </div>
              </div>
              <aside className="yaml-side">
                <SectionTitle hint="E-codes block saving. W-codes are advice.">Diagnostics</SectionTitle>
                <DiagnosticsList result={validation || detail?.validation} />
                <div className="yaml-help">
                  <strong>Quick rules</strong>
                  <ul>
                    <li>Every step needs a unique <code>id</code>.</li>
                    <li>Reference only declared inputs and ancestor steps.</li>
                    <li>Put <code>approval</code> before write/external steps.</li>
                    <li><code>needs</code> defines the DAG. Independent steps run in parallel.</li>
                  </ul>
                </div>
              </aside>
            </div>
          )}

          {tab === 'versions' && detail && (
            <VersionsTab
              detail={detail}
              onApprove={async (v) => {
                setBusy('approve-' + v.id)
                try {
                  await api.approveWorkflow(detail.workflow.id, v.id)
                  toast(`Version ${v.version} approved`, undefined, 'success')
                  await load()
                  void refresh()
                } catch (err) {
                  toast('Could not approve version', err instanceof Error ? err.message : undefined, 'error')
                } finally {
                  setBusy(null)
                }
              }}
              onLoad={(v) => { setYaml(v.yaml); setSavedYaml(v.yaml); setYamlDirty(false); setTab('yaml') }}
              busy={busy}
            />
          )}

          {tab === 'triggers' && detail && (
            <TriggersTab detail={detail} onChanged={() => { void load(); void refresh() }} />
          )}
        </>
      )}

      {detail && (
        <RunWorkflowDialog
          open={runOpen}
          onClose={() => setRunOpen(false)}
          detail={detail}
          onStarted={(run) => { setRunOpen(false); navigate(`/runs/${run.id}`) }}
        />
      )}
    </div>
  )
}

function NewWorkflowEditor({
  yaml,
  setYaml,
  validation,
  validate,
  save,
  busy,
}: {
  yaml: string
  setYaml: (s: string) => void
  validation: ValidationResult | null
  validate: () => Promise<ValidationResult | null>
  save: (approve?: boolean) => Promise<unknown>
  busy: string | null
}) {
  return (
    <div className="yaml-workspace new-workflow">
      <div className="yaml-editor-panel">
        <div className="yaml-toolbar">
          <div>
            <strong>New workflow</strong>
            <span className="muted tiny">Starts as a draft. Nothing runs until you approve it.</span>
          </div>
          <div className="toolbar-actions">
            <Button size="sm" variant="subtle" icon={<Check size={14} />} onClick={validate} loading={busy === 'validate'}>Validate</Button>
            <Button size="sm" variant="primary" icon={<Save size={14} />} onClick={() => save(false)} loading={busy === 'save'}>Save draft</Button>
          </div>
        </div>
        <CodeEditor value={yaml} onChange={setYaml} />
      </div>
      <aside className="yaml-side">
        <SectionTitle hint="Save a draft, then dry-run or chat with the agent to iterate.">Start with YAML</SectionTitle>
        <DiagnosticsList result={validation || undefined} />
        <div className="yaml-help">
          <strong>Starter structure</strong>
          <p>Every workflow has inputs, steps and outputs. Tool names are available in the Tools tab.</p>
          <Button variant="ghost" size="sm" onClick={() => setYaml(STARTER_YAML)}>Load starter</Button>
        </div>
      </aside>
    </div>
  )
}

function CodeEditor({ value, onChange, readOnly }: { value: string; onChange?: (v: string) => void; readOnly?: boolean }) {
  const lines = Math.max(value.split('\n').length, 1)
  const gutterRef = useRef<HTMLDivElement>(null)
  return (
    <div className="code-editor">
      <div className="code-gutter" aria-hidden ref={gutterRef}>
        {Array.from({ length: lines }, (_, i) => <span key={i}>{i + 1}</span>)}
      </div>
      <textarea
        spellCheck={false}
        readOnly={readOnly}
        value={value}
        onScroll={(e) => {
          if (gutterRef.current) gutterRef.current.scrollTop = e.currentTarget.scrollTop
        }}
        onChange={(e) => onChange?.(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Tab') {
            e.preventDefault()
            const target = e.currentTarget
            const start = target.selectionStart
            const end = target.selectionEnd
            const next = value.slice(0, start) + '  ' + value.slice(end)
            onChange?.(next)
            requestAnimationFrame(() => {
              target.selectionStart = target.selectionEnd = start + 2
            })
          }
        }}
      />
    </div>
  )
}

function WorkflowSidePanel({ detail, selectedStepId, onClose }: { detail: WorkflowDetail; selectedStepId?: string; onClose: () => void }) {
  const spec = detail.spec
  const step = spec?.steps.find((s) => s.id === selectedStepId)
  const runs = detail.runs || []
  if (step) {
    return (
      <aside className="workflow-side">
        <div className="side-head">
          <div><div className="eyebrow">Step</div><h2>{step.id}</h2></div>
          <IconButton label="Close" onClick={onClose}><X size={15} /></IconButton>
        </div>
        <div className="side-badges">
          <Badge>{stepKindLabel(step)}</Badge>
          {step.side_effect && <Badge tone={step.side_effect === 'write' || step.side_effect === 'external' ? 'warn' : 'neutral'}>{step.side_effect}</Badge>}
          {(step.needs || []).length > 0 && <Badge tone="neutral">after {step.needs?.join(', ')}</Badge>}
        </div>
        <div className="side-scroll">
          <SectionTitle>Configuration</SectionTitle>
          <pre className="json-block">{JSON.stringify(step, null, 2)}</pre>
          {step.approval && (
            <div className="side-note"><ShieldCheck size={14} /> This step pauses the run and waits for a human decision.</div>
          )}
        </div>
      </aside>
    )
  }
  return (
    <aside className="workflow-side">
      <div className="side-head">
        <div><div className="eyebrow">Workflow</div><h2>{detail.workflow.name}</h2></div>
      </div>
      <div className="side-scroll">
        {spec?.description && <p className="side-description">{spec.description}</p>}
        <div className="side-stats">
          <div><strong>{spec?.steps.length || 0}</strong><span>steps</span></div>
          <div><strong>{Object.keys(spec?.inputs || {}).length}</strong><span>inputs</span></div>
          <div><strong>{runs.length}</strong><span>runs</span></div>
        </div>
        <SectionTitle>Inputs</SectionTitle>
        <InputSpecList inputs={spec?.inputs} />
        <SectionTitle hint="Click a node to inspect it.">Recent runs</SectionTitle>
        <div className="side-runs">
          {runs.slice(0, 6).map((r) => (
            <button key={r.id} onClick={() => navigate(`/runs/${r.id}`)}>
              <StatusBadge status={r.status} />
              <span>{formatRelative(r.created_at)}</span>
              <span className="mono tiny">{shortId(r.id)}</span>
              <ChevronRight size={13} />
            </button>
          ))}
          {runs.length === 0 && <div className="muted small">No runs yet.</div>}
        </div>
        <SectionTitle>Validation</SectionTitle>
        <DiagnosticsList result={detail.validation} compact />
      </div>
    </aside>
  )
}

function stepKindLabel(step: StepSpec) {
  if (step.tool) return `tool · ${step.tool}`
  if (step.llm) return `llm · ${step.llm.role || 'executor'}`
  if (step.agent) return 'agent'
  if (step.map) return 'map'
  if (step.branch) return 'branch'
  if (step.parallel) return 'parallel'
  if (step.approval) return 'approval'
  if (step.workflow) return 'subworkflow'
  if (step.set) return 'set'
  return 'step'
}

function VersionsTab({
  detail,
  onApprove,
  onLoad,
  busy,
}: {
  detail: WorkflowDetail
  onApprove: (v: WorkflowVersion) => void
  onLoad: (v: WorkflowVersion) => void
  busy: string | null
}) {
  return (
    <div className="versions-tab">
      <div className="versions-table">
        <div className="versions-head">
          <span>Version</span><span>Status</span><span>Author</span><span>Note</span><span>Created</span><span />
        </div>
        {detail.versions.map((v) => (
          <div key={v.id} className="versions-row">
            <span className="mono">{versionLabel(v)}</span>
            <StatusBadge status={v.status} />
            <span>{v.created_by}</span>
            <span className="muted">{v.change_note || '—'}</span>
            <span className="muted tiny">{formatDate(v.created_at)}</span>
            <div className="versions-actions">
              <button className="link-btn" onClick={() => onLoad(v)}>View YAML</button>
              {v.status === 'draft' && <Button size="sm" variant="subtle" loading={busy === 'approve-' + v.id} onClick={() => onApprove(v)}>Approve</Button>}
            </div>
          </div>
        ))}
      </div>
      <div className="versions-note">
        <Info size={14} />
        <span>Drafts stay editable until approved. Approving snapshots the working draft as the next version; approved versions are immutable.</span>
      </div>
    </div>
  )
}

function TriggersTab({ detail, onChanged }: { detail: WorkflowDetail; onChanged: () => void }) {
  const toast = useToast()
  const confirm = useConfirm()
  const [triggers, setTriggers] = useState<Trigger[]>(detail.triggers || [])
  const [open, setOpen] = useState(false)
  const [kind, setKind] = useState('cron')
  const [schedule, setSchedule] = useState('0 9 * * 1')
  const [paths, setPaths] = useState('.')
  const [unattended, setUnattended] = useState('ask')
  const [concurrency, setConcurrency] = useState('queue')
  const [inputs, setInputs] = useState<Record<string, unknown>>(() => schemaDefaults(detail.workflow.version?.input_schema))
  const [busy, setBusy] = useState<string | null>(null)

  useEffect(() => setTriggers(detail.triggers || []), [detail])

  async function create() {
    setBusy('create')
    try {
      const config: Record<string, unknown> = kind === 'cron' ? { schedule } : kind === 'file' ? { paths: paths.split('\n').map((s) => s.trim()).filter(Boolean) } : {}
      const res = await api.createTrigger(detail.workflow.id, { kind, config, inputs, enabled: true, unattended, concurrency })
      setTriggers((prev) => [...prev, res])
      setOpen(false)
      toast('Trigger enabled', res.kind === 'webhook' ? 'Copy the webhook URL from the trigger card.' : undefined, 'success')
      onChanged()
    } catch (err) {
      toast('Could not create trigger', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(null)
    }
  }

  async function toggle(t: Trigger, enabled: boolean) {
    setBusy(t.id)
    try {
      const next = await api.updateTrigger(t.id, { enabled })
      setTriggers((prev) => prev.map((x) => x.id === t.id ? next : x))
      onChanged()
    } catch (err) {
      toast('Could not update trigger', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(null)
    }
  }

  async function remove(t: Trigger) {
    const ok = await confirm({
      title: 'Delete trigger',
      message: 'Delete this trigger? Scheduled and watched runs will stop.',
      confirmLabel: 'Delete trigger',
    })
    if (!ok) return
    setBusy(t.id)
    try {
      await api.deleteTrigger(t.id)
      setTriggers((prev) => prev.filter((x) => x.id !== t.id))
      onChanged()
    } catch (err) {
      toast('Could not delete trigger', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(null)
    }
  }

  const inputSchema = detail.workflow.version?.input_schema as JSONSchema | undefined
  const canTrigger = detail.workflow.version?.status === 'approved'

  return (
    <div className="triggers-tab">
      <div className="triggers-top">
        <div>
          <h2>Triggers</h2>
          <p>Run this workflow without a chat. Automated runs pause at approvals unless the workflow allows unattended work.</p>
        </div>
        <Button variant="primary" size="sm" icon={<Plus size={14} />} onClick={() => setOpen(true)} disabled={!canTrigger}>Add trigger</Button>
      </div>
      {!canTrigger && <InlineNotice tone="warn" title="Approve this version first">Triggers can only start approved workflow versions.</InlineNotice>}
      <div className="trigger-grid">
        {triggers.map((t) => (
          <TriggerCard key={t.id} trigger={t} busy={busy === t.id} onToggle={(v) => toggle(t, v)} onDelete={() => remove(t)} />
        ))}
        {triggers.length === 0 && (
          <EmptyState compact icon={<CalendarClock size={24} />} title="No triggers" description="Add a schedule, file watcher or local webhook." action={<Button variant="subtle" size="sm" onClick={() => setOpen(true)}>Add trigger</Button>} />
        )}
      </div>
      <Modal open={open} onClose={() => setOpen(false)} title="Add a trigger" subtitle="Creating a trigger is an approval decision: automated runs can act while you are away." width={620}
        footer={<><Button variant="ghost" onClick={() => setOpen(false)}>Cancel</Button><Button variant="primary" onClick={create} loading={busy === 'create'}>Enable trigger</Button></>}>
        <div className="form-grid">
          <Field label="Kind">
            <Select value={kind} onChange={(e) => setKind(e.target.value)}>
              <option value="cron">Schedule (cron)</option>
              <option value="file">Folder watcher</option>
              <option value="webhook">Local webhook</option>
            </Select>
          </Field>
          {kind === 'cron' && (
            <Field label="Schedule" hint="Five-field cron in local time. Example: 0 9 * * 1 (Mondays 09:00).">
              <TextInput value={schedule} onChange={(e) => setSchedule(e.target.value)} placeholder="0 9 * * 1" />
            </Field>
          )}
          {kind === 'file' && (
            <Field label="Paths" hint="One path per line, relative to the workspace.">
              <TextArea rows={3} value={paths} onChange={(e) => setPaths(e.target.value)} />
            </Field>
          )}
          {kind === 'webhook' && <InlineNotice tone="info" title="A token is generated">POST to the URL with the token. The endpoint only listens on loopback.</InlineNotice>}
          <div className="form-grid two">
            <Field label="Unattended policy">
              <Select value={unattended} onChange={(e) => setUnattended(e.target.value)}>
                <option value="ask">Ask and wait</option>
                <option value="allow">Allow risky steps</option>
                <option value="deny">Deny unattended approvals</option>
              </Select>
            </Field>
            <Field label="Concurrency">
              <Select value={concurrency} onChange={(e) => setConcurrency(e.target.value)}>
                <option value="queue">Queue</option>
                <option value="skip">Skip if running</option>
                <option value="cancel-previous">Cancel previous</option>
              </Select>
            </Field>
          </div>
          <div className="form-section-label">Fixed inputs</div>
          <InputForm schema={inputSchema} value={inputs} onChange={setInputs} />
        </div>
      </Modal>
    </div>
  )
}

function TriggerCard({ trigger, busy, onToggle, onDelete }: { trigger: Trigger; busy: boolean; onToggle: (v: boolean) => void; onDelete: () => void }) {
  const cfg = trigger.config || {}
  const token = String(cfg.token || '')
  const hookURL = `${window.location.origin}/api/hooks/${trigger.id}${token ? `?token=${encodeURIComponent(token)}` : ''}`
  return (
    <div className="trigger-card">
      <div className="trigger-head">
        <span className="trigger-icon">{trigger.kind === 'cron' ? <Clock3 size={16} /> : trigger.kind === 'file' ? <RefreshCw size={16} /> : <Webhook size={16} />}</span>
        <div>
          <strong>{trigger.kind === 'cron' ? 'Schedule' : trigger.kind === 'file' ? 'Folder watcher' : 'Local webhook'}</strong>
          <span className="muted tiny">{trigger.enabled ? 'enabled' : 'disabled'} · {trigger.unattended} · {trigger.concurrency}</span>
        </div>
        <Toggle checked={trigger.enabled} onChange={onToggle} disabled={busy} />
      </div>
      <div className="trigger-config">
        {trigger.kind === 'cron' && <code>{String(cfg.schedule || '')}</code>}
        {trigger.kind === 'file' && <code>{Array.isArray(cfg.paths) ? cfg.paths.join(', ') : String(cfg.paths || '')}</code>}
        {trigger.kind === 'webhook' && (
          <div className="webhook-row">
            <code className="truncate">{hookURL}</code>
            <CopyButton text={hookURL} label="Copy URL" />
          </div>
        )}
      </div>
      <div className="trigger-foot">
        <span>{trigger.last_fired_at ? `Last fired ${formatRelative(trigger.last_fired_at)}` : 'Never fired'}</span>
        <button className="link-btn danger" onClick={onDelete} disabled={busy}>Delete</button>
      </div>
    </div>
  )
}

function RunWorkflowDialog({
  open,
  onClose,
  detail,
  onStarted,
}: {
  open: boolean
  onClose: () => void
  detail: WorkflowDetail
  onStarted: (run: Run) => void
}) {
  const toast = useToast()
  const schema = detail.workflow.version?.input_schema as JSONSchema | undefined
  const [inputs, setInputs] = useState<Record<string, unknown>>(() => schemaDefaults(schema))
  const [busy, setBusy] = useState(false)
  const errors = validateInputs(schema, inputs)
  useEffect(() => {
    if (open) setInputs(schemaDefaults(schema))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])
  const warnings = detail.validation?.warnings?.length || 0
  const errorsCount = detail.validation?.errors?.length || 0
  async function run() {
    setBusy(true)
    try {
      const res = await api.runWorkflow(detail.workflow.id, inputs, detail.workflow.version?.id)
      toast('Run queued', undefined, 'success')
      onStarted(res.run)
    } catch (err) {
      toast('Could not start the run', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(false)
    }
  }
  return (
    <Modal open={open} onClose={onClose} title="Run workflow" subtitle={`${detail.workflow.name} · ${versionLabel(detail.workflow.version)}`} width={600}
      footer={<><Button variant="ghost" onClick={onClose}>Cancel</Button><Button variant="primary" icon={<CirclePlay size={15} />} onClick={run} loading={busy} disabled={Object.keys(errors).length > 0}>Start run</Button></>}>
      {errorsCount > 0 && <InlineNotice tone="bad" title="This version has validation errors">Run will be rejected until the E-codes are fixed.</InlineNotice>}
      {warnings > 0 && <InlineNotice tone="warn" title={`${warnings} warning(s)`}>The run is allowed; review risky steps in the graph.</InlineNotice>}
      <InputForm schema={schema} value={inputs} onChange={setInputs} />
      <div className="run-dialog-note"><ShieldCheck size={14} /> Risky steps still pause for approval unless the workflow allows unattended work.</div>
    </Modal>
  )
}
