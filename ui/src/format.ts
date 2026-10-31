export function formatRelative(ms?: number): string {
  if (!ms) return 'never'
  const diff = Date.now() - ms
  const abs = Math.abs(diff)
  const minute = 60_000
  const hour = 60 * minute
  const day = 24 * hour
  if (abs < 45_000) return diff >= 0 ? 'just now' : 'in a moment'
  if (abs < hour) return `${Math.round(abs / minute)}m ${diff >= 0 ? 'ago' : 'from now'}`
  if (abs < day) return `${Math.round(abs / hour)}h ${diff >= 0 ? 'ago' : 'from now'}`
  if (abs < 7 * day) return `${Math.round(abs / day)}d ${diff >= 0 ? 'ago' : 'from now'}`
  return new Date(ms).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}

export function versionLabel(v?: { version: number; status?: string } | null): string {
  if (!v) return ''
  return v.status === 'draft' ? 'draft' : `v${v.version}`
}

export function formatDate(ms?: number): string {
  if (!ms) return '—'
  return new Date(ms).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function formatTime(ms?: number): string {
  if (!ms) return '—'
  return new Date(ms).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

export function formatDuration(start?: number, end?: number): string {
  if (!start) return '—'
  const ms = (end || Date.now()) - start
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`
  const min = Math.floor(ms / 60_000)
  const sec = Math.round((ms % 60_000) / 1000)
  return `${min}m ${sec}s`
}

export function formatCost(micro: number): string {
  const usd = (micro || 0) / 1_000_000
  if (usd === 0) return '$0.00'
  if (usd < 0.01) return `$${usd.toFixed(4)}`
  return `$${usd.toFixed(2)}`
}

export function formatNumber(n?: number): string {
  return new Intl.NumberFormat().format(n || 0)
}

export function shortId(id?: string): string {
  if (!id) return '—'
  return id.length > 10 ? id.slice(0, 8) : id
}

export function titleCase(s: string): string {
  return s.replace(/[_-]/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase())
}

export function statusTone(status?: string): string {
  switch (status) {
    case 'succeeded':
    case 'active':
    case 'approved':
    case 'present':
    case 'accepted':
      return 'ok'
    case 'running':
    case 'queued':
    case 'testing':
    case 'tested':
      return 'run'
    case 'waiting_approval':
    case 'waiting_secret':
    case 'proposed':
    case 'draft':
      return 'warn'
    case 'failed':
    case 'expired':
    case 'rejected':
    case 'missing':
    case 'rolled_back':
      return 'bad'
    case 'paused':
    case 'skipped':
    case 'deprecated':
      return 'muted'
    default:
      return 'neutral'
  }
}

export function copyText(text: string) {
  if (navigator.clipboard) return navigator.clipboard.writeText(text)
  return Promise.resolve()
}

export function safeJSON(value: unknown, max = 4000): string {
  if (value === undefined) return ''
  try {
    const s = typeof value === 'string' ? value : JSON.stringify(value, null, 2)
    return s.length > max ? `${s.slice(0, max)}\n…` : s
  } catch {
    return String(value)
  }
}

export function parseJSON(text: string): unknown {
  try {
    return JSON.parse(text)
  } catch {
    return undefined
  }
}
