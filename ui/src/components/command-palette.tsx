import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Activity,
  ArrowRight,
  Command,
  GitBranch,
  Home,
  MessageSquareText,
  Plus,
  Search,
  Settings2,
  Sparkles,
  Wrench,
  ZoomIn,
  ZoomOut,
  RotateCcw,
} from 'lucide-react'
import clsx from 'clsx'
import { navigate } from '../router'
import { useApp } from '../store'
import { versionLabel } from '../format'
import { Kbd } from './ui'

interface Item {
  id: string
  label: string
  hint?: string
  group: string
  icon: React.ReactNode
  keywords?: string
  action: () => void
}

export function CommandPalette({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { data, increaseFontScale, decreaseFontScale, resetFontScale } = useApp()
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (open) {
      setQuery('')
      setActive(0)
      window.setTimeout(() => inputRef.current?.focus(), 30)
    }
  }, [open])

  const items = useMemo<Item[]>(() => {
    if (!data) return []
    const base: Item[] = [
      { id: 'nav-home', label: 'Go to Home', group: 'Navigate', icon: <Home size={16} />, action: () => navigate('/home') },
      { id: 'nav-chats', label: 'Go to Chats', group: 'Navigate', icon: <MessageSquareText size={16} />, action: () => navigate('/chats') },
      { id: 'nav-workflows', label: 'Go to Workflows', group: 'Navigate', icon: <GitBranch size={16} />, action: () => navigate('/workflows') },
      { id: 'nav-tools', label: 'Go to Tools', group: 'Navigate', icon: <Wrench size={16} />, action: () => navigate('/tools') },
      { id: 'nav-runs', label: 'Go to Runs', group: 'Navigate', icon: <Activity size={16} />, action: () => navigate('/runs') },
      { id: 'nav-settings', label: 'Open Settings', group: 'Navigate', icon: <Settings2 size={16} />, action: () => navigate('/settings') },
      { id: 'new-chat', label: 'Start a new chat', group: 'Actions', icon: <Plus size={16} />, action: () => navigate('/chats/new') },
      { id: 'new-workflow', label: 'Create a workflow', group: 'Actions', icon: <Sparkles size={16} />, action: () => navigate('/workflows/new') },
      { id: 'new-tool', label: 'Create a tool', group: 'Actions', icon: <Wrench size={16} />, action: () => navigate('/tools/new') },
      { id: 'new-model', label: 'Add a model profile', group: 'Actions', icon: <Settings2 size={16} />, action: () => navigate('/settings/models/new') },
      { id: 'new-secret', label: 'Add a secret', group: 'Actions', icon: <Settings2 size={16} />, action: () => navigate('/settings/secrets/new') },
      { id: 'font-up', label: 'Increase text size', hint: '⌘ +', group: 'Appearance', icon: <ZoomIn size={16} />, action: increaseFontScale },
      { id: 'font-down', label: 'Decrease text size', hint: '⌘ −', group: 'Appearance', icon: <ZoomOut size={16} />, action: decreaseFontScale },
      { id: 'font-reset', label: 'Reset text size', hint: '⌘ 0', group: 'Appearance', icon: <RotateCcw size={16} />, action: resetFontScale },
    ]
    for (const c of data.conversations.slice(0, 12)) {
      base.push({ id: `conv-${c.id}`, label: c.title, hint: 'Conversation', group: 'Chats', icon: <MessageSquareText size={16} />, action: () => navigate(`/chats/${c.id}`) })
    }
    for (const w of data.workflows.slice(0, 30)) {
      base.push({
        id: `wf-${w.id}`, label: w.name, hint: w.version ? (w.version.status === 'draft' ? 'draft' : `${versionLabel(w.version)} · ${w.version.status}`) : '', group: 'Workflows',
        icon: <GitBranch size={16} />, keywords: w.version?.yaml, action: () => navigate(`/workflows/${w.id}`),
      })
    }
    for (const t of data.tools.slice(0, 30)) {
      base.push({ id: `tool-${t.id}`, label: t.name, hint: t.side_effect, group: 'Tools', icon: <Wrench size={16} />, keywords: t.description, action: () => navigate(`/tools/${t.id}`) })
    }
    return base
  }, [data, increaseFontScale, decreaseFontScale, resetFontScale])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return items.slice(0, 18)
    return items
      .map((item) => {
        const hay = `${item.label} ${item.hint || ''} ${item.group} ${item.keywords || ''}`.toLowerCase()
        let score = 0
        for (const word of q.split(/\s+/)) if (hay.includes(word)) score += 1
        return { item, score }
      })
      .filter((x) => x.score > 0)
      .sort((a, b) => b.score - a.score)
      .slice(0, 24)
      .map((x) => x.item)
  }, [items, query])

  useEffect(() => setActive(0), [query])

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setActive((a) => Math.min(a + 1, filtered.length - 1))
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault()
        setActive((a) => Math.max(a - 1, 0))
      }
      if (e.key === 'Enter' && filtered[active]) {
        e.preventDefault()
        filtered[active].action()
        onClose()
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, filtered, active, onClose])

  useEffect(() => {
    const el = listRef.current?.querySelector<HTMLElement>(`[data-index="${active}"]`)
    el?.scrollIntoView({ block: 'nearest' })
  }, [active])

  if (!open) return null
  let lastGroup = ''
  return (
    <div className="cmdk-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="cmdk" role="dialog" aria-modal="true" aria-label="Command palette">
        <div className="cmdk-input-row">
          <Search size={17} />
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search chats, workflows, tools or actions…"
            aria-label="Command"
          />
          <Kbd>esc</Kbd>
        </div>
        <div className="cmdk-list" ref={listRef}>
          {filtered.map((item, index) => {
            const showGroup = item.group !== lastGroup
            lastGroup = item.group
            return (
              <div key={item.id}>
                {showGroup && <div className="cmdk-group">{item.group}</div>}
                <button
                  data-index={index}
                  className={clsx('cmdk-item', active === index && 'active')}
                  onMouseEnter={() => setActive(index)}
                  onClick={() => { item.action(); onClose() }}
                >
                  <span className="cmdk-icon">{item.icon}</span>
                  <span className="cmdk-label">{item.label}</span>
                  {item.hint && <span className="cmdk-hint">{item.hint}</span>}
                  {active === index ? <ArrowRight size={14} /> : <span className="cmdk-enter"><Kbd>↵</Kbd></span>}
                </button>
              </div>
            )
          })}
          {filtered.length === 0 && (
            <div className="cmdk-empty">
              <Command size={18} />
              <span>No matches. Try a workflow or tool name.</span>
            </div>
          )}
        </div>
        <div className="cmdk-footer">
          <span><Kbd>↑</Kbd><Kbd>↓</Kbd> navigate</span>
          <span><Kbd>↵</Kbd> open</span>
          <span className="cmdk-brand">MyAgent</span>
        </div>
      </div>
    </div>
  )
}
