export function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

export function parseRichSpec(raw: string): unknown {
  try {
    return JSON.parse(raw)
  } catch {
    return undefined
  }
}

/**
 * True when a parsed value has the shape of a json-render spec. The catalog
 * still validates every prop before rendering; this only decides whether a
 * blob of JSON is worth handing to the renderer.
 */
export function looksLikeRichSpec(value: unknown): boolean {
  if (!isRecord(value) || typeof value.root !== 'string' || !isRecord(value.elements)) {
    return false
  }
  const root = value.elements[value.root]
  return isRecord(root) && typeof root.type === 'string'
}

/**
 * Return the end index of the JSON object starting at `start`, or -1 when the
 * braces are unbalanced. String contents and escapes are skipped so braces in
 * captions and markdown do not confuse the scan.
 */
export function objectEnd(text: string, start: number): number {
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

interface Fence {
  char: string
  length: number
  info: string
}

const FENCE_OPEN = /^ {0,3}(`{3,}|~{3,})(.*)$/
const FENCE_CLOSE = /^ {0,3}(`{3,}|~{3,})\s*$/

function fenceOpen(line: string): Fence | undefined {
  const match = line.match(FENCE_OPEN)
  if (!match) return undefined
  return { char: match[1][0], length: match[1].length, info: match[2].trim() }
}

function isFenceClose(line: string, char: string, length: number): boolean {
  const match = line.match(FENCE_CLOSE)
  return !!match && match[1][0] === char && match[1].length >= length
}

/** Index just past the closing fence line, or the end of the text. */
function skipFence(text: string, lineStart: number, fence: Fence): number {
  let lineEnd = text.indexOf('\n', lineStart)
  let i = lineEnd === -1 ? text.length : lineEnd + 1
  while (i < text.length) {
    lineEnd = text.indexOf('\n', i)
    const next = lineEnd === -1 ? text.length : lineEnd + 1
    if (isFenceClose(text.slice(i, lineEnd === -1 ? text.length : lineEnd), fence.char, fence.length)) {
      return next
    }
    i = next
  }
  return text.length
}

/**
 * Models sometimes tag a spec as ```json, or forget the fence entirely. Promote
 * those blocks to ```ui so markdown.tsx renders them like a normal ui block.
 */
export function promoteRichBlocks(text: string): string {
  return wrapBareSpecs(promoteFenced(text))
}

function promoteFenced(text: string): string {
  const lines = text.split('\n')
  for (let i = 0; i < lines.length; i++) {
    const fence = fenceOpen(lines[i])
    if (!fence) continue
    let j = i + 1
    while (j < lines.length && !isFenceClose(lines[j], fence.char, fence.length)) j++
    if (j >= lines.length) break
    const language = fence.info.split(/\s+/)[0]?.toLowerCase() ?? ''
    if (language !== 'ui' && (language === '' || language === 'json' || language === 'jsonc')) {
      const body = lines.slice(i + 1, j).join('\n')
      if (looksLikeRichSpec(parseRichSpec(body))) {
        const marker = lines[i].match(FENCE_OPEN)?.[1] ?? ''
        const indent = lines[i].slice(0, lines[i].indexOf(marker))
        lines[i] = `${indent}${marker}ui`
      }
    }
    i = j
  }
  return lines.join('\n')
}

function wrapBareSpecs(text: string): string {
  let out = ''
  let cursor = 0
  let i = 0
  while (i < text.length) {
    if (i > 0 && text[i - 1] !== '\n') {
      i++
      continue
    }
    const lineEnd = text.indexOf('\n', i)
    const line = text.slice(i, lineEnd === -1 ? text.length : lineEnd)
    const fence = fenceOpen(line)
    if (fence) {
      const next = skipFence(text, i, fence)
      out += text.slice(cursor, next)
      cursor = next
      i = next
      continue
    }
    const start = i + (line.length - line.trimStart().length)
    if (text[start] === '{') {
      const close = objectEnd(text, start)
      if (close > start && looksLikeRichSpec(parseRichSpec(text.slice(start, close + 1)))) {
        const before = text.slice(cursor, start).replace(/[ \t]+$/, '')
        out += before
        if (before && !before.endsWith('\n')) out += '\n'
        out += '```ui\n' + text.slice(start, close + 1) + '\n```'
        if (close + 1 < text.length && text[close + 1] !== '\n') out += '\n'
        cursor = close + 1
        i = close + 1
        continue
      }
    }
    i = lineEnd === -1 ? text.length : lineEnd + 1
  }
  out += text.slice(cursor)
  return out
}

/**
 * While a response streams, a bare spec is still being written and must not
 * flash as raw JSON. Return its start offset when it is recognizably a spec but
 * the closing brace has not arrived yet.
 */
export function unclosedSpecStart(text: string): number {
  let i = 0
  while (i < text.length) {
    if (i > 0 && text[i - 1] !== '\n') {
      i++
      continue
    }
    const lineEnd = text.indexOf('\n', i)
    const line = text.slice(i, lineEnd === -1 ? text.length : lineEnd)
    const fence = fenceOpen(line)
    if (fence) {
      i = skipFence(text, i, fence)
      continue
    }
    const start = i + (line.length - line.trimStart().length)
    if (text[start] === '{') {
      const close = objectEnd(text, start)
      if (close < 0) {
        const partial = text.slice(start)
        if (/"root"\s*:/.test(partial) && /"elements"\s*:/.test(partial)) return start
        return -1
      }
      i = close + 1
      continue
    }
    i = lineEnd === -1 ? text.length : lineEnd + 1
  }
  return -1
}
