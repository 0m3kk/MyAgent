import { useMemo } from 'react'
import { CircleAlert } from 'lucide-react'
import type { Spec } from '@json-render/core'
import { JSONUIProvider, Renderer } from '@json-render/react'
import { CopyButton } from '../ui'
import { richCatalog } from './catalog'
import { richRegistry } from './registry'

function rawSpec(spec: unknown, raw?: string) {
  if (raw) return raw
  try {
    return JSON.stringify(spec, null, 2)
  } catch {
    return String(spec)
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

/**
 * `children` is required by the json-render spec schema, but models routinely
 * omit it on leaves. Fill it in so an otherwise valid output still renders.
 */
function normalizeSpec(spec: unknown): unknown {
  if (!isRecord(spec) || !isRecord(spec.elements)) return spec
  const elements = Object.fromEntries(
    Object.entries(spec.elements).map(([key, element]) => {
      if (!isRecord(element)) return [key, element]
      return [
        key,
        {
          ...element,
          props: isRecord(element.props) ? element.props : {},
          children: Array.isArray(element.children) ? element.children : [],
        },
      ]
    }),
  )
  return { ...spec, elements }
}

export function RichOutput({ spec, raw }: { spec: unknown; raw?: string }) {
  const normalized = useMemo(() => normalizeSpec(spec), [spec])
  const validation = useMemo(() => richCatalog.validate(normalized), [normalized])
  const text = rawSpec(spec, raw)
  if (!validation.success) {
    const issues = validation.error?.issues?.map((issue) => issue.message).slice(0, 3) ?? []
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
