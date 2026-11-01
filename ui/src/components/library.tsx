import { useEffect, useMemo, useState, type ReactNode } from 'react'
import {
  ChevronDown,
  ChevronRight,
  CircleAlert,
  Folder as FolderIcon,
  FolderPlus,
  Inbox,
  Layers,
  Loader2,
  Pencil,
  Sparkles,
  Star,
  Trash2,
} from 'lucide-react'
import clsx from 'clsx'
import type { Folder } from '../types'
import { Button, Field, IconButton, Modal, Select, TextInput } from './ui'

export type LibrarySelection =
  | { kind: 'all' | 'pinned' | 'attention' | 'unfiled' }
  | { kind: 'folder'; id: string }

export interface LibraryCounts {
  all: number
  pinned: number
  attention: number
  unfiled: number
  byFolder: Record<string, number>
}

export function folderMap(folders: Folder[]) {
  return new Map(folders.map((folder) => [folder.id, folder]))
}

export function folderDescendantIDs(folders: Folder[], id: string) {
  const children = new Map<string, string[]>()
  for (const folder of folders) {
    if (!folder.parent_id) continue
    const list = children.get(folder.parent_id) || []
    list.push(folder.id)
    children.set(folder.parent_id, list)
  }
  const out = new Set<string>()
  const queue = [id]
  while (queue.length) {
    const next = queue.shift()!
    if (out.has(next)) continue
    out.add(next)
    for (const child of children.get(next) || []) queue.push(child)
  }
  return out
}

export function folderPath(folders: Folder[], id?: string) {
  if (!id) return ''
  const byID = folderMap(folders)
  const parts: string[] = []
  const seen = new Set<string>()
  let current = byID.get(id)
  while (current && !seen.has(current.id)) {
    seen.add(current.id)
    parts.unshift(current.name)
    current = current.parent_id ? byID.get(current.parent_id) : undefined
  }
  return parts.join(' / ')
}

export function flattenFolders(
  folders: Folder[],
  exclude?: Set<string>,
): { folder: Folder; depth: number }[] {
  const byParent = new Map<string, Folder[]>()
  for (const folder of folders) {
    if (exclude?.has(folder.id)) continue
    const parent = folder.parent_id && folders.some((f) => f.id === folder.parent_id)
      ? folder.parent_id
      : ''
    const list = byParent.get(parent) || []
    list.push(folder)
    byParent.set(parent, list)
  }
  const out: { folder: Folder; depth: number }[] = []
  const visit = (parent: string, depth: number, trail: Set<string>) => {
    for (const folder of byParent.get(parent) || []) {
      if (trail.has(folder.id)) continue
      out.push({ folder, depth })
      visit(folder.id, depth + 1, new Set([...trail, folder.id]))
    }
  }
  visit('', 0, new Set())
  return out
}

export function topLevelFolders(folders: Folder[]) {
  return flattenFolders(folders).filter((entry) => entry.depth === 0).map((entry) => entry.folder)
}

export function buildLibraryCounts<T extends { folder_id?: string; pinned?: boolean }>(
  folders: Folder[],
  items: T[],
  isAttention: (item: T) => boolean,
): LibraryCounts {
  const known = folderMap(folders)
  const byFolder: Record<string, number> = {}
  for (const folder of folders) {
    const IDs = folderDescendantIDs(folders, folder.id)
    byFolder[folder.id] = items.filter((item) => item.folder_id && IDs.has(item.folder_id)).length
  }
  return {
    all: items.length,
    pinned: items.filter((item) => item.pinned).length,
    attention: items.filter(isAttention).length,
    unfiled: items.filter((item) => !item.folder_id || !known.has(item.folder_id)).length,
    byFolder,
  }
}

