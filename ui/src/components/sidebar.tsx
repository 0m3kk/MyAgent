import { useCallback, useEffect, useState } from 'react'
import {
  Activity,
  Bot,
  Command,
  GitBranch,
  Home,
  Loader2,
  MessageSquareText,
  Search,
  Settings2,
  ShieldCheck,
  Wrench,
} from 'lucide-react'
import clsx from 'clsx'
import { api } from '../api'
import { formatRelative } from '../format'
import { usePolling } from '../hooks'
import { href, navigate, useRoute } from '../router'
import { useApp } from '../store'
import { Badge } from './ui'
import { WorkspaceSwitcher } from './workspace-switcher'

export function Sidebar({
  onCommand,
  onApprovals,
}: {
  onCommand: () => void
  onApprovals: () => void
}) {
  const { data, approvals } = useApp()
  const route = useRoute()
  const [activeChats, setActiveChats] = useState<Set<string>>(() => new Set())
  const refreshActiveChats = useCallback(async () => {
    try {
      const res = await api.activeConversations()
      const ids = res.conversation_ids ?? []
      setActiveChats((prev) => {
        if (prev.size === ids.length && ids.every((id) => prev.has(id))) return prev
        return new Set(ids)
      })
    } catch {
      // Keep the last known state while the local core restarts.
    }
  }, [])
  useEffect(() => {
    void refreshActiveChats()
  }, [refreshActiveChats])
  usePolling(refreshActiveChats, 2_000, !!data)
  if (!data) return null
  const conversations = data.conversations.slice(0, 5)
  const defaultModel = data.model_profiles.find((m) => m.id === data.settings.default_model_id) || data.model_profiles[0]
  const running = (data.counts?.run_running || 0) + (data.counts?.run_waiting_approval || 0)
  const nav = [
    { id: 'home', label: 'Home', icon: Home },
    { id: 'chats', label: 'Chats', icon: MessageSquareText, count: data.conversations.length },
    { id: 'workflows', label: 'Workflows', icon: GitBranch, count: data.workflows.length },
    { id: 'tools', label: 'Tools', icon: Wrench, count: data.tools.length },
    { id: 'runs', label: 'Runs', icon: Activity, count: running || undefined },
  ]
  const activeRoot = route.segments[0] || 'home'

  return (
    <aside className="sidebar">
      <div className="sidebar-top">
        <a className="brand" href={href('/home')} onClick={(e) => { e.preventDefault(); navigate('/home') }}>
          <span className="brand-mark"><Bot size={18} /></span>
          <span className="brand-copy">
            <strong>MyAgent</strong>
          </span>
        </a>
        <button className="command-trigger" onClick={onCommand} title="Command palette (⌘K)">
          <Command size={15} />
          <span>Command</span>
          <kbd>⌘K</kbd>
        </button>
        <WorkspaceSwitcher />
      </div>

      <nav className="nav">
        {nav.map((item) => {
          const Icon = item.icon
          const active = activeRoot === item.id
          return (
            <a
              key={item.id}
              href={href(`/${item.id}`)}
              className={clsx('nav-item', active && 'active')}
              onClick={(e) => { e.preventDefault(); navigate(`/${item.id}`) }}
            >
              <Icon size={16} />
              <span>{item.label}</span>
              {item.count !== undefined && <span className="nav-count">{item.count}</span>}
            </a>
          )
        })}
      </nav>

      <div className="sidebar-recent">
        <div className="sidebar-section-label">
          <span>Recent</span>
          <button className="link-btn tiny" onClick={onCommand}><Search size={12} /> Search</button>
        </div>
        <div className="recent-list">
          {conversations.map((c) => {
            const isRunning = activeChats.has(c.id)
            return (
              <a
                key={c.id}
                href={href(`/chats/${c.id}`)}
                className={clsx('recent-item', route.segments[1] === c.id && 'active', isRunning && 'running')}
                onClick={(e) => { e.preventDefault(); navigate(`/chats/${c.id}`) }}
                title={c.title}
              >
                <MessageSquareText size={13} />
                <span>{c.title}</span>
                {isRunning && <Loader2 size={11} className="spin recent-running" aria-label="Agent is running" />}
                <em>{formatRelative(c.updated_at)}</em>
              </a>
            )
          })}
          {conversations.length === 0 && <div className="sidebar-empty">No conversations yet</div>}
        </div>
      </div>

      <div className="sidebar-foot">
        <button className="approval-button" onClick={onApprovals}>
          <span className={clsx('approval-dot', approvals.length > 0 && 'active')}>
            <ShieldCheck size={13} />
          </span>
          <span>{approvals.length > 0 ? `${approvals.length} waiting for approval` : 'No pending approvals'}</span>
          {approvals.length > 0 && <Badge tone="warn">{approvals.length}</Badge>}
        </button>
        <div className="model-card">
          <span className="model-orb" />
          <div>
            <strong>{defaultModel?.name || 'No model'}</strong>
            <span>{defaultModel ? defaultModel.model : 'Open settings to add one'}</span>
          </div>
          <span className={`model-status ${defaultModel ? 'ok' : 'missing'}`} />
        </div>
        <div className="sidebar-bottom-row">
          <a
            href={href('/settings')}
            className={clsx('nav-item slim', activeRoot === 'settings' && 'active')}
            onClick={(e) => { e.preventDefault(); navigate('/settings') }}
          >
            <Settings2 size={16} /><span>Settings</span>
            {data.key_kind === 'file' && <span className="dev-key-dot" title="Development key file" />}
          </a>
        </div>
      </div>
    </aside>
  )
}
