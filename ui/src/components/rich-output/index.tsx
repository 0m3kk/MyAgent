import { useMemo } from 'react'
import { CircleAlert } from 'lucide-react'
import type { Spec } from '@json-render/core'
import { JSONUIProvider, Renderer } from '@json-render/react'
import { CopyButton } from '../ui'
import { richCatalog } from './catalog'
import { normalizeSpec } from './normalize'
import { richRegistry } from './registry'

function rawSpec(spec: unknown, raw?: string) {
  if (raw) return raw
  try {
    return JSON.stringify(spec, null, 2)
  } catch {
    return String(spec)
  }
}

export function RichOutput({ spec, raw }: { spec: unknown; raw?: string }) {
  const normalized = useMemo(() => normalizeSpec(spec, raw), [spec, raw])
  const validation = useMemo(() => richCatalog.validate(normalized), [normalized])
  const text = rawSpec(spec, raw)
  if (!validation.success) {
    const issues = spec === undefined
      ? ['The ui block is not valid JSON.']
      : validation.error?.issues?.map((issue) => issue.message).slice(0, 3) ?? []
    return (
      <div className="rich-output-error">
        <div className="rich-output-error-head">
          <CircleAlert size={14} />
          <span>Visual output could not be rendered</span>
          <CopyButton text={text} />
        </div>
        <details>
          <summary>Show raw output</summary>
          {issues.length > 0 && <div className="rich-output-error-detail">{issues.join(' · ')}</div>}
          <pre className="json-block small">{text}</pre>
        </details>
      </div>
    )
  }
  const parsed = validation.data as Spec | undefined
  if (!parsed) return null
  return (
    <div className="rich-output">
      <JSONUIProvider registry={richRegistry}>
        <Renderer
          spec={parsed}
          registry={richRegistry}
          fallback={({ element }) => (
            <div className="rich-unknown">Unsupported block: {String(element.type)}</div>
          )}
        />
      </JSONUIProvider>
    </div>
  )
}
