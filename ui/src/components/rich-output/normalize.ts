function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

function looksLikeElement(value: unknown): value is Record<string, unknown> {
  return isRecord(value) && typeof value.type === 'string'
}

/**
 * Return the end index of the JSON object starting at `start`, or -1 when the
 * braces are unbalanced. String contents and escapes are skipped so braces in
 * captions and markdown do not confuse the scan.
 */
function objectEnd(text: string, start: number): number {
  if (text[start] !== '{') return -1
  let depth = 0
  let inString = false
  let escaped = false
  for (let i = start; i < text.length; i++) {
    const char = text[i]
    if (inString) {
      if (escaped) escaped = false
      else if (char === '\\') escaped = true
      else if (char === '"') inString = false
      continue
    }
    if (char === '"') inString = true
    else if (char === '{') depth++
    else if (char === '}') {
      depth--
      if (depth === 0) return i
    }
  }
  return -1
}

/**
 * Models occasionally repeat an element id inside `elements`, usually when they
 * mean `children: []` but write `"<id>": []` after the element instead. JSON
 * keeps only the last duplicate, so the real element is lost. Recover the first
 * complete element object for that id from the raw block.
 */
function recoverElement(raw: string, id: string): Record<string, unknown> | undefined {
  const needle = `${JSON.stringify(id)}:`
  let from = 0
  while (from < raw.length) {
    const index = raw.indexOf(needle, from)
    if (index < 0) return undefined
    from = index + needle.length
    let start = from
    while (start < raw.length && /\s/.test(raw[start])) start++
    if (raw[start] !== '{') continue
    const end = objectEnd(raw, start)
    if (end < 0) continue
    try {
      const value: unknown = JSON.parse(raw.slice(start, end + 1))
      if (looksLikeElement(value)) return value
    } catch {
      // Try the next occurrence.
    }
  }
  return undefined
}

/**
 * `children` is required by the json-render spec schema, but models routinely
 * omit it on leaves. Fill it in so an otherwise valid output still renders.
 * Also repair elements that lost a duplicate-key race in the raw JSON.
 */
export function normalizeSpec(spec: unknown, raw?: string): unknown {
  if (!isRecord(spec) || !isRecord(spec.elements)) return spec
  const elements = Object.fromEntries(
    Object.entries(spec.elements).map(([key, element]) => {
      let value = element
      if (!looksLikeElement(element) && raw) {
        const recovered = recoverElement(raw, key)
        if (recovered) value = recovered
      }
      if (!isRecord(value)) return [key, value]
      return [
        key,
        {
          ...value,
          props: isRecord(value.props) ? value.props : {},
          children: Array.isArray(value.children) ? value.children : [],
        },
      ]
    }),
  )
  return { ...spec, elements }
}
