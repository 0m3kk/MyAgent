import { useCallback, useEffect, useMemo, useState } from 'react'
import clsx from 'clsx'
import {
  ArrowLeft,
  Braces,
  CheckCircle2,
  Code2,
  FileCode2,
  FlaskConical,
  Folder,
  FolderInput,
  KeyRound,
  Lock,
  Play,
  Plus,
  RefreshCw,
  Save,
  ShieldCheck,
  Sparkles,
  Star,
  TerminalSquare,
  Trash2,
  Wrench,
  XCircle,
} from 'lucide-react'
import { api } from '../api'
import { navigate } from '../router'
import { useApp } from '../store'
import { useToast } from '../components/toast'
import { useConfirm } from '../components/confirm'
import { useEventStream } from '../hooks'
import type { EventEnvelope, Folder as FolderRecord, SecretMeta, TestResult, Tool, ToolDetail, ToolVersion } from '../types'
import { formatDate, formatRelative, safeJSON, shortId, versionLabel } from '../format'
import { Badge, Button, EmptyState, Field, IconButton, InlineNotice, Modal, PageHeader, SectionTitle, Select, Spinner, StatusBadge, Tabs, TextArea, TextInput } from '../components/ui'
import { InputForm, schemaDefaults, validateInputs, type JSONSchema } from '../components/input-form'
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

const STARTER_MANIFEST = `schema: 1
name: text.hello
description: Return a friendly greeting. Replace this with your tool.
kind: starlark
runtime:
  entry: main.star
side_effect: none
input_schema:
  type: object
  additionalProperties: false
  required: [name]
  properties:
    name:
      type: string
      description: Person to greet
output_schema:
  type: object
  required: [message]
  properties:
    message:
      type: string
permissions:
  network: { domains: [] }
  fs: { read: [], write: [] }
  exec: []
secrets: []
limits: { timeout_s: 10, memory_mb: 64, output_kb: 64 }
tests:
  - name: greets
    input: { name: Ada }
    expect:
      - 'startsWith(output.message, "Hello")'
  - name: rejects empty input
    input: {}
    expect_error: invalid_input
`

const STARTER_CODE = `def run(input, ctx):
    return {"message": "Hello, " + input["name"] + "!"}
`

const STARTER_SCRIPT_MANIFEST = `schema: 1
name: script.example
description: Run a Python script in the subprocess sandbox.
kind: script
runtime:
  language: python
  entry: main.py
side_effect: read
input_schema:
  type: object
  additionalProperties: false
  required: [text]
  properties:
    text:
      type: string
      description: Text to process
output_schema:
  type: object
  required: [length]
  properties:
    length:
      type: integer
permissions:
  network: { domains: [] }
  fs: { read: [], write: [] }
  exec: []
secrets: []
limits: { timeout_s: 15, memory_mb: 128, output_kb: 64 }
tests:
  - name: counts characters
    input: { text: "hello" }
    expect:
      - "output.length == 5"
  - name: missing text
    input: {}
    expect_error: invalid_input
`

const STARTER_SCRIPT = `import json, sys

payload = json.load(sys.stdin)
text = payload["input"]["text"]
print(json.dumps({"length": len(text)}))
`

function toolNeedsAttention(tool: Tool) {
  const status = tool.version?.status
  return !tool.version || status === 'draft' || status === 'tested'
}

