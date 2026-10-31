import type { ReactNode } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import clsx from 'clsx'
import { ExternalLink } from 'lucide-react'
import { CopyButton } from './ui'

export function Markdown({ children, className }: { children: string; className?: string }) {
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
            return (
              <div className="code-wrap">
                <div className="code-head"><span>code</span><CopyButton text={text} /></div>
                <pre>{children}</pre>
              </div>
            )
          },
        }}
      >
        {children}
      </ReactMarkdown>
    </div>
  )
}

function extractText(node: ReactNode): string {
  if (typeof node === 'string' || typeof node === 'number') return String(node)
  if (Array.isArray(node)) return node.map(extractText).join('')
  if (node && typeof node === 'object' && 'props' in node) {
    return extractText((node as { props?: { children?: ReactNode } }).props?.children)
  }
  return ''
}
