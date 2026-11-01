import { useEffect, useState } from 'react'
import { AlertTriangle, Bot, Command, RefreshCw, ShieldCheck, Sparkles } from 'lucide-react'
import { useApp } from './store'
import { navigate, useRoute } from './router'
import { Sidebar } from './components/sidebar'
import { CommandPalette } from './components/command-palette'
import { ApprovalCenter } from './components/approval'
import { Button, EmptyState, Spinner } from './components/ui'
import { ChatHome, ChatPage, ChatsPage, NewChatPage } from './pages/chat'
import { WorkflowPage, WorkflowsPage } from './pages/workflows'
import { RunPage, RunsPage } from './pages/runs'
import { ToolPage, ToolsPage } from './pages/tools'
import { SettingsPage } from './pages/settings'

export default function App() {
  const { data, loading, error, refresh, increaseFontScale, decreaseFontScale, resetFontScale } = useApp()
  const route = useRoute()
  const [commandOpen, setCommandOpen] = useState(false)
  const [approvalOpen, setApprovalOpen] = useState(false)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setCommandOpen((v) => !v)
        return
      }
      if (e.metaKey || e.ctrlKey) {
        if (e.key === '=' || e.key === '+' || e.key === 'Add') {
          e.preventDefault()
          increaseFontScale()
          return
        }
        if (e.key === '-' || e.key === '_' || e.key === 'Subtract') {
          e.preventDefault()
          decreaseFontScale()
          return
        }
        if (e.key === '0') {
          e.preventDefault()
          resetFontScale()
          return
        }
      }
      if (e.key === 'Escape') {
        setCommandOpen(false)
        setApprovalOpen(false)
      }
    }
    const openApprovals = () => setApprovalOpen(true)
    window.addEventListener('keydown', onKey)
    window.addEventListener('myagent:open-approvals', openApprovals)
    return () => {
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('myagent:open-approvals', openApprovals)
    }
  }, [increaseFontScale, decreaseFontScale, resetFontScale])

  if (loading && !data) return <BootScreen />
  if (error && !data) {
    return (
      <div className="boot-screen">
        <div className="boot-card">
          <div className="boot-orb error"><AlertTriangle size={24} /></div>
          <h1>Cannot reach the local agent</h1>
          <p>{error}</p>
          <p className="muted small">
            Start the Go core with <code>make dev</code>, or launch the built app. The API only listens on loopback and needs the session token from the app URL or the Vite dev token endpoint.
          </p>
          <Button variant="primary" icon={<RefreshCw size={15} />} onClick={() => void refresh()}>Retry</Button>
        </div>
      </div>
    )
  }
  if (!data) return <BootScreen />

  return (
    <div className="app-shell">
      <Sidebar onCommand={() => setCommandOpen(true)} onApprovals={() => setApprovalOpen(true)} />
      <main className="app-main">
        <Router route={route} />
      </main>
      <CommandPalette open={commandOpen} onClose={() => setCommandOpen(false)} />
      <ApprovalCenter open={approvalOpen} onClose={() => setApprovalOpen(false)} />
    </div>
  )
}

function Router({ route }: { route: ReturnType<typeof useRoute> }) {
  const [root, id, extra] = route.segments
  switch (root) {
    case undefined:
    case 'home':
      return <ChatHome />
    case 'chats':
      if (id === 'new') return <NewChatPage />
      return id ? <ChatPage conversationId={id} /> : <ChatsPage />
    case 'workflows':
      return id ? <WorkflowPage workflowId={id} /> : <WorkflowsPage />
    case 'runs':
      return id ? <RunPage runId={id} /> : <RunsPage />
    case 'tools':
      return id ? <ToolPage toolId={id} /> : <ToolsPage />
    case 'settings':
      return <SettingsPage sub={route.segments.slice(1)} />
    default:
      return (
        <div className="page center-page">
          <EmptyState icon={<Sparkles size={26} />} title="Page not found" description={`Unknown route “${route.path}”.`} action={<Button onClick={() => navigate('/home')}>Go home</Button>} />
        </div>
      )
  }
}

function BootScreen() {
  return (
    <div className="boot-screen">
      <div className="boot-card loading">
        <div className="boot-orb"><Bot size={26} /></div>
        <h1>MyAgent</h1>
        <p>Starting the local core…</p>
        <div className="boot-progress"><span /></div>
        <div className="boot-foot"><ShieldCheck size={13} /> Data stays on this Mac</div>
      </div>
    </div>
  )
}