export function LibraryNav({
  folders,
  counts,
  selection,
  onSelect,
  onCreateFolder,
  onRenameFolder,
  onDeleteFolder,
  onOrganize,
  organizing,
}: {
  folders: Folder[]
  counts: LibraryCounts
  selection: LibrarySelection
  onSelect: (selection: LibrarySelection) => void
  onCreateFolder: () => void
  onRenameFolder: (folder: Folder) => void
  onDeleteFolder: (folder: Folder) => void
  onOrganize?: () => void
  organizing?: boolean
}) {
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set())
  const roots = useMemo(() => {
    const known = new Set(folders.map((folder) => folder.id))
    return folders.filter((folder) => !folder.parent_id || !known.has(folder.parent_id))
  }, [folders])

  function toggleFolder(id: string) {
    setCollapsed((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  return (
    <aside className="library-nav" aria-label="Library folders">
      <div className="library-nav-head">
        <span>Library</span>
        <IconButton label="New folder" onClick={onCreateFolder}><FolderPlus size={15} /></IconButton>
      </div>
      <div className="library-nav-scroll">
        <div className="library-smart">
          <NavRow icon={<Layers size={14} />} label="All" count={counts.all} active={selection.kind === 'all'} onClick={() => onSelect({ kind: 'all' })} />
          <NavRow icon={<Star size={14} />} label="Pinned" count={counts.pinned} active={selection.kind === 'pinned'} onClick={() => onSelect({ kind: 'pinned' })} />
          <NavRow icon={<CircleAlert size={14} />} label="Needs attention" count={counts.attention} tone="warn" active={selection.kind === 'attention'} onClick={() => onSelect({ kind: 'attention' })} />
          <NavRow icon={<Inbox size={14} />} label="Unfiled" count={counts.unfiled} active={selection.kind === 'unfiled'} onClick={() => onSelect({ kind: 'unfiled' })} />
        </div>

        {onOrganize && (
          <button
            className="library-organize"
            onClick={onOrganize}
            disabled={organizing}
            title="Let the configured model create a tidy folder structure"
          >
            {organizing ? <Loader2 className="spin" size={13} /> : <Sparkles size={13} />}
            <span>{organizing ? 'Organizing…' : 'Organize with agent'}</span>
          </button>
        )}

        <div className="library-nav-section">
          <span>Folders</span>
          <button className="link-btn tiny" onClick={onCreateFolder}><FolderPlus size={12} /> New</button>
        </div>

        {roots.map((folder) => (
          <FolderNode
            key={folder.id}
            folder={folder}
            folders={folders}
            depth={0}
            counts={counts}
            collapsed={collapsed}
            selection={selection}
            onSelect={onSelect}
            onToggle={toggleFolder}
            onRename={onRenameFolder}
            onDelete={onDeleteFolder}
          />
        ))}

        {folders.length === 0 && (
          <div className="library-nav-empty">
            <p>Group related items into folders.</p>
            <button className="link-btn tiny" onClick={onCreateFolder}><FolderPlus size={12} /> Create a folder</button>
          </div>
        )}
      </div>
    </aside>
  )
}

function FolderNode({
  folder,
  folders,
  depth,
  counts,
  collapsed,
  selection,
  onSelect,
  onToggle,
  onRename,
  onDelete,
}: {
  folder: Folder
  folders: Folder[]
  depth: number
  counts: LibraryCounts
  collapsed: Set<string>
  selection: LibrarySelection
  onSelect: (selection: LibrarySelection) => void
  onToggle: (id: string) => void
  onRename: (folder: Folder) => void
  onDelete: (folder: Folder) => void
}) {
  const children = folders.filter((f) => f.parent_id === folder.id)
  const isOpen = !collapsed.has(folder.id)
  const active = selection.kind === 'folder' && selection.id === folder.id
  return (
    <div>
      <div className={clsx('library-folder-row', active && 'active')} style={{ paddingLeft: 4 + depth * 12 }}>
        {children.length > 0 ? (
          <button
            className="folder-caret"
            aria-label={isOpen ? `Collapse ${folder.name}` : `Expand ${folder.name}`}
            onClick={() => onToggle(folder.id)}
          >
            {isOpen ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
          </button>
        ) : <span className="folder-caret-spacer" />}
        <button className="folder-hit" onClick={() => onSelect({ kind: 'folder', id: folder.id })} title={folderPath(folders, folder.id)}>
          <FolderIcon size={14} />
          <span>{folder.name}</span>
          <em>{counts.byFolder[folder.id] || 0}</em>
        </button>
        <span className="folder-actions">
          <IconButton label={`Rename ${folder.name}`} onClick={() => onRename(folder)}><Pencil size={11} /></IconButton>
          <IconButton label={`Delete ${folder.name}`} className="danger" onClick={() => onDelete(folder)}><Trash2 size={11} /></IconButton>
        </span>
      </div>
      {children.length > 0 && isOpen && (
        <div className="folder-children">
          {children.map((child) => (
            <FolderNode
              key={child.id}
              folder={child}
              folders={folders}
              depth={depth + 1}
              counts={counts}
              collapsed={collapsed}
              selection={selection}
              onSelect={onSelect}
              onToggle={onToggle}
              onRename={onRename}
              onDelete={onDelete}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function NavRow({
  icon,
  label,
  count,
  active,
  tone,
  onClick,
}: {
  icon: ReactNode
  label: string
  count: number
  active: boolean
  tone?: 'warn'
  onClick: () => void
}) {
  return (
    <button className={clsx('library-nav-row', active && 'active', tone === 'warn' && count > 0 && 'warn')} onClick={onClick}>
      {icon}
      <span>{label}</span>
      <em>{count}</em>
    </button>
  )
}

export function FolderDialog({
  open,
  onClose,
  mode,
  folder,
  folders,
  defaultParentID,
  onSubmit,
}: {
  open: boolean
  onClose: () => void
  mode: 'create' | 'edit'
  folder?: Folder
  folders: Folder[]
  defaultParentID?: string
  onSubmit: (name: string, parentID: string) => Promise<boolean>
}) {
  const [name, setName] = useState('')
  const [parentID, setParentID] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    setName(folder?.name || '')
    setParentID(folder?.parent_id || defaultParentID || '')
  }, [open, folder?.id, folder?.name, folder?.parent_id, defaultParentID])

  const excluded = folder ? folderDescendantIDs(folders, folder.id) : undefined
  const options = flattenFolders(folders, excluded)

  async function submit() {
    if (!name.trim()) return
    setSaving(true)
    try {
      const ok = await onSubmit(name.trim(), parentID)
      if (ok) onClose()
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={mode === 'create' ? 'New folder' : 'Rename folder'}
      subtitle={mode === 'create' ? 'Folders keep related tools and workflows together. They can be nested.' : 'Renaming or moving a folder does not change its contents.'}
      width={440}
      footer={(
        <>
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          <Button variant="primary" onClick={submit} loading={saving} disabled={!name.trim()}>{mode === 'create' ? 'Create folder' : 'Save folder'}</Button>
        </>
      )}
    >
      <div className="form-grid">
        <Field label="Name">
          <TextInput value={name} autoFocus onChange={(e) => setName(e.target.value)} placeholder="e.g. Research" onKeyDown={(e) => { if (e.key === 'Enter') void submit() }} />
        </Field>
        <Field label="Parent folder" hint="Leave empty for a top-level folder.">
          <Select value={parentID} onChange={(e) => setParentID(e.target.value)}>
            <option value="">No parent (top level)</option>
            {options.map(({ folder: option, depth }) => (
              <option key={option.id} value={option.id}>{`${'— '.repeat(depth)}${option.name}`}</option>
            ))}
          </Select>
        </Field>
      </div>
    </Modal>
  )
}

export function MoveToFolderDialog({
  open,
  onClose,
  itemName,
  folders,
  currentFolderID,
  onMove,
}: {
  open: boolean
  onClose: () => void
  itemName: string
  folders: Folder[]
  currentFolderID?: string
  onMove: (folderID: string) => Promise<boolean>
}) {
  const [selected, setSelected] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (open) setSelected(currentFolderID || '')
  }, [open, currentFolderID])

  async function submit() {
    setSaving(true)
    try {
      const ok = await onMove(selected)
      if (ok) onClose()
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Move to folder"
      subtitle={itemName}
      width={430}
      footer={(
        <>
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          <Button variant="primary" onClick={submit} loading={saving} disabled={selected === (currentFolderID || '')}>Move</Button>
        </>
      )}
    >
      <div className="move-list">
        <button className={clsx('move-row', selected === '' && 'active')} onClick={() => setSelected('')}>
          <Inbox size={14} />
          <span>No folder</span>
          {!currentFolderID && <em>current</em>}
        </button>
        {flattenFolders(folders).map(({ folder, depth }) => (
          <button
            key={folder.id}
            className={clsx('move-row', selected === folder.id && 'active')}
            style={{ paddingLeft: 9 + depth * 14 }}
            onClick={() => setSelected(folder.id)}
          >
            <FolderIcon size={14} />
            <span>{folder.name}</span>
            {folder.id === currentFolderID && <em>current</em>}
          </button>
        ))}
      </div>
    </Modal>
  )
}
