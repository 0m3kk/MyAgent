import { useEffect, useRef, useState } from 'react'
import { eventURL } from './api'
import type { EventEnvelope } from './types'

export function useEventStream(
  stream: string | null,
  handler: (event: EventEnvelope) => void,
  opts: { live?: boolean; after?: number; enabled?: boolean } = {},
) {
  const handlerRef = useRef(handler)
  handlerRef.current = handler
  const { live = false, after = 0, enabled = true } = opts
  useEffect(() => {
    if (!stream || !enabled) return
    const es = new EventSource(eventURL(stream, { live, after }))
    const onMessage = (ev: MessageEvent) => {
      try {
        const data = JSON.parse(ev.data) as EventEnvelope
        handlerRef.current(data)
      } catch {
        // Ignore malformed frames.
      }
    }
    es.onmessage = onMessage
    const types = [
      'run.queued', 'run.started', 'run.paused', 'run.resumed', 'run.finished',
      'step.started', 'step.log', 'step.output.delta', 'step.retrying', 'step.finished', 'step.skipped',
      'llm.request', 'llm.delta', 'llm.finished',
      'tool.call.started', 'tool.call.finished', 'tool.call.log', 'tool.sandbox.denied',
      'approval.requested', 'approval.resolved',
      'secret.requested', 'secret.accessed', 'secret.redacted',
      'message.created', 'conversation.updated',
      'trigger.created', 'trigger.fired', 'trigger.skipped',
      'workflow.version.created', 'workflow.validation.failed', 'workflow.version.approved',
      'tool.version.created', 'tool.test.finished', 'tool.version.approved',
      'tuning.proposal.created', 'tuning.proposal.decided',
      'eval.case.finished', 'eval.run.finished', 'eval.case.created', 'eval.run.started',
      'feedback.created', 'note.created', 'app.started',
    ]
    for (const t of types) es.addEventListener(t, onMessage as EventListener)
    return () => es.close()
  }, [stream, live, after, enabled])
}

export function usePolling(fn: () => void | Promise<void>, ms: number, enabled = true) {
  const fnRef = useRef(fn)
  fnRef.current = fn
  useEffect(() => {
    if (!enabled) return
    const id = window.setInterval(() => void fnRef.current(), ms)
    return () => window.clearInterval(id)
  }, [ms, enabled])
}

export function useClickOutside<T extends HTMLElement>(onOutside: () => void) {
  const ref = useRef<T | null>(null)
  useEffect(() => {
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onOutside()
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [onOutside])
  return ref
}

export function useNow(interval = 1000) {
  const [now, setNow] = useState(Date.now())
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), interval)
    return () => window.clearInterval(id)
  }, [interval])
  return now
}

export function useAutoScroll<T extends HTMLElement>(dep: unknown) {
  const ref = useRef<T | null>(null)
  const pinned = useRef(true)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const onScroll = () => {
      pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < 90
    }
    el.addEventListener('scroll', onScroll, { passive: true })
    return () => el.removeEventListener('scroll', onScroll)
  }, [])
  useEffect(() => {
    const el = ref.current
    if (el && pinned.current) el.scrollTop = el.scrollHeight
  }, [dep])
  return ref
}
