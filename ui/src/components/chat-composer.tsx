import { useEffect, useRef, useState } from 'react'
import { Send } from 'lucide-react'
import { Button, TextArea } from './ui'

export function StartChatComposer({
  hasModel,
  model,
  onSend,
}: {
  hasModel: boolean
  model?: string
  onSend: (text: string) => Promise<boolean>
}) {
  const [draft, setDraft] = useState('')
  const [sending, setSending] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, 180)}px`
  }, [draft])

  async function send() {
    const text = draft.trim()
    if (!text || sending) return
    setDraft('')
    setSending(true)
    try {
      const ok = await onSend(text)
      if (!ok) setDraft(text)
    } finally {
      setSending(false)
      window.setTimeout(() => textareaRef.current?.focus(), 30)
    }
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      void send()
    }
  }

  return (
    <>
      <div className="composer">
        <TextArea
          ref={textareaRef}
          rows={1}
          placeholder={hasModel ? 'Ask anything, or describe a task to automate…' : 'Add a model in Settings to start chatting…'}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={onKeyDown}
          disabled={sending || !hasModel}
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
            onClick={send}
            loading={sending}
            disabled={!hasModel || sending || !draft.trim()}
          >
            Send
          </Button>
        </div>
      </div>
      <div className="composer-foot">
        <span>{model || 'No model selected'}</span>
        <span>·</span>
        <span>Secrets never enter the prompt</span>
      </div>
    </>
  )
}
