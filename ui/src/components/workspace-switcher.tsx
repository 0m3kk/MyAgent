import { useCallback, useEffect, useRef, useState } from 'react'
import clsx from 'clsx'
import {
  Check,
  ChevronsUpDown,
  Download,
  Pencil,
  Plus,
  Trash2,
  Upload,
} from 'lucide-react'
import { api } from '../api'
import { useApp } from '../store'
import { useConfirm } from './confirm'
import { useToast } from './toast'
import { Button, Field, Modal, Spinner, TextInput } from './ui'
import type { WorkspaceProfile } from '../types'

export function WorkspaceSwitcher() {
  const { data, refresh } = useApp()
  const confirm = useConfirm()
  const toast = useToast()
  const [enabled, setEnabled] = useState(false)
  const [profiles, setProfiles] = useState<WorkspaceProfile[]>([])
  const [activeId, setActiveId] = useState('')
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [switching, setSwitching] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [createName, setCreateName] = useState('')
  const [createRoot, setCreateRoot] = useState('')
  const [editTarget, setEditTarget] = useState<WorkspaceProfile | null>(null)
  const [editName, setEditName] = useState('')
  const [editRoot, setEditRoot] = useState('')
  const fileRef = useRef<HTMLInputElement>(null)

  const load = useCallback(async () => {
    try {
      const res = await api.workspaces()
      setEnabled(res.enabled)
      setProfiles(res.workspaces)
      setActiveId(res.active)
    } catch {
      setEnabled(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  if (!data || !enabled) return null

  const active = profiles.find((p) => p.id === activeId)
  const displayName = active?.name || data.workspace.name
  const displayRoot = active?.root_path || data.workspace.root_path

  function openMenu() {
    setOpen((v) => !v)
    void load()
  }

  async function switchTo(profile: WorkspaceProfile) {
    if (profile.id === activeId) {
      setOpen(false)
      return
    }
    const ok = await confirm({
      title: 'Switch workspace',
      message: `Switch to “${profile.name}”? Active chats and runs in “${displayName}” will stop.`,
      confirmLabel: 'Switch workspace',
      tone: 'primary',
    })
    if (!ok) return
    setOpen(false)
    setSwitching(true)
    try {
      await api.activateWorkspace(profile.id)
    } catch (err) {
      // The core exits right after replying, so a transport error here can be
      // the expected restart. HTTP errors are real failures.
      if ((err as { status?: number }).status) {
        setSwitching(false)
        toast('Could not switch workspace', err instanceof Error ? err.message : undefined, 'error')
      }
    }
  }

  async function createWorkspace() {
    const name = createName.trim()
    if (!name || busy) return
    setBusy(true)
    try {
      const profile = await api.createWorkspace(name, createRoot.trim())
      setCreateOpen(false)
      setCreateName('')
      setCreateRoot('')
      await load()
      toast('Workspace created', profile.root_path, 'success')
    } catch (err) {
      toast('Could not create workspace', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(false)
    }
  }

  async function saveWorkspace() {
    if (!editTarget || !editName.trim() || busy) return
    setBusy(true)
    try {
      await api.updateWorkspace(editTarget.id, {
        name: editName.trim(),
        root_path: editRoot.trim(),
      })
      setEditTarget(null)
      setEditName('')
      setEditRoot('')
      await load()
      void refresh()
      toast('Workspace updated', undefined, 'success')
    } catch (err) {
      toast('Could not update workspace', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(false)
    }
  }

  async function removeWorkspace(profile: WorkspaceProfile) {
    setOpen(false)
    const ok = await confirm({
      title: 'Delete workspace',
      message: `Delete “${profile.name}”? Its chats, workflows, tools and runs will be removed. The project folder is not touched.`,
      confirmLabel: 'Delete workspace',
    })
    if (!ok) return
    try {
      await api.deleteWorkspace(profile.id)
      await load()
      toast('Workspace deleted', undefined, 'success')
    } catch (err) {
      toast('Could not delete workspace', err instanceof Error ? err.message : undefined, 'error')
    }
  }

  function exportWorkspace(profile: WorkspaceProfile) {
    setOpen(false)
    window.location.assign(api.exportWorkspaceUrl(profile.id))
  }

  async function importWorkspace(file: File) {
    setBusy(true)
    try {
      const profile = await api.importWorkspace(file)
      await load()
      toast(
        'Workspace imported',
        'Secrets are not included. Re-enter API keys in Settings before using model profiles.',
        'success',
      )
      await switchTo(profile)
    } catch (err) {
      toast('Could not import workspace', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="workspace-wrap">
      <button className="workspace-trigger" onClick={openMenu} aria-label="Workspace menu">
        <span className="workspace-avatar">{displayName.slice(0, 1).toUpperCase()}</span>
        <span className="workspace-copy">
          <strong>{displayName}</strong>
          <em>{displayRoot}</em>
        </span>
        <ChevronsUpDown size={14} />
      </button>

      {open && (
        <div className="menu-pop workspace-menu">
          <div className="workspace-menu-label">Workspaces</div>
          <div className="workspace-menu-list">
            {profiles.map((profile) => {
              const isActive = profile.id === activeId
              return (
                <div key={profile.id} className={clsx('workspace-menu-row', isActive && 'active')}>
                  <button className="workspace-menu-main" onClick={() => void switchTo(profile)}>
                    <span className="workspace-check">{isActive ? <Check size={13} /> : null}</span>
                    <span className="workspace-menu-copy">
                      <strong>{profile.name}</strong>
                      <em>{profile.root_path}</em>
                    </span>
                  </button>
                  <button
                    className="icon-btn tiny"
                    title="Rename workspace"
                    aria-label={`Rename ${profile.name}`}
                    onClick={() => {
                      setOpen(false)
                      setEditTarget(profile)
                      setEditName(profile.name)
                      setEditRoot(profile.root_path)
                    }}
                  >
                    <Pencil size={12} />
                  </button>
                  <button
                    className="icon-btn tiny"
                    title="Export workspace"
                    aria-label={`Export ${profile.name}`}
                    onClick={() => exportWorkspace(profile)}
                  >
                    <Download size={12} />
                  </button>
                  {!isActive && (
                    <button
                      className="icon-btn tiny danger"
                      title="Delete workspace"
                      aria-label={`Delete ${profile.name}`}
                      onClick={() => void removeWorkspace(profile)}
                    >
                      <Trash2 size={12} />
                    </button>
                  )}
                </div>
              )
            })}
          </div>
          <div className="workspace-menu-actions">
            <button onClick={() => { setOpen(false); setCreateOpen(true) }}>
              <Plus size={14} /> New workspace…
            </button>
            <button onClick={() => { setOpen(false); fileRef.current?.click() }}>
              <Upload size={14} /> Import workspace…
            </button>
          </div>
        </div>
      )}

      <input
        ref={fileRef}
        className="hidden-file"
        type="file"
        accept=".myagentws,application/zip"
        onChange={(e) => {
          const file = e.target.files?.[0]
          e.target.value = ''
          if (file) void importWorkspace(file)
        }}
      />


      <Modal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        title="New workspace"
        subtitle="Each workspace has its own chats, workflows, tools, runs, secrets and settings."
        width={520}
        footer={(
          <>
            <Button variant="ghost" onClick={() => setCreateOpen(false)}>Cancel</Button>
            <Button variant="primary" onClick={() => void createWorkspace()} loading={busy} disabled={!createName.trim()}>
              Create workspace
            </Button>
          </>
        )}
      >
        <Field label="Name" required>
          <TextInput
            autoFocus
            value={createName}
            onChange={(e) => setCreateName(e.target.value)}
            placeholder="Client work"
            onKeyDown={(e) => { if (e.key === 'Enter') void createWorkspace() }}
          />
        </Field>
        <Field label="Project folder" hint="Leave blank for ~/MyAgent/<name>. This folder is not included in exports.">
          <TextInput
            value={createRoot}
            onChange={(e) => setCreateRoot(e.target.value)}
            placeholder="~/Projects/MyAgent"
            className="mono"
          />
        </Field>
      </Modal>

      <Modal
        open={editTarget !== null}
        onClose={() => setEditTarget(null)}
        title="Edit workspace"
        subtitle="The project folder is not included in workspace exports."
        width={520}
        footer={(
          <>
            <Button variant="ghost" onClick={() => setEditTarget(null)}>Cancel</Button>
            <Button variant="primary" onClick={() => void saveWorkspace()} loading={busy} disabled={!editName.trim()}>
              Save workspace
            </Button>
          </>
        )}
      >
        <Field label="Name" required>
          <TextInput
            autoFocus
            value={editName}
            onChange={(e) => setEditName(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') void saveWorkspace() }}
          />
        </Field>
        <Field label="Project folder" hint="Changing the folder while chats or runs are active is blocked.">
          <TextInput
            value={editRoot}
            onChange={(e) => setEditRoot(e.target.value)}
            className="mono"
          />
        </Field>
      </Modal>

      {switching && (
        <div className="workspace-switching">
          <div>
            <Spinner size={22} />
            <strong>Switching workspace…</strong>
            <span>Restarting the local core. The window will reload.</span>
          </div>
        </div>
      )}
    </div>
  )
}