export function ToolsPage() {
  const { data, refresh } = useApp()
  const toast = useToast()
  const confirm = useConfirm()
  const [query, setQuery] = useState('')
  const [selection, setSelection] = useState<LibrarySelection>({ kind: 'all' })
  const [statusFilter, setStatusFilter] = useState('all')
  const [kindFilter, setKindFilter] = useState('all')
  const [sort, setSort] = useState<'name' | 'newest' | 'updated'>('name')
  const [folderDialog, setFolderDialog] = useState<{ mode: 'create' | 'edit'; folder?: FolderRecord } | null>(null)
  const [moveTool, setMoveTool] = useState<Tool | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [organizing, setOrganizing] = useState(false)

  const allTools = useMemo(() => data?.tools ?? [], [data?.tools])
  const folders = useMemo(() => (data?.folders ?? []).filter((f) => f.kind === 'tool'), [data?.folders])
  const counts = useMemo(() => buildLibraryCounts(folders, allTools, toolNeedsAttention), [folders, allTools])

  useEffect(() => {
    if (selection.kind === 'folder' && !folders.some((folder) => folder.id === selection.id)) {
      setSelection({ kind: 'all' })
    }
  }, [folders, selection])

  const filtered = useMemo(() => {
    const known = folderMap(folders)
    const needle = query.trim().toLowerCase()
    const selectedFolderIDs = selection.kind === 'folder' ? folderDescendantIDs(folders, selection.id) : null
    const items = allTools.filter((tool) => {
      if (selection.kind === 'pinned' && !tool.pinned) return false
      if (selection.kind === 'attention' && !toolNeedsAttention(tool)) return false
      if (selection.kind === 'unfiled' && tool.folder_id && known.has(tool.folder_id)) return false
      if (selectedFolderIDs && (!tool.folder_id || !selectedFolderIDs.has(tool.folder_id))) return false
      if (needle && !`${tool.name} ${tool.description || ''}`.toLowerCase().includes(needle)) return false
      const status = tool.version?.status || ''
      if (statusFilter === 'active' && status !== 'active' && status !== 'approved') return false
      if (statusFilter === 'draft' && status !== 'draft' && status !== 'tested') return false
      if (kindFilter !== 'all' && tool.kind !== kindFilter) return false
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
  }, [allTools, folders, selection, query, statusFilter, kindFilter, sort])

  const groups = useMemo(() => {
    const known = folderMap(folders)
    if (selection.kind !== 'all' || query.trim()) {
      return [{ key: 'view', title: selectionTitle(selection, folders, query), items: filtered }]
    }
    const out: { key: string; title: string; items: Tool[] }[] = []
    for (const folder of topLevelFolders(folders)) {
      const IDs = folderDescendantIDs(folders, folder.id)
      const items = filtered.filter((tool) => tool.folder_id && IDs.has(tool.folder_id))
      if (items.length) out.push({ key: folder.id, title: folder.name, items })
    }
    const unfiled = filtered.filter((tool) => !tool.folder_id || !known.has(tool.folder_id))
    if (unfiled.length) out.push({ key: 'unfiled', title: 'Unfiled', items: unfiled })
    return out
  }, [filtered, folders, selection, query])

  if (!data) return null

  const title = selection.kind === 'folder' ? folderPath(folders, selection.id) : selectionTitle(selection, folders, query)
  const filtersActive = !!query || statusFilter !== 'all' || kindFilter !== 'all'
  const emptyCopy = toolEmptyState(selection, query)

  async function saveFolder(name: string, parentID: string) {
    try {
      if (folderDialog?.mode === 'edit' && folderDialog.folder) {
        await api.updateFolder(folderDialog.folder.id, { name, parent_id: parentID })
        await refresh()
        toast('Folder updated', name, 'success')
      } else {
        const folder = await api.createFolder({ kind: 'tool', name, parent_id: parentID || undefined })
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
      message: `Delete “${folder.name}”? Nested folders are removed and their tools move to the parent folder. The tools themselves are not deleted.`,
      confirmLabel: 'Delete folder',
    })
    if (!ok) return
    try {
      await api.deleteFolder(folder.id)
      if (selection.kind === 'folder' && IDs.has(selection.id)) setSelection({ kind: 'all' })
      toast('Folder deleted', 'Its tools were moved to the parent folder.', 'success')
      await refresh()
    } catch (err) {
      toast('Could not delete the folder', err instanceof Error ? err.message : undefined, 'error')
    }
  }

  async function togglePin(tool: Tool) {
    setBusy(`pin-${tool.id}`)
    try {
      await api.updateTool(tool.id, { pinned: !tool.pinned })
      await refresh()
      toast(tool.pinned ? 'Tool unpinned' : 'Tool pinned', tool.name, 'success')
    } catch (err) {
      toast('Could not update the tool', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(null)
    }
  }

  async function moveToolTo(tool: Tool, folderID: string) {
    try {
      await api.updateTool(tool.id, { folder_id: folderID })
      await refresh()
      toast('Tool moved', folderID ? folderPath(folders, folderID) : 'No folder', 'success')
      return true
    } catch (err) {
      toast('Could not move the tool', err instanceof Error ? err.message : undefined, 'error')
      return false
    }
  }

  async function organizeWithAgent() {
    if (!data?.model_profiles.length) {
      toast('Add a model first', 'Settings → Models is required before the agent can organize the library.', 'error')
      return
    }
    setOrganizing(true)
    try {
      const result = await api.organizeLibrary('tool')
      await refresh()
      setSelection({ kind: 'all' })
      const parts: string[] = []
      if (result.created_folders.length) {
        parts.push(`${result.created_folders.length} new folder${result.created_folders.length === 1 ? '' : 's'}`)
      }
      parts.push(`${result.moved_items} moved`)
      if (result.unchanged_items) parts.push(`${result.unchanged_items} unchanged`)
      toast(
        result.moved_items || result.created_folders.length ? 'Agent organized your tools' : 'Library already looks tidy',
        parts.join(' · '),
        'success',
      )
    } catch (err) {
      toast('Could not organize tools', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setOrganizing(false)
    }
  }

  return (
    <div className="page">
      <PageHeader
        eyebrow="Capabilities"
        title="Tools"
        description="Built-ins, approved Starlark/script tools and the manifests the agent can use. Group them into folders, pin the ones you use often, and test everything."
        actions={
          <>
            <div className="search-box"><Wrench size={14} /><input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Filter tools…" /></div>
            <Button variant="primary" icon={<Plus size={15} />} onClick={() => navigate('/tools/new')}>New tool</Button>
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
          onOrganize={() => void organizeWithAgent()}
          organizing={organizing}
        />
        <div className="library-main">
          <div className="library-toolbar">
            <div className="library-title">
              <h2>{title}</h2>
              <span>{filtered.length === allTools.length ? `${allTools.length} tools` : `${filtered.length} of ${allTools.length} tools`}</span>
            </div>
            <div className="library-filters">
              <Select
                className="compact"
                value={statusFilter}
                onChange={setStatusFilter}
                aria-label="Filter by status"
                options={[
                  { value: 'all', label: 'Any status' },
                  { value: 'active', label: 'Active' },
                  { value: 'draft', label: 'Drafts & tested' },
                ]}
              />
              <Select
                className="compact"
                value={kindFilter}
                onChange={setKindFilter}
                aria-label="Filter by kind"
                options={[
                  { value: 'all', label: 'Any kind' },
                  { value: 'builtin', label: 'Built-in' },
                  { value: 'starlark', label: 'Starlark' },
                  { value: 'script', label: 'Script' },
                ]}
              />
              <Select
                className="compact"
                value={sort}
                onChange={(next) => setSort(next as typeof sort)}
                aria-label="Sort tools"
                options={[
                  { value: 'name', label: 'Name' },
                  { value: 'newest', label: 'Newest' },
                  { value: 'updated', label: 'Recently updated' },
                ]}
              />
              {filtersActive && (
                <button className="link-btn tiny" onClick={() => { setQuery(''); setStatusFilter('all'); setKindFilter('all') }}>Reset</button>
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
              <div className="tool-grid">
                {group.items.map((t) => (
                  <ToolTile
                    key={t.id}
                    tool={t}
                    folderLabel={folderPath(folders, t.folder_id)}
                    pinning={busy === `pin-${t.id}`}
                    onPin={() => void togglePin(t)}
                    onMove={() => setMoveTool(t)}
                  />
                ))}
              </div>
            </section>
          ))}

          {filtered.length === 0 && (
            <EmptyState
              icon={<Wrench size={28} />}
              title={emptyCopy.title}
              description={emptyCopy.description}
              action={filtersActive
                ? <Button variant="subtle" onClick={() => { setQuery(''); setStatusFilter('all'); setKindFilter('all') }}>Clear filters</Button>
                : <Button variant="primary" icon={<Plus size={15} />} onClick={() => navigate('/tools/new')}>New tool</Button>}
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
        open={!!moveTool}
        onClose={() => setMoveTool(null)}
        itemName={moveTool?.name || ''}
        folders={folders}
        currentFolderID={moveTool?.folder_id}
        onMove={(folderID) => moveTool ? moveToolTo(moveTool, folderID) : Promise.resolve(false)}
      />
    </div>
  )
}

function selectionTitle(selection: LibrarySelection, folders: FolderRecord[], query: string) {
  if (query.trim()) return `Results for “${query.trim()}”`
  if (selection.kind === 'pinned') return 'Pinned tools'
  if (selection.kind === 'attention') return 'Needs attention'
  if (selection.kind === 'unfiled') return 'Unfiled'
  if (selection.kind === 'folder') return folderPath(folders, selection.id) || 'Folder'
  return 'All tools'
}

function toolEmptyState(selection: LibrarySelection, query: string) {
  if (query.trim()) return { title: 'No matching tools', description: 'Try another name or clear the current filters.' }
  if (selection.kind === 'pinned') return { title: 'Nothing pinned yet', description: 'Pin tools you use often and they will stay at the top of every list.' }
  if (selection.kind === 'attention') return { title: 'Nothing needs attention', description: 'Every tool is tested, approved and ready for the agent.' }
  if (selection.kind === 'unfiled') return { title: 'No unfiled tools', description: 'Every tool belongs to a folder. Create another folder to keep the library tidy.' }
  if (selection.kind === 'folder') return { title: 'This folder is empty', description: 'Move a tool here with the folder button on any tool card, or create something new.' }
  return { title: 'No tools yet', description: 'Ask the agent to create one, or start from a manifest.' }
}

function ToolTile({
  tool,
  folderLabel,
  pinning,
  onPin,
  onMove,
}: {
  tool: Tool
  folderLabel: string
  pinning: boolean
  onPin: () => void
  onMove: () => void
}) {
  const v = tool.version
  return (
    <div className={clsx('tool-tile', tool.pinned && 'pinned')}>
      <div className="tool-tile-head">
        <span className={clsx('tool-kind-icon', `kind-${tool.kind}`)}>
          {tool.kind === 'builtin' ? <Lock size={16} /> : tool.kind === 'starlark' ? <Sparkles size={16} /> : <FileCode2 size={16} />}
        </span>
        <div className="tool-tile-title">
          <button className="tool-tile-open" onClick={() => navigate(`/tools/${tool.id}`)}>{tool.name}</button>
          <span>{tool.kind}</span>
        </div>
        {v && <StatusBadge status={v.status} />}
        {tool.draft_version_id && <Badge tone="warn">draft</Badge>}
      </div>
      <p>{tool.description || 'No description'}</p>
      <div className="tool-tile-foot">
        <Badge tone={tool.side_effect === 'write' || tool.side_effect === 'external' ? 'warn' : 'neutral'}>{tool.side_effect || 'none'}</Badge>
        {folderLabel && <span className="folder-chip" title={folderLabel}><Folder size={11} />{folderLabel}</span>}
        <span className="tool-tile-foot-end">
          {v && v.status !== 'draft' && v.status !== 'tested' && <span className="muted tiny">{versionLabel(v)}</span>}
          <span className="muted tiny">{Array.isArray(tool.tests) ? `${tool.tests.length} tests` : 'tests ?'}</span>
          <IconButton className="tile-icon-btn" label={`Move ${tool.name} to folder`} onClick={(e) => { e.stopPropagation(); onMove() }}><FolderInput size={12} /></IconButton>
          <IconButton className={clsx('tile-icon-btn', tool.pinned && 'active')} label={tool.pinned ? `Unpin ${tool.name}` : `Pin ${tool.name}`} onClick={(e) => { e.stopPropagation(); onPin() }} disabled={pinning}>
            <Star size={12} fill={tool.pinned ? 'currentColor' : 'none'} />
          </IconButton>
        </span>
      </div>
    </div>
  )
}

export function ToolPage({ toolId }: { toolId: string }) {
  if (toolId === 'new') return <NewToolPage />
  return <ToolDetailPage toolId={toolId} />
}

function NewToolPage() {
  const toast = useToast()
  const { refresh } = useApp()
  const [manifest, setManifest] = useState(STARTER_MANIFEST)
  const [code, setCode] = useState(STARTER_CODE)
  const [template, setTemplate] = useState('starlark')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  function applyTemplate(t: string) {
    setTemplate(t)
    if (t === 'starlark') { setManifest(STARTER_MANIFEST); setCode(STARTER_CODE) }
    if (t === 'script') { setManifest(STARTER_SCRIPT_MANIFEST); setCode(STARTER_SCRIPT) }
  }

  async function create() {
    setBusy(true)
    setError(null)
    try {
      const entry = template === 'script' ? 'main.py' : 'main.star'
      const res = await api.createTool(manifest, { [entry]: code })
      toast('Tool draft created', 'Run the tests, then ask the user to approve it.', 'success')
      void refresh()
      navigate(`/tools/${res.result.tool.id}`)
    } catch (err) {
      const e = err as Error & { details?: { issues?: { path: string; message: string }[] } }
      setError(e.message)
      if (e.details?.issues) setError(e.details.issues.map((i) => `${i.path}: ${i.message}`).join('\n'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="tool-page new-tool-page">
      <header className="workflow-head">
        <div className="workflow-head-main">
          <button className="back-link" onClick={() => navigate('/tools')}><ArrowLeft size={14} /> Tools</button>
          <div className="workflow-title-row">
            <span className="workflow-icon"><Wrench size={20} /></span>
            <div><h1>New tool</h1><div className="workflow-subline"><span>draft</span><span className="dot-sep" /><span>created by you</span></div></div>
          </div>
        </div>
        <div className="workflow-actions">
          <Select
            value={template}
            onChange={applyTemplate}
            aria-label="Tool template"
            options={[
              { value: 'starlark', label: 'Starlark template' },
              { value: 'script', label: 'Python script template' },
            ]}
          />
          <Button variant="primary" icon={<Save size={15} />} onClick={create} loading={busy}>Create draft</Button>
        </div>
      </header>
      {error && <InlineNotice tone="bad" title="Could not create the tool"><pre className="inline-pre">{error}</pre></InlineNotice>}
      <div className="new-tool-grid">
        <div className="editor-panel">
          <div className="editor-panel-head"><span><Braces size={14} /> manifest.yaml</span><span className="muted tiny">strict fields</span></div>
          <textarea className="mono-editor" value={manifest} onChange={(e) => setManifest(e.target.value)} spellCheck={false} />
        </div>
        <div className="editor-panel">
          <div className="editor-panel-head"><span><Code2 size={14} /> {template === 'script' ? 'main.py' : 'main.star'}</span><span className="muted tiny">{template === 'script' ? 'stdin JSON → stdout JSON' : 'def run(input, ctx)'}</span></div>
          <textarea className="mono-editor" value={code} onChange={(e) => setCode(e.target.value)} spellCheck={false} />
        </div>
      </div>
      <div className="manifest-tip">
        <ShieldCheck size={15} />
        <span>Tools start as drafts. They can only run after tests pass and you approve the card. Network domains, file roots and secrets are declared in the manifest.</span>
      </div>
    </div>
  )
}

function ToolDetailPage({ toolId }: { toolId: string }) {
  const toast = useToast()
  const confirm = useConfirm()
  const { refresh, refreshApprovals, data } = useApp()
  const [detail, setDetail] = useState<ToolDetail | null>(null)
  const [secrets, setSecrets] = useState<SecretMeta[]>([])
  const [loading, setLoading] = useState(true)
  const [tab, setTab] = useState<'overview' | 'code' | 'tests' | 'versions'>('overview')
  const [busy, setBusy] = useState<string | null>(null)
  const [tests, setTests] = useState<TestResult[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const [d, s] = await Promise.all([api.tool(toolId), api.secrets()])
      setDetail(d)
      setSecrets(s.secrets)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not load the tool')
    } finally {
      setLoading(false)
    }
  }, [toolId])

  useEffect(() => { setLoading(true); void load() }, [load])

  if (loading) return <div className="page center-page"><Spinner size={26} /></div>
  if (error || !detail) return <div className="page"><EmptyState icon={<Wrench size={26} />} title="Tool not found" description={error || undefined} action={<Button onClick={() => navigate('/tools')}>Back to tools</Button>} /></div>

  const tool = detail.tool
  const version = tool.version
  const current = detail.versions.find((v) => v.id === version?.id)
  const status = current?.status || version?.status
  const editable = status === 'draft' || status === 'tested'
  const manifest = parseManifestLite(detail.manifest_yaml)
  const declaredTests = Array.isArray(tool.tests) ? tool.tests : []
  const secretDecls = (Array.isArray(tool.secrets) ? tool.secrets : []) as { name: string; description?: string; inject?: string; required?: boolean }[]
  const permissions = (tool.permissions || {}) as Record<string, any>
  const testsCount = declaredTests.length
  const inputSchema = tool.input_schema as JSONSchema | undefined
  const folderLabel = folderPath(data?.folders || [], tool.folder_id)

  async function runTests() {
    if (!current) return
    setBusy('test')
    try {
      const res = await api.testTool(tool.id, current.id)
      setTests(res.tests)
      setTab('tests')
      toast(res.passed ? 'All tests passed' : 'Some tests failed', res.passed ? 'The version is marked tested.' : 'Fix the failures and run again.', res.passed ? 'success' : 'error')
      await load()
      void refresh()
    } catch (err) {
      toast('Could not run tests', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(null)
    }
  }

  async function approve() {
    if (!current) return
    const ok = await confirm({
      title: 'Approve and activate tool',
      message: `Approve and activate ${tool.name}? It becomes available to the agent and workflows.`,
      confirmLabel: 'Approve tool',
      tone: 'primary',
    })
    if (!ok) return
    setBusy('approve')
    try {
      await api.approveTool(tool.id, current.id)
      toast('Tool approved', 'It is now available to the agent and workflows.', 'success')
      await load()
      void refresh()
      void refreshApprovals()
    } catch (err) {
      toast('Could not approve the tool', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(null)
    }
  }

  async function del() {
    const ok = await confirm({
      title: 'Delete tool',
      message: `Delete ${tool.name} and all of its versions?`,
      confirmLabel: 'Delete tool',
    })
    if (!ok) return
    try {
      await api.deleteTool(tool.id)
      toast('Tool deleted', undefined, 'success')
      void refresh()
      navigate('/tools')
    } catch (err) {
      toast('Could not delete the tool', err instanceof Error ? err.message : undefined, 'error')
    }
  }

  async function bindSecret(logical: string, secretId: string) {
    setBusy('secret-' + logical)
    try {
      await api.bindSecret(tool.id, logical, secretId)
      toast(`Bound ${logical}`, undefined, 'success')
      await load()
    } catch (err) {
      toast('Could not bind secret', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="tool-page">
      <header className="workflow-head">
        <div className="workflow-head-main">
          <button className="back-link" onClick={() => navigate('/tools')}><ArrowLeft size={14} /> Tools</button>
          <div className="workflow-title-row">
            <span className={clsx('workflow-icon', `kind-${tool.kind}`)}>{tool.kind === 'builtin' ? <Lock size={20} /> : <Wrench size={20} />}</span>
            <div>
              <h1>{tool.name}</h1>
              <div className="workflow-subline">
                <Badge>{tool.kind}</Badge>
                <StatusBadge status={status} />
                {version && status !== 'draft' && status !== 'tested' && <span>{versionLabel(version)}</span>}
                <span className="dot-sep" />
                <Badge tone={tool.side_effect === 'write' || tool.side_effect === 'external' ? 'warn' : 'neutral'}>{tool.side_effect || 'none'}</Badge>
                {folderLabel && <span className="folder-inline"><Folder size={12} />{folderLabel}</span>}
                {version && <span className="muted tiny">{shortId(version.content_hash)}</span>}
              </div>
            </div>
          </div>
        </div>
        <div className="workflow-actions">
          {testsCount > 0 && <Button variant="subtle" size="sm" icon={<FlaskConical size={14} />} onClick={runTests} loading={busy === 'test'}>Run tests</Button>}
          {status === 'tested' && <Button variant="primary" size="sm" icon={<ShieldCheck size={14} />} onClick={approve} loading={busy === 'approve'}>Approve</Button>}
          {status === 'draft' && <Badge tone="warn">run tests to approve</Badge>}
          <Button variant="ghost" size="sm" icon={<Trash2 size={14} />} onClick={del}>Delete</Button>
        </div>
      </header>

      <div className="workflow-tabs-row">
        <Tabs
          tabs={[
            { id: 'overview', label: <><Braces size={13} /> Overview</> },
            { id: 'code', label: <><Code2 size={13} /> Code</> },
            { id: 'tests', label: <><FlaskConical size={13} /> Tests {testsCount ? <span className="tab-count">{testsCount}</span> : null}</> },
            { id: 'versions', label: <><RefreshCw size={13} /> Versions {detail.versions.length ? <span className="tab-count">{detail.versions.length}</span> : null}</> },
          ]}
          value={tab}
          onChange={setTab}
        />
      </div>

      {tab === 'overview' && (
        <div className="tool-overview">
          <div className="tool-overview-main">
            <div className="card">
              <p className="tool-description">{tool.description}</p>
              <div className="tool-stats">
                <div><strong>{manifest?.limits?.timeout_s || '—'}s</strong><span>timeout</span></div>
                <div><strong>{manifest?.limits?.output_kb || '—'} KB</strong><span>output cap</span></div>
                <div><strong>{current ? formatDate(current.created_at) : '—'}</strong><span>created</span></div>
                <div><strong>{current?.created_by || '—'}</strong><span>author</span></div>
              </div>
            </div>

            <SchemaCard title="Input schema" schema={tool.input_schema} />
            <SchemaCard title="Output schema" schema={tool.output_schema} />

            <div className="card">
              <SectionTitle hint="Applied by the sandbox and proxy, not just declared.">Permissions</SectionTitle>
              <PermissionsView permissions={permissions} />
            </div>

            <SecretBindings
              decls={secretDecls}
              bindings={detail.bindings}
              secrets={secrets}
              busy={busy}
              onBind={bindSecret}
              onCreate={() => navigate('/settings/secrets/new')}
            />

            <ToolRunnerCard tool={tool} version={current} schema={inputSchema} />
          </div>
          <aside className="tool-overview-side">
            <div className="card">
              <SectionTitle>Version</SectionTitle>
              <div className="version-stack">
                {detail.versions.map((v) => (
                  <div key={v.id} className={clsx('version-mini', v.id === current?.id && 'current')}>
                    <span className="mono">{versionLabel(v)}</span>
                    <StatusBadge status={v.status} />
                    <span className="muted tiny">{v.created_by}</span>
                  </div>
                ))}
              </div>
            </div>
            <div className="card">
              <SectionTitle>Safety</SectionTitle>
              <ul className="check-list">
                <li><CheckCircle2 size={13} /> Runs in a process sandbox</li>
                <li><CheckCircle2 size={13} /> Network is deny-by-default</li>
                <li><CheckCircle2 size={13} /> Secrets are injected at runtime</li>
                <li><CheckCircle2 size={13} /> Output and logs are redacted</li>
                <li><CheckCircle2 size={13} /> Approval is tied to content hash</li>
              </ul>
            </div>
          </aside>
        </div>
      )}

      {tab === 'code' && <CodeTab tool={tool} detail={detail} editable={editable} onChanged={() => { void load(); void refresh() }} />}
      {tab === 'tests' && <TestsTab tests={tests} manifest={{ tests: declaredTests }} onRun={runTests} busy={busy === 'test'} />}
      {tab === 'versions' && <ToolVersionsTab detail={detail} onApprove={approve} />}
    </div>
  )
}

function parseManifestLite(yaml: string): Record<string, any> | null {
  // The server owns full parsing and diagnostics. This tiny reader only feeds
  // presentation (permissions, tests, limits) and never validates execution.
  try {
    const out: Record<string, any> = {}
    const tests = yaml.match(/^tests:\s*$/m)
    out.hasTests = !!tests
    const testsBlock = yaml.split(/^tests:\s*$/m)[1]
    if (testsBlock) out.tests = Array.from(testsBlock.matchAll(/^\s{2,}- name:/gm))
    const timeout = yaml.match(/timeout_s:\s*(\d+)/)
    if (timeout) out.limits = { ...(out.limits || {}), timeout_s: Number(timeout[1]) }
    const output = yaml.match(/output_kb:\s*(\d+)/)
    if (output) out.limits = { ...(out.limits || {}), output_kb: Number(output[1]) }
    const side = yaml.match(/^side_effect:\s*(\w+)/m)
    if (side) out.side_effect = side[1]
    const kind = yaml.match(/^kind:\s*(\w+)/m)
    if (kind) out.kind = kind[1]
    const permBlock = yaml.split(/^permissions:\s*$/m)[1]?.split(/^\w/m)[0] || ''
    out.permissions = {
      network: { domains: Array.from(permBlock.matchAll(/domains:\s*\[([^\]]*)\]/g)).flatMap((m) => m[1].split(',').map((s) => s.trim()).filter(Boolean)) },
      raw: permBlock,
    }
    const secretsBlock = yaml.split(/^secrets:\s*$/m)[1]
    const secrets: { name: string; description: string; inject: string; required: boolean }[] = []
    if (secretsBlock) {
      for (const m of secretsBlock.matchAll(/^-\s+name:\s*(\S+)/gm)) {
        const name = m[1]
        if (name === '[]') continue
        secrets.push({ name, description: '', inject: 'env', required: true })
      }
    }
    out.secrets = secrets
    return out
  } catch {
    return null
  }
}

function SchemaCard({ title, schema }: { title: string; schema?: Record<string, unknown> }) {
  const s = (schema || {}) as JSONSchema
  const props = Object.entries(s.properties || {})
  const required = s.required || []
  return (
    <div className="card">
      <SectionTitle hint="Every field needs a description; the agent uses it to fill arguments.">{title}</SectionTitle>
      {props.length === 0 ? <div className="muted small">No fields.</div> : (
        <div className="schema-table">
          <div className="schema-head"><span>Field</span><span>Type</span><span>Description</span></div>
          {props.map(([name, p]) => (
            <div key={name} className="schema-row">
              <span className="mono">{name}{required.includes(name) && <em className="required">*</em>}</span>
              <span><Badge>{p.type || 'any'}</Badge>{p.default !== undefined && <span className="muted tiny"> = {String(p.default)}</span>}</span>
              <span className="muted">{p.description || '—'}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function PermissionsView({ permissions }: { permissions?: Record<string, any> }) {
  const network = permissions?.network?.domains || []
  const raw = permissions?.raw || ''
  const read = /read:\s*\[([^\]]*)\]/.exec(raw)?.[1] || ''
  const write = /write:\s*\[([^\]]*)\]/.exec(raw)?.[1] || ''
  const exec = /exec:\s*\[([^\]]*)\]/.exec(raw)?.[1] || ''
  return (
    <div className="permission-grid">
      <div className="permission-item"><GlobeIcon /><span>Network</span><code>{network.length ? network.join(', ') : 'none'}</code></div>
      <div className="permission-item"><Lock size={13} /><span>Read</span><code>{read || 'none'}</code></div>
      <div className="permission-item"><Save size={13} /><span>Write</span><code>{write || 'none'}</code></div>
      <div className="permission-item"><TerminalSquare size={13} /><span>Exec</span><code>{exec || 'none'}</code></div>
    </div>
  )
}

function GlobeIcon() {
  return <span className="globe-icon">◎</span>
}

function SecretBindings({
  decls,
  bindings,
  secrets,
  busy,
  onBind,
  onCreate,
}: {
  decls: { name: string; description?: string; inject?: string; required?: boolean }[]
  bindings: ToolDetail['bindings']
  secrets: SecretMeta[]
  busy: string | null
  onBind: (logical: string, id: string) => void
  onCreate: () => void
}) {
  if (!decls?.length) return null
  return (
    <div className="card">
      <SectionTitle hint="The agent sees names and status only. Values are injected at runtime and never enter prompts.">Secrets</SectionTitle>
      <div className="binding-list">
        {decls.map((d) => {
          const bound = bindings.find((b) => b.logical_name === d.name)
          const secret = secrets.find((s) => s.id === bound?.secret_id)
          return (
            <div key={d.name} className="binding-row">
              <span className="binding-icon"><KeyRound size={14} /></span>
              <div className="binding-copy">
                <strong>{d.name} {d.required && <em className="required">*</em>}</strong>
                <span>{d.description || d.inject || 'env'} · inject: {d.inject || 'env'}</span>
              </div>
              <Select
                className="compact"
                value={bound?.secret_id || ''}
                onChange={(secretID) => secretID && onBind(d.name, secretID)}
                disabled={busy === 'secret-' + d.name}
                placeholder={bound ? 'Bound' : 'Not bound'}
                aria-label={`Bind ${d.name}`}
                options={secrets.map((s) => ({ value: s.id, label: `${s.name} (${s.status})` }))}
              />
              {secret ? <Badge tone="ok">{secret.status}</Badge> : <Badge tone="warn">missing</Badge>}
            </div>
          )
        })}
      </div>
      {secrets.length === 0 && <div className="binding-foot">No secrets stored. <button className="link-btn" onClick={onCreate}>Add one in Settings</button></div>}
    </div>
  )
}

function ToolRunnerCard({ tool, version, schema }: { tool: Tool; version?: ToolVersion; schema?: JSONSchema }) {
  const toast = useToast()
  const [inputs, setInputs] = useState<Record<string, unknown>>(() => schemaDefaults(schema))
  const [busy, setBusy] = useState(false)
  const [runId, setRunId] = useState<string | null>(null)
  const [events, setEvents] = useState<EventEnvelope[]>([])
  const errors = validateInputs(schema, inputs)
  const logs = events.filter((e) => e.type === 'step.log').map((e) => `${new Date(e.ts).toLocaleTimeString()}  ${String(e.payload?.stream || 'log')}  ${String(e.payload?.text || '')}`)
  const finished = [...events].reverse().find((e) => e.type === 'run.finished')
  const result = events.filter((e) => e.type === 'step.finished').at(-1)?.payload?.output

  useEventStream(runId ? `run:${runId}` : null, (e) => setEvents((prev) => prev.some((x) => x.id && x.id === e.id) ? prev : [...prev, e]))

  async function run() {
    if (!version) return
    setBusy(true)
    setEvents([])
    setRunId(null)
    try {
      const res = await api.runTool(tool.id, inputs, version.id)
      setRunId(res.run.id)
      toast('Tool run started', 'Logs stream into the panel.', 'success')
    } catch (err) {
      toast('Could not run the tool', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="card runner-card">
      <SectionTitle hint="Manual runs are explicit: no extra approval card. Secrets are still injected at the boundary.">Run this tool</SectionTitle>
      <InputForm schema={schema} value={inputs} onChange={setInputs} />
      <div className="runner-actions">
        <Button variant="primary" size="sm" icon={<Play size={14} />} onClick={run} loading={busy} disabled={Object.keys(errors).length > 0 || !version}>Run tool</Button>
        {runId && <button className="link-btn" onClick={() => navigate(`/runs/${runId}`)}>Open run <span className="mono">{shortId(runId)}</span></button>}
      </div>
      {runId && (
        <div className="inline-run">
          <div className="terminal-head"><span><TerminalSquare size={13} /> Terminal</span><StatusBadge status={finished ? String(finished.payload?.status || 'succeeded') : 'running'} /></div>
          <pre className="terminal-block">{logs.length ? logs.join('\n') : 'Waiting for output…'}</pre>
          {result !== undefined && <pre className="json-block">{safeJSON(result, 12000)}</pre>}
          {finished && finished.payload?.error ? <pre className="error-block">{String(finished.payload.error)}</pre> : null}
        </div>
      )}
    </div>
  )
}

function CodeTab({ tool, detail, editable, onChanged }: { tool: Tool; detail: ToolDetail; editable: boolean; onChanged: () => void }) {
  const toast = useToast()
  const version = detail.tool.version
  const [file, setFile] = useState(detail.files[0]?.path || 'manifest.yaml')
  const [content, setContent] = useState('')
  const [dirty, setDirty] = useState(false)
  const [busy, setBusy] = useState(false)
  const [newVersionOpen, setNewVersionOpen] = useState(false)
  const [newManifest, setNewManifest] = useState(detail.manifest_yaml)

  const loadFile = useCallback(async (path: string) => {
    if (!version) return
    setFile(path)
    setDirty(false)
    if (path === 'manifest.yaml') {
      setContent(detail.manifest_yaml)
      return
    }
    try {
      const res = await api.readToolFile(tool.id, version.id, path)
      setContent(res.content)
    } catch (err) {
      toast('Could not read file', err instanceof Error ? err.message : undefined, 'error')
    }
  }, [tool.id, version, detail.manifest_yaml, toast])

  useEffect(() => { void loadFile(file) }, [file, loadFile])

  async function save() {
    if (!version) return
    setBusy(true)
    try {
      await api.writeToolFile(tool.id, version.id, file, content)
      setDirty(false)
      toast('File saved', 'Content hash updated. Run tests again before approval.', 'success')
      onChanged()
    } catch (err) {
      toast('Could not save the file', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(false)
    }
  }

  async function createVersion() {
    setBusy(true)
    try {
      const res = await api.createTool(newManifest)
      toast(editable ? 'Draft updated' : 'New draft version created', undefined, 'success')
      setNewVersionOpen(false)
      onChanged()
      navigate(`/tools/${res.result.tool.id}`)
    } catch (err) {
      toast('Could not create a new version', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(false)
    }
  }

  const fileMap = new Map(detail.files.map((f) => [f.path, f]))
  fileMap.set('manifest.yaml', { path: 'manifest.yaml', size: detail.manifest_yaml.length })
  const files = [...fileMap.values()]
  return (
    <div className="code-workspace">
      <aside className="code-files">
        <div className="code-files-head"><span>Files</span><Badge tone={editable ? 'warn' : 'muted'}>{editable ? 'editable' : 'frozen'}</Badge></div>
        {files.map((f) => (
          <button key={f.path} className={clsx('code-file', file === f.path && 'active')} onClick={() => loadFile(f.path)}>
            <FileCode2 size={13} />
            <span>{f.path}</span>
            <em>{Math.ceil(f.size / 1024)}k</em>
          </button>
        ))}
        <div className="code-files-actions">
          <Button size="sm" variant="subtle" icon={<Plus size={13} />} onClick={() => setNewVersionOpen(true)}>{editable ? 'Draft manifest' : 'New version'}</Button>
        </div>
      </aside>
      <div className="code-main">
        <div className="code-toolbar">
          <span><Code2 size={14} /> {file} {dirty && <span className="dirty-dot" />}</span>
          <div className="toolbar-actions">
            {editable ? <Button size="sm" variant="primary" icon={<Save size={13} />} onClick={save} loading={busy} disabled={!dirty}>Save file</Button> : <Badge tone="warn">Create a new version to edit</Badge>}
          </div>
        </div>
        <textarea className="mono-editor code-editor-area" value={content} onChange={(e) => { setContent(e.target.value); setDirty(true) }} readOnly={!editable} spellCheck={false} />
      </div>
      <Modal
        open={newVersionOpen}
        onClose={() => setNewVersionOpen(false)}
        title={editable ? 'Edit draft manifest' : 'New draft version'}
        subtitle={editable
          ? 'Changes update the working draft. Tests must pass again before approval.'
          : 'Existing files are copied into the new draft. Approval is required to activate it.'}
        width={760}
        footer={(
          <>
            <Button variant="ghost" onClick={() => setNewVersionOpen(false)}>Cancel</Button>
            <Button variant="primary" onClick={createVersion} loading={busy}>{editable ? 'Save draft' : 'Create draft'}</Button>
          </>
        )}
      >
        <Field label="Manifest YAML" hint="Manifest changes update the content hash and require re-approval.">
          <TextArea rows={20} className="mono" value={newManifest} onChange={(e) => setNewManifest(e.target.value)} />
        </Field>
      </Modal>
    </div>
  )
}

function TestsTab({ tests, manifest, onRun, busy }: { tests: TestResult[] | null; manifest: Record<string, any> | null; onRun: () => void; busy: boolean }) {
  const declared = Array.isArray(manifest?.tests) ? manifest!.tests : []
  const hasTests = declared.length > 0
  return (
    <div className="tests-tab">
      <div className="tests-head">
        <div><h2>Tests</h2><p>Tests run in the sandbox with network mocks and dummy secrets. A version becomes <strong>tested</strong> only when every test passes.</p></div>
        <Button variant="primary" size="sm" icon={<FlaskConical size={14} />} onClick={onRun} loading={busy} disabled={!hasTests}>Run tests</Button>
      </div>
      {!hasTests && <InlineNotice tone="warn" title="No tests declared">Add a passing test and an invalid-input test to the manifest before asking for approval.</InlineNotice>}
      <div className="test-list">
        {(tests || []).map((t, i) => (
          <div key={`${t.name}-${i}`} className={clsx('test-row', t.passed ? 'pass' : 'fail')}>
            <span className="test-status">{t.passed ? <CheckCircle2 size={16} /> : <XCircle size={16} />}</span>
            <div className="test-copy">
              <strong>{t.name}</strong>
              <span>{t.duration_ms}ms · {t.passed ? 'passed' : 'failed'}</span>
              {t.error && <pre className="error-block small">{t.error}</pre>}
              {t.logs && t.logs.length > 0 && <pre className="terminal-block small">{t.logs.join('\n')}</pre>}
              {t.output !== undefined && <pre className="json-block small">{safeJSON(t.output, 5000)}</pre>}
            </div>
          </div>
        ))}
        {!tests && hasTests && (
          <div className="test-pending">
            {declared.map((t: { name?: string }, i: number) => <div key={i} className="test-declared"><FlaskConical size={13} /> {t.name || `Test ${i + 1}`}<span className="muted tiny">not run</span></div>)}
          </div>
        )}
      </div>
    </div>
  )
}

function ToolVersionsTab({ detail, onApprove }: { detail: ToolDetail; onApprove: () => void }) {
  return (
    <div className="versions-tab">
      <div className="versions-table">
        <div className="versions-head"><span>Version</span><span>Status</span><span>Author</span><span>Hash</span><span>Created</span><span /></div>
        {detail.versions.map((v) => (
          <div key={v.id} className="versions-row">
            <span className="mono">{versionLabel(v)}</span>
            <StatusBadge status={v.status} />
            <span>{v.created_by}</span>
            <span className="mono tiny">{shortId(v.content_hash)}</span>
            <span className="muted tiny">{formatDate(v.created_at)}</span>
            <div className="versions-actions">
              {v.id === detail.tool.version?.id && <Badge tone="neutral">current</Badge>}
              {v.status === 'tested' && v.id === detail.tool.version?.id && <Button size="sm" variant="subtle" onClick={onApprove}>Approve</Button>}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
