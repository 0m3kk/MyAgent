import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  ArrowRight,
  Bot,
  Cpu,
  MessageSquareText,
  MoreHorizontal,
  Plus,
  Send,
  Sparkles,
  Square,
  Trash2,
  User,
  Workflow,
} from 'lucide-react'
import clsx from 'clsx'
import { api } from '../api'
import { navigate } from '../router'
import { useApp } from '../store'
import { useEventStream, usePolling, useAutoScroll } from '../hooks'
import type { Approval, Conversation, EventEnvelope, Message } from '../types'
import { formatRelative } from '../format'
import { ApprovalCard } from '../components/approval'
import { Markdown, MessageMeta, StreamCursor, ToolCard, TypingIndicator, type ToolView } from '../components/chat-bits'
import { Badge, Button, EmptyState, IconButton, Spinner, TextArea } from '../components/ui'
import { useToast } from '../components/toast'

function approvalsFor(list: Approval[], conversationId: string) {
  return list.filter((a) => a.conversation_id === conversationId && a.status === 'pending')
}

export function ChatHome() {
  const { data, approvals, refresh } = useApp()
  const toast = useToast()
  const [starting, setStarting] = useState<string | null>(null)
  const [draft, setDraft] = useState('')
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  useEffect(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, 180)}px`
  }, [draft])
  if (!data) return null
  const recent = data.conversations.slice(0, 6)
  const hasModel = data.model_profiles.length > 0
  const defaultModel = data.model_profiles.find((m) => m.id === data.settings.default_model_id) || data.model_profiles[0]

  async function start(prompt: string): Promise<boolean> {
    if (!hasModel) {
      navigate('/settings/models/new')
      return false
    }
    setStarting(prompt)
    try {
      const res = await api.sendChat('', prompt)
      void refresh()
      navigate(`/chats/${res.conversation.id}`)
      return true
    } catch (err) {
      toast('Could not start the chat', err instanceof Error ? err.message : undefined, 'error')
      return false
    } finally {
      setStarting(null)
    }
  }

  async function sendDraft() {
    const text = draft.trim()
    if (!text || starting) return
    setDraft('')
    const ok = await start(text)
    if (!ok) setDraft(text)
  }

  function onDraftKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      void sendDraft()
    }
  }

  return (
    <div className="page chat-home">
      <div className="home-hero">
        <div className="hero-orb"><Sparkles size={26} /></div>
        <div className="eyebrow">Local-first agent workspace</div>
        <h1>What should we get done?</h1>
        <p>
          Describe the outcome. The agent uses tools, runs workflows, and asks before anything risky.
          It can also build reusable tools and workflows from the work it just did.
        </p>
        {approvals.length > 0 && (
          <button className="hero-approval" onClick={() => window.dispatchEvent(new CustomEvent('myagent:open-approvals'))}>
            <span className="status-dot warn" /> {approvals.length} action{approvals.length > 1 ? 's' : ''} waiting for your approval
            <ArrowRight size={14} />
          </button>
        )}
      </div>

      {!hasModel && (
        <div className="setup-banner">
          <span className="setup-icon"><Cpu size={18} /></span>
          <div>
            <strong>Add a model to start</strong>
            <span>Connect any OpenAI-compatible endpoint — cloud or local. No demo model is installed.</span>
          </div>
          <Button variant="primary" size="md" className="setup-action" onClick={() => navigate('/settings/models/new')}>Add model</Button>
        </div>
      )}

      <div className="home-composer">
        <div className="composer">
          <TextArea
            ref={textareaRef}
            rows={1}
            placeholder={hasModel ? 'Ask anything, or describe a task to automate…' : 'Add a model in Settings to start chatting…'}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={onDraftKeyDown}
            disabled={!!starting || !hasModel}
            autoFocus
          />
          <div className="composer-actions">
            <span className="composer-hint">
              <span><kbd>↵</kbd> send</span>
              <span><kbd>⇧ ↵</kbd> newline</span>
            </span>
            <Button
              variant="primary"
              size="sm"
              icon={<Send size={14} />}
              onClick={sendDraft}
              loading={!!starting}
              disabled={!hasModel || !draft.trim()}
            >
              Send
            </Button>
          </div>
        </div>
        <div className="composer-foot">
          <span>{defaultModel?.model || 'No model selected'}</span>
          <span>·</span>
          <span>Secrets never enter the prompt</span>
        </div>
      </div>

      <div className="home-columns">
        <section className="home-panel">
          <div className="home-panel-head">
            <h2>Recent chats</h2>
            <button className="link-btn" onClick={() => navigate('/chats')}>All chats</button>
          </div>
          <div className="conversation-list">
            {recent.map((c) => (
              <button key={c.id} className="conversation-row" onClick={() => navigate(`/chats/${c.id}`)}>
                <span className="conversation-icon"><MessageSquareText size={15} /></span>
                <span className="conversation-copy">
                  <strong>{c.title}</strong>
                  <em>{formatRelative(c.updated_at)}</em>
                </span>
                <ArrowRight size={14} className="muted" />
              </button>
            ))}
            {recent.length === 0 && (
              <EmptyState compact icon={<MessageSquareText size={22} />} title="No chats yet" description="Start with a suggestion above." />
            )}
          </div>
        </section>
        <section className="home-panel quick-panel">
          <div className="home-panel-head"><h2>Workspace</h2></div>
          <div className="quick-stats">
            <button onClick={() => navigate('/workflows')}>
              <span className="quick-number">{data.workflows.length}</span>
              <span>Workflows</span>
            </button>
            <button onClick={() => navigate('/tools')}>
              <span className="quick-number">{data.tools.length}</span>
              <span>Tools</span>
            </button>
            <button onClick={() => navigate('/runs')}>
              <span className="quick-number">{data.counts?.runs || 0}</span>
              <span>Runs</span>
            </button>
          </div>
          <div className="workspace-note">
            <span className="mono">{data.workspace.root_path}</span>
            <button className="link-btn" onClick={() => navigate('/settings')}>Open settings</button>
          </div>
          <div className="home-tip">
            <Sparkles size={14} />
            <span>Try asking the agent to turn a successful chat into a workflow. It keeps the approvals you already made.</span>
          </div>
        </section>
      </div>
    </div>
  )
}

export function ChatPage({ conversationId }: { conversationId: string }) {
  const { data, approvals, refresh, refreshApprovals } = useApp()
  const toast = useToast()
  const [conversation, setConversation] = useState<Conversation | null>(null)
  const [messages, setMessages] = useState<Message[]>([])
  const [loading, setLoading] = useState(true)
  const [notFound, setNotFound] = useState(false)
  const [draft, setDraft] = useState('')
  const [sending, setSending] = useState(false)
  const [streaming, setStreaming] = useState(false)
  const [streamText, setStreamText] = useState('')
  const [liveTools, setLiveTools] = useState<Record<string, ToolView>>({})
  const [menuOpen, setMenuOpen] = useState(false)
  const [savingWorkflow, setSavingWorkflow] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const pending = useMemo(() => approvalsFor(approvals, conversationId), [approvals, conversationId])
  const defaultModel = data?.model_profiles.find((m) => m.id === data.settings.default_model_id) || data?.model_profiles[0]
  const scrollRef = useAutoScroll<HTMLDivElement>(`${messages.length}:${streamText.length}:${Object.keys(liveTools).length}:${pending.length}`)

  const load = useCallback(async () => {
    try {
      const res = await api.conversation(conversationId)
      setConversation(res.conversation)
      setMessages(res.messages)
      setNotFound(false)
    } catch (err) {
      if ((err as Error & { status?: number }).status === 404) setNotFound(true)
      else toast('Could not load the conversation', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setLoading(false)
    }
  }, [conversationId, toast])

  useEffect(() => {
    setLoading(true)
    setConversation(null)
    setMessages([])
    setStreaming(false)
    setStreamText('')
    setLiveTools({})
    void load()
    void refreshApprovals()
  }, [conversationId, load, refreshApprovals])

  usePolling(() => void load(), 2200, streaming)
  usePolling(() => void refreshApprovals(), 3000, streaming)

  const onEvent = useCallback(
    (event: EventEnvelope) => {
      const p = event.payload as Record<string, unknown>
      switch (event.type) {
        case 'llm.request':
          setStreaming(true)
          setStreamText('')
          break
        case 'llm.delta':
          if (typeof p.text === 'string') {
            setStreaming(true)
            setStreamText((prev) => prev + p.text)
          }
          break
        case 'tool.call.started': {
          const key = String(p.tool_call_id || `${p.tool}-${Date.now()}`)
          setLiveTools((prev) => ({
            ...prev,
            [key]: { key, name: String(p.tool || 'tool'), args: p.args, status: 'running', logs: [] },
          }))
          break
        }
        case 'tool.call.log': {
          const key = String(p.tool_call_id || '')
          const line = `${String(p.stream || 'stdout')}: ${String(p.text || '')}`
          setLiveTools((prev) => {
            const cur = prev[key]
            if (!cur) return prev
            return { ...prev, [key]: { ...cur, logs: [...(cur.logs || []), line].slice(-200) } }
          })
          break
        }
        case 'tool.call.finished': {
          const key = String(p.tool_call_id || '')
          setLiveTools((prev) => {
            const cur = prev[key]
            if (!cur) return prev
            return {
              ...prev,
              [key]: {
                ...cur,
                status: String(p.status || 'succeeded'),
                result: p.result,
                error: p.error ? String(p.error) : undefined,
                duration_ms: typeof p.duration_ms === 'number' ? p.duration_ms : undefined,
              },
            }
          })
          break
        }
        case 'message.created': {
          const msg = p.message as Message | undefined
          if (!msg) break
          if (msg.role === 'assistant') {
            setStreaming(false)
            setStreamText('')
            setLiveTools({})
            void load()
            void refresh()
          } else {
            void load()
          }
          break
        }
        case 'conversation.updated': {
          const c = p.conversation as Conversation | undefined
          if (c?.id === conversationId) setConversation(c)
          break
        }
        case 'approval.requested':
        case 'approval.resolved':
          void refreshApprovals()
          break
        default:
          break
      }
    },
    [conversationId, load, refresh, refreshApprovals],
  )

  useEventStream(conversationId ? `conv:${conversationId}` : null, onEvent, { live: true })

  useEffect(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, 180)}px`
  }, [draft])

  async function send() {
    const text = draft.trim()
    if (!text || sending || streaming) return
    setDraft('')
    setSending(true)
    try {
      await api.sendChat(conversationId, text)
      setStreaming(true)
      await load()
    } catch (err) {
      setDraft(text)
      toast('Message was not sent', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setSending(false)
      window.setTimeout(() => textareaRef.current?.focus(), 30)
    }
  }

  async function stop() {
    try {
      await api.stopChat(conversationId)
      setStreaming(false)
    } catch (err) {
      toast('Could not stop the agent', err instanceof Error ? err.message : undefined, 'error')
    }
  }

  async function saveAsWorkflow() {
    setSavingWorkflow(true)
    setMenuOpen(false)
    try {
      const res = await api.saveAsWorkflow(conversationId, conversation?.title)
      toast('Workflow draft saved', 'Review the YAML and approve it before enabling triggers.', 'success')
      void refresh()
      navigate(`/workflows/${res.workflow.id}`)
    } catch (err) {
      toast('Could not save a workflow', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setSavingWorkflow(false)
    }
  }

  async function removeConversation() {
    if (!window.confirm('Delete this conversation? This does not delete runs or workflows.')) return
    setMenuOpen(false)
    try {
      await api.deleteConversation(conversationId)
      toast('Conversation deleted', undefined, 'success')
      void refresh()
      navigate('/chats')
    } catch (err) {
      toast('Could not delete the conversation', err instanceof Error ? err.message : undefined, 'error')
    }
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      void send()
    }
  }

  if (notFound) {
    return (
      <div className="page">
        <EmptyState
          icon={<MessageSquareText size={26} />}
          title="Conversation not found"
          description="It may have been deleted."
          action={<Button onClick={() => navigate('/chats')}>Back to chats</Button>}
        />
      </div>
    )
  }

  if (loading && !conversation) {
    return <div className="page center-page"><Spinner size={26} /></div>
  }

  const visible = messages.filter((m) => m.role !== 'tool')
  const liveToolList = Object.values(liveTools)
  const lastAssistant = [...visible].reverse().find((m) => m.role === 'assistant')

  return (
    <div className="chat-page">
      <header className="chat-head">
        <div className="chat-title-wrap">
          <div className="eyebrow"><MessageSquareText size={12} /> Chats</div>
          <h1>{conversation?.title || 'Chat'}</h1>
          <div className="chat-subline">
            <span className="model-chip"><span className="model-orb small" /> {defaultModel?.name || 'No model'}</span>
            <span>{lastAssistant ? `Updated ${formatRelative(lastAssistant.created_at)}` : 'New conversation'}</span>
          </div>
        </div>
        <div className="chat-head-actions">
          <Button variant="subtle" size="sm" icon={<Workflow size={15} />} onClick={saveAsWorkflow} loading={savingWorkflow}>
            Save as workflow
          </Button>
          <div className="menu-wrap">
            <IconButton label="Conversation menu" onClick={() => setMenuOpen((o) => !o)}>
              <MoreHorizontal size={17} />
            </IconButton>
            {menuOpen && (
              <div className="menu-pop">
                <button onClick={saveAsWorkflow}><Workflow size={14} /> Save as workflow</button>
                <button className="danger" onClick={removeConversation}><Trash2 size={14} /> Delete conversation</button>
              </div>
            )}
          </div>
        </div>
      </header>

      <div className="chat-scroll" ref={scrollRef}>
        <div className="message-stack">
          {visible.map((m) => (
            <ChatMessage key={m.id} message={m} profileName={defaultModel?.name} />
          ))}

          {(streaming || liveToolList.length > 0) && (
            <div className="message assistant streaming-message">
              <div className="avatar assistant-avatar"><Bot size={16} /></div>
              <div className="message-body">
                <div className="assistant-card">
                  {streamText ? (
                    <>
                      <Markdown>{streamText}</Markdown>
                      <StreamCursor />
                    </>
                  ) : !liveToolList.length ? <TypingIndicator /> : null}
                  {liveToolList.length > 0 && (
                    <div className="message-tools">
                      {liveToolList.map((tool) => <ToolCard key={tool.key} tool={tool} live />)}
                    </div>
                  )}
                </div>
              </div>
            </div>
          )}

          {pending.length > 0 && (
            <div className="inline-approvals">
              {pending.map((ap) => (
                <ApprovalCard
                  key={ap.id}
                  approval={ap}
                  onDecided={() => { void refreshApprovals(); void load() }}
                />
              ))}
            </div>
          )}

          {!streaming && visible.length > 0 && !pending.length && (
            <div className="chat-end-cap">
              <Sparkles size={13} />
              <span>Agent actions are logged. Open <button className="link-btn" onClick={() => navigate('/runs')}><u>Runs</u></button> to inspect traces.</span>
            </div>
          )}
        </div>
      </div>

      <div className="composer-dock">
        <div className="composer">
          <TextArea
            ref={textareaRef}
            rows={1}
            placeholder={streaming ? 'The agent is working… press stop to intervene.' : 'Ask anything, or describe a task to automate…'}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={onKeyDown}
            disabled={sending}
            autoFocus
          />
          <div className="composer-actions">
            <span className="composer-hint">
              <span><kbd>↵</kbd> send</span>
              <span><kbd>⇧ ↵</kbd> newline</span>
            </span>
            {streaming ? (
              <Button variant="danger" size="sm" icon={<Square size={13} />} onClick={stop}>Stop</Button>
            ) : (
              <Button variant="primary" size="sm" icon={<Send size={14} />} onClick={send} loading={sending} disabled={!draft.trim()}>
                Send
              </Button>
            )}
          </div>
        </div>
        <div className="composer-foot">
          <span>{defaultModel?.model}</span>
          <span>·</span>
          <span>Secrets never enter the prompt</span>
          {pending.length > 0 && <Badge tone="warn">{pending.length} awaiting approval</Badge>}
        </div>
      </div>
    </div>
  )
}

