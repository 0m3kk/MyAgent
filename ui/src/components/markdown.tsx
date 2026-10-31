import { lazy, Suspense, type ReactNode } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import clsx from 'clsx'
import { ExternalLink, Loader2 } from 'lucide-react'
import { CopyButton } from './ui'

const RichOutput = lazy(() => import('./rich-output').then((mod) => ({ default: mod.RichOutput })))

function RichPending() {
  return (
    <div className="rich-output-pending">
      <Loader2 size={13} className="spin" />
      <span>Rendering visual output…</span>
    </div>
  )
}

export function Markdown({
  children,
  className,
  streaming,
}: {
  children: string
  className?: string
  streaming?: boolean
}) {
  const content = streaming ? splitUnclosedRichBlock(children) : { text: children, pending: false }
  return (
    <div className={clsx('markdown', className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          a: ({ href, children }) => (
            <a href={href} target="_blank" rel="noreferrer">
              {children} <ExternalLink size={11} className="inline-icon" />
            </a>
          ),
          pre: ({ children }) => {
            const text = extractText(children)
            const language = codeLanguage(children)
            if (language === 'ui') {
              return (
                <Suspense fallback={<RichPending />}>
                  <RichOutput spec={parseRichSpec(text)} raw={text} />
                </Suspense>
              )
            }
            return (
              <div className="code-wrap">
                <div className="code-head"><span>{language || 'code'}</span><CopyButton text={text} /></div>
                <pre>{children}</pre>
              </div>
            )
          },
        }}
      >
        {content.text}
      </ReactMarkdown>
      {content.pending && <RichPending />}
    </div>
  )
}

function parseRichSpec(raw: string): unknown {
  try {
    return JSON.parse(raw)
  } catch {
    return undefined
  }
}

function extractText(node: ReactNode): string {
  if (typeof node === 'string' || typeof node === 'number') return String(node)
  if (Array.isArray(node)) return node.map(extractText).join('')
  if (node && typeof node === 'object' && 'props' in node) {
    return extractText((node as { props?: { children?: ReactNode } }).props?.children)
  }
  return ''
}

function codeLanguage(node: ReactNode): string | undefined {
  const child = Array.isArray(node) ? node[0] : node
  if (!child || typeof child !== 'object' || !('props' in child)) return undefined
  const className = (child as { props?: { className?: unknown } }).props?.className
  if (typeof className !== 'string') return undefined
  return className.match(/language-([\w-]+)/)?.[1]?.toLowerCase()
}

/**
 * A rich block is only valid after its closing fence arrives. While the
 * response is streaming, hide the unfinished JSON and show the placeholder
 * instead of flashing a raw, half-written spec.
 */
function splitUnclosedRichBlock(text: string): { text: string; pending: boolean } {
  let open = -1
  let marker = ''
  for (const candidate of ['```ui', '~~~ui']) {
    const index = text.lastIndexOf(candidate)
    if (index > open) {
      open = index
      marker = candidate.slice(0, 3)
    }
  }
  if (open < 0 || !marker) return { text, pending: false }
  if (text.slice(open + 5).includes(marker)) return { text, pending: false }
  return { text: text.slice(0, open).trimEnd(), pending: true }
}
