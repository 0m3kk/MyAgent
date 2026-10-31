import { ExternalLink, FileJson, Link2 } from 'lucide-react'
import { parseJSON, safeJSON, titleCase } from '../format'
import { Markdown } from './markdown'

const TEXT_KEYS = ['text', 'markdown', 'content', 'summary', 'report', 'answer', 'body', 'description']
const LINK_KEYS = ['url', 'href', 'link']
const ITEM_KEYS = ['items', 'results', 'links', 'sources', 'files', 'entries', 'data']

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

function isURL(value: string) {
  return /^https?:\/\/\S+$/i.test(value.trim())
}

function isMarkdown(value: string) {
  return /(^|\n)(#{1,6}\s|[-*]\s|\d+\.\s|>\s)|\*\*|\[[^\]]+\]\([^)]+\)|```/.test(value)
}

function isEmpty(value: unknown) {
  return value === undefined || value === null || (typeof value === 'string' && value.trim() === '')
}

function isLongText(value: unknown): value is string {
  return typeof value === 'string' && (value.length > 160 || value.includes('\n') || isMarkdown(value))
}

function isScalar(value: unknown) {
  return typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean'
}

function stringValue(value: unknown) {
  if (value === undefined || value === null) return ''
  return typeof value === 'string' ? value : String(value)
}

export function ResultBlock({ value, rawMax = 30000 }: { value: unknown; rawMax?: number }) {
  const raw = safeJSON(value, rawMax)
  const text = typeof value === 'string' ? value.trim() : ''
  const showRaw = typeof value !== 'string' || text.startsWith('{') || text.startsWith('[')
  return (
    <div className="result-block">
      <ResultView value={value} />
      {showRaw && (
        <details className="result-raw">
          <summary><FileJson size={12} /> Raw JSON</summary>
          <pre className="json-block">{raw}</pre>
        </details>
      )}
    </div>
  )
}

export function ResultView({ value, depth = 0 }: { value: unknown; depth?: number }) {
  if (isEmpty(value)) return <div className="result-empty">No output.</div>
  if (depth > 5) return <pre className="json-block">{safeJSON(value, 20000)}</pre>

  if (typeof value === 'string') {
    const text = value.trim()
    if (text.startsWith('{') || text.startsWith('[')) {
      const parsed = parseJSON(text)
      if (parsed !== undefined) return <ResultView value={parsed} depth={depth + 1} />
    }
    if (isURL(text)) return <ResultLink url={text} />
    if (isMarkdown(text)) return <Markdown>{text}</Markdown>
    return <div className="result-text">{text}</div>
  }
  if (typeof value === 'number' || typeof value === 'boolean') {
    return <div className="result-text">{String(value)}</div>
  }
  if (Array.isArray(value)) return <ResultList items={value} depth={depth} />
  if (isRecord(value)) return <ResultObject value={value} depth={depth} />
  return <pre className="json-block">{safeJSON(value, 20000)}</pre>
}

function ResultObject({ value, depth }: { value: Record<string, unknown>; depth: number }) {
  const entries = Object.entries(value).filter(([, item]) => !isEmpty(item))
  if (entries.length === 0) return <div className="result-empty">Empty object.</div>
  if (entries.length === 1) {
    const [key, item] = entries[0]
    if (TEXT_KEYS.includes(key) || ITEM_KEYS.includes(key) || key === 'result' || key === 'output') {
      return <ResultView value={item} depth={depth + 1} />
    }
    return (
      <div className="result-single">
        <div className="result-label">{titleCase(key)}</div>
        <ResultView value={item} depth={depth + 1} />
      </div>
    )
  }

  const linkKey = LINK_KEYS.find((key) => typeof value[key] === 'string' && isURL(value[key] as string))
  if (linkKey) return <ResultLinkCard item={value} />

  const textKey = TEXT_KEYS.find((key) => typeof value[key] === 'string' && (value[key] as string).trim())
  if (textKey) {
    const rest = entries.filter(([key]) => key !== textKey)
    return (
      <div className="result-object">
        <ResultView value={value[textKey]} depth={depth + 1} />
        {rest.length > 0 && <ResultFields entries={rest} depth={depth + 1} />}
      </div>
    )
  }

  const itemKey = ITEM_KEYS.find((key) => Array.isArray(value[key]) && (value[key] as unknown[]).length > 0)
  if (itemKey) {
    const rest = entries.filter(([key]) => key !== itemKey)
    return (
      <div className="result-object">
        <div className="result-label">{titleCase(itemKey)}</div>
        <ResultList items={value[itemKey] as unknown[]} depth={depth + 1} />
        {rest.length > 0 && <ResultFields entries={rest} depth={depth + 1} />}
      </div>
    )
  }

  return <ResultFields entries={entries} depth={depth} />
}

function ResultFields({ entries, depth }: { entries: [string, unknown][]; depth: number }) {
  const rows = entries.filter(([, value]) => isScalar(value) && !isLongText(value))
  const blocks = entries.filter(([, value]) => !isScalar(value) || isLongText(value))
  return (
    <div className="result-fields">
      {rows.length > 0 && (
        <div className="result-kv">
          {rows.map(([key, value]) => (
            <div className="result-kv-row" key={key}>
              <span>{titleCase(key)}</span>
              <span><ResultScalar value={value} /></span>
            </div>
          ))}
        </div>
      )}
      {blocks.map(([key, value]) => (
        <div className="result-field" key={key}>
          <div className="result-label">{titleCase(key)}</div>
          <ResultView value={value} depth={depth + 1} />
        </div>
      ))}
    </div>
  )
}

function ResultScalar({ value }: { value: unknown }) {
  if (typeof value === 'string') {
    const text = value.trim()
    if (isURL(text)) return <ResultLink url={text} />
    return <>{text}</>
  }
  return <>{String(value)}</>
}

function ResultList({ items, depth }: { items: unknown[]; depth: number }) {
  if (items.length === 0) return <div className="result-empty">Empty list.</div>
  const records = items.filter(isRecord)
  if (records.length === items.length && records.every(hasLink)) {
    return (
      <div className="result-links">
        {records.map((item, i) => <ResultLinkCard key={i} item={item} />)}
      </div>
    )
  }
  if (records.length === items.length) {
    const keys = tableKeys(records)
    if (keys.length >= 2) {
      return (
        <div className="result-table-wrap">
          <table className="result-table">
            <thead>
              <tr>{keys.map((key) => <th key={key}>{titleCase(key)}</th>)}</tr>
            </thead>
            <tbody>
              {records.map((item, i) => (
                <tr key={i}>
                  {keys.map((key) => <td key={key}><ResultTableCell value={item[key]} /></td>)}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )
    }
    return (
      <div className="result-cards">
        {records.map((item, i) => (
          <div className="result-card" key={i}>
            <ResultView value={item} depth={depth + 1} />
          </div>
        ))}
      </div>
    )
  }
  return (
    <ul className="result-list">
      {items.map((item, i) => <li key={i}><ResultView value={item} depth={depth + 1} /></li>)}
    </ul>
  )
}

function ResultLink({ url, label }: { url: string; label?: string }) {
  return (
    <a className="result-link" href={url} target="_blank" rel="noreferrer">
      <Link2 size={13} />
      <span>{label || url}</span>
      <ExternalLink size={11} />
    </a>
  )
}

function ResultLinkCard({ item }: { item: Record<string, unknown> }) {
  const url = stringValue(item.url ?? item.href ?? item.link)
  const title = stringValue(item.title ?? item.name ?? item.label) || url
  const description = stringValue(item.description ?? item.snippet ?? item.summary)
  return (
    <a className="result-link-card" href={url} target="_blank" rel="noreferrer">
      <span className="result-link-icon"><Link2 size={15} /></span>
      <span className="result-link-copy">
        <strong>{title}</strong>
        {description && <em>{description}</em>}
        <span className="mono">{url}</span>
      </span>
      <ExternalLink size={13} className="result-link-external" />
    </a>
  )
}

function ResultTableCell({ value }: { value: unknown }) {
  if (typeof value === 'string' && isURL(value)) return <ResultLink url={value} label={value} />
  return <>{stringValue(value)}</>
}

function hasLink(item: Record<string, unknown>) {
  return LINK_KEYS.some((key) => typeof item[key] === 'string' && isURL(item[key] as string))
}

function tableKeys(records: Record<string, unknown>[]) {
  const first = records[0]
  return Object.keys(first)
    .filter((key) => records.every((item) => isScalar(item[key]) && String(item[key] ?? '').length < 120))
    .slice(0, 5)
}
