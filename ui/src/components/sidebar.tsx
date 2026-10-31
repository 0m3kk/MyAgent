import {
  Activity,
  Bot,
  Command,
  GitBranch,
  MessageSquareText,
  Plus,
  Search,
  Settings2,
  ShieldCheck,
  SlidersHorizontal,
  Wrench,
} from 'lucide-react'
import clsx from 'clsx'
import { formatRelative } from '../format'
import { href, navigate, useRoute } from '../router'
import { useApp } from '../store'
import { Badge, IconButton } from './ui'

export function Sidebar({
  onCommand,
  onApprovals,
}: {
  onCommand: () => void
  onApprovals: () => void
}) {
  const { data, approvals } = useApp()
  const route = useRoute()
  if (!data) return null
  const conversations = data.conversations.slice(0, 5)
  const defaultModel = data.model_profiles.find((m) => m.id === data.settings.default_model_id) || data.model_profiles[0]
  const running = (data.counts?.run_running || 0) + (data.counts?.run_waiting_approval || 0)
  const nav = [
    { id: 'chats', label: 'Chats', icon: MessageSquareText, count: data.conversations.length },
    { id: 'workflows', label: 'Workflows', icon: GitBranch, count: data.workflows.length },
    { id: 'tools', label: 'Tools', icon: Wrench, count: data.tools.length },
    { id: 'runs', label: 'Runs', icon: Activity, count: running || undefined },
    { id: 'tuning', label: 'Tuning', icon: SlidersHorizontal },
  ]
  const activeRoot = route.segments[0] || 'chats'

  return (
    <aside className="sidebar">
      <div className="sidebar-top">
        <a className="brand" href={href('/chats')} onClick={(e) => { e.preventDefault(); navigate('/chats') }}>
          <span className="brand-mark"><Bot size={18} /></span>
          <span className="brand-copy">
            <strong>MyAgent</strong>
          </span>
        </a>
        <div className="sidebar-actions">
          <button className="new-chat" onClick={() => navigate('/chats')}>
            <Plus size={15} /> New chat
          </button>
          <IconButton label="Command palette (⌘K)" onClick={onCommand} className="sidebar-icon">
            <Command size={16} />
          </IconButton>
        </div>
      </div>

      <nav className="nav">
        {nav.map((item) => {
          const Icon = item.icon
          const active = activeRoot === item.id || (item.id === 'chats' && activeRoot === 'chats')
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
          {conversations.map((c) => (
            <a
              key={c.id}
              href={href(`/chats/${c.id}`)}
              className={clsx('recent-item', route.segments[1] === c.id && 'active')}
              onClick={(e) => { e.preventDefault(); navigate(`/chats/${c.id}`) }}
              title={c.title}
            >
              <MessageSquareText size={13} />
              <span>{c.title}</span>
              <em>{formatRelative(c.updated_at)}</em>
            </a>
          ))}
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
            <span>{defaultModel ? `${defaultModel.model} · ${defaultModel.role}` : 'Open settings to add one'}</span>
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