function ChatMessage({ message, profileName }: { message: Message; profileName?: string }) {
  const c = message.content || {}
  if (message.role === 'user') {
    return (
      <div className="message user-message">
        <div className="message-body">
          <div className="user-bubble">{c.text}</div>
          <MessageMeta time={message.created_at} />
        </div>
        <div className="avatar user-avatar"><User size={15} /></div>
      </div>
    )
  }
  if (message.role !== 'assistant') return null
  const parts = (c.parts || []).filter((p) => p.type === 'tool-call')
  if (!c.text && parts.length === 0) return null
  return (
    <div className="message assistant-message">
      <div className="avatar assistant-avatar"><Bot size={16} /></div>
      <div className="message-body">
        <div className="assistant-name">{profileName || 'Agent'}</div>
        <div className="assistant-card">
          {c.text && <Markdown>{c.text}</Markdown>}
          {parts.length > 0 && (
            <div className="message-tools">
              {parts.map((p) => (
                <ToolCard
                  key={p.tool_call_id || `${p.name}-${message.id}`}
                  tool={{
                    key: p.tool_call_id || `${p.name}-${message.id}`,
                    name: p.name || 'tool',
                    args: p.args,
                    status: p.status || 'succeeded',
                    result: p.result,
                    error: p.error,
                    duration_ms: p.duration_ms,
                    logs: p.logs,
                  }}
                />
              ))}
            </div>
          )}
        </div>
        <MessageMeta
          time={message.created_at}
          tokens={(message.tokens_in || 0) + (message.tokens_out || 0)}
          cost={message.cost_micro}
        />
      </div>
    </div>
  )
}
