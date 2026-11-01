import { statusTone, titleCase } from './format'

export type ToolTone = 'ok' | 'bad' | 'warn' | 'run' | 'neutral' | 'muted'

export interface ToolSummary {
  /** Human-friendly action label, e.g. "Validate workflow". */
  title: string
  /** The concrete target of the call, e.g. "daily-report" or "src/main.go". */
  target?: string
  /** Full target when `target` had to be clipped. */
  targetFull?: string
  /** Extra context, e.g. "v3 · 12 steps". */
  detail?: string
}

export interface ToolOutcome {
  text: string
  tone: ToolTone
}

type Args = Record<string, unknown>

function isRecord(value: unknown): value is Args {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

function text(value: unknown): string | undefined {
  if (typeof value === 'string') {
    const out = value.trim()
    return out || undefined
  }
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return undefined
}

function integer(value: unknown): number | undefined {
  if (typeof value === 'number' && Number.isFinite(value)) return Math.trunc(value)
  if (typeof value === 'string' && /^\d+$/.test(value.trim())) return Number(value)
  return undefined
}

function count(value: number, singular: string, plural = `${singular}s`) {
  return `${value} ${value === 1 ? singular : plural}`
}

function shortId(value?: string) {
  if (!value) return undefined
  return value.length > 10 ? `${value.slice(0, 8)}…` : value
}

function target(raw?: string, max = 90): Pick<ToolSummary, 'target' | 'targetFull'> {
  if (!raw) return {}
  const clean = raw.replace(/\s+/g, ' ').trim()
  if (!clean) return {}
  if (clean.length <= max) return { target: clean }
  return { target: `${clean.slice(0, max - 1)}…`, targetFull: clean }
}

function detail(...parts: Array<string | undefined | false>): string | undefined {
  const out = parts.filter((part): part is string => typeof part === 'string' && part.length > 0)
  return out.length > 0 ? out.join(' · ') : undefined
}

/** Read a scalar field from the document root, ignoring nested fields with the same name. */
function yamlTopLevel(source: string, key: string): string | undefined {
  if (!source) return undefined
  const match = source.match(new RegExp(`^${key}:[ \\t]*(.*)$`, 'm'))
  if (!match) return undefined
  let value = match[1].trim()
  if (!value || value.startsWith('#')) return undefined
  if (!value.startsWith('"') && !value.startsWith("'")) value = value.replace(/\s+#.*$/, '').trim()
  if (
    (value.startsWith('"') && value.endsWith('"')) ||
    (value.startsWith("'") && value.endsWith("'"))
  ) {
    value = value.slice(1, -1).trim()
  }
  return value || undefined
}

/** Count list items directly under the top-level `steps:` key. */
function yamlStepCount(source: string): number | undefined {
  if (!source) return undefined
  if (/^steps:[ \t]*\[[ \t]*\][ \t]*$/m.test(source)) return 0
  const lines = source.split(/\r?\n/)
  const start = lines.findIndex((line) => /^steps:[ \t]*$/.test(line))
  if (start < 0) return undefined
  let itemIndent = -1
  let steps = 0
  for (let i = start + 1; i < lines.length; i++) {
    const line = lines[i]
    if (!line.trim() || line.trimStart().startsWith('#')) continue
    if (!line.startsWith(' ') && !line.startsWith('\t')) break
    const item = line.match(/^([ \t]*)-[ \t]+/)
    if (!item) continue
    const indent = item[1].length
    if (itemIndent < 0) itemIndent = indent
    if (indent < itemIndent) break
    if (indent === itemIndent) steps++
  }
  return steps
}

function workflowSummary(source: string): { name?: string; steps?: number } {
  return { name: yamlTopLevel(source, 'name'), steps: yamlStepCount(source) }
}

function folderText(value: unknown): string | undefined {
  if (Array.isArray(value)) {
    const parts = value.map((part) => text(part)).filter((part): part is string => !!part)
    return parts.length > 0 ? parts.join(' / ') : undefined
  }
  return text(value)
}

function fallbackTarget(args: Args): string | undefined {
  const preferred = [
    'path',
    'file',
    'url',
    'query',
    'name',
    'workflow',
    'tool',
    'run_id',
    'suite',
    'command',
    'question',
    'note',
  ]
  const entries = Object.entries(args).filter(([key]) => !['content', 'yaml', 'manifest_yaml'].includes(key))
  const ordered = [
    ...preferred.flatMap((key) => entries.filter(([entryKey]) => entryKey === key)),
    ...entries.filter(([key]) => !preferred.includes(key)),
  ]
  for (const [key, value] of ordered) {
    const out = text(value)
    if (out) return `${key}: ${out}`
  }
  return undefined
}

function humanName(name: string) {
  return name.includes('.') ? name : titleCase(name)
}

export function summarizeToolCall(name: string, rawArgs: unknown): ToolSummary {
  const args = isRecord(rawArgs) ? rawArgs : {}
  const s = (key: string) => text(args[key])
  const version = integer(args.version)

  switch (name) {
    case 'validate_workflow': {
      const yaml = s('yaml') ?? ''
      const wf = workflowSummary(yaml)
      return {
        title: 'Validate workflow',
        ...target(wf.name ?? 'untitled workflow'),
        detail: detail(wf.steps !== undefined ? count(wf.steps, 'step') : undefined),
      }
    }
    case 'save_workflow_draft': {
      const yaml = s('yaml') ?? ''
      const wf = workflowSummary(yaml)
      const base = integer(args.base_version)
      const folder = folderText(args.folder)
      return {
        title: 'Save workflow draft',
        ...target(wf.name ?? 'untitled workflow'),
        detail: detail(
          wf.steps !== undefined ? count(wf.steps, 'step') : undefined,
          base ? `from v${base}` : undefined,
          folder ? `in ${folder}` : undefined,
        ),
      }
    }
    case 'dry_run_workflow':
    case 'run_workflow': {
      const inputs = isRecord(args.inputs) ? Object.keys(args.inputs).length : undefined
      return {
        title: name === 'run_workflow' ? 'Run workflow' : 'Dry-run workflow',
        ...target(s('name') ?? s('workflow') ?? 'untitled workflow'),
        detail: detail(version ? `v${version}` : undefined, inputs !== undefined ? count(inputs, 'input') : undefined),
      }
    }
    case 'get_workflow':
      return {
        title: 'Load workflow',
        ...target(s('name')),
        detail: version ? `v${version}` : undefined,
      }
    case 'list_workflows':
      return { title: 'List workflows' }
    case 'list_folders':
      return { title: 'List folders', ...target(s('kind')) }
    case 'search_tools':
      return { title: 'Search tools', ...target(s('query')) }
    case 'describe_tool':
      return {
        title: 'Describe tool',
        ...target(s('name')),
        detail: version ? `v${version}` : undefined,
      }
    case 'create_tool_draft': {
      const manifest = s('manifest_yaml') ?? ''
      const kind = yamlTopLevel(manifest, 'kind')
      const folder = folderText(args.folder)
      return {
        title: 'Create tool draft',
        ...target(yamlTopLevel(manifest, 'name') ?? 'untitled tool'),
        detail: detail(kind, folder ? `in ${folder}` : undefined),
      }
    }
    case 'write_tool_file': {
      const content = s('content')
      return {
        title: 'Write tool file',
        ...target(s('path')),
        detail: detail(s('tool'), content ? `${content.length} chars` : undefined),
      }
    }
    case 'read_tool_file':
      return { title: 'Read tool file', ...target(s('path')), detail: s('tool') }
    case 'list_tool_files':
      return { title: 'List tool files', ...target(s('tool')) }
    case 'run_tool_tests':
      return { title: 'Run tool tests', ...target(s('tool')) }
    case 'probe_permissions':
      return { title: 'Probe permissions', ...target(s('tool')) }
    case 'request_tool_approval':
      return { title: 'Request tool approval', ...target(s('tool')) }
    case 'get_run':
      return { title: 'Inspect run', ...target(shortId(s('run_id'))) }
    case 'read_run':
      return { title: 'Read run events', ...target(shortId(s('run_id'))) }
    case 'wait_run': {
      const timeout = integer(args.timeout_s)
      return {
        title: 'Wait for run',
        ...target(shortId(s('run_id'))),
        detail: timeout ? `up to ${timeout}s` : undefined,
      }
    }
    case 'compare_runs': {
      const a = shortId(s('run_a'))
      const b = shortId(s('run_b'))
      return { title: 'Compare runs', ...target(a && b ? `${a} ↔ ${b}` : a ?? b) }
    }
    case 'list_runs':
      return {
        title: 'List runs',
        ...target(s('workflow')),
        detail: s('status') ? `status ${s('status')}` : undefined,
      }
    case 'list_secrets':
      return { title: 'List secrets' }
    case 'get_environment':
      return { title: 'Check environment' }
    case 'ask_user':
      return { title: 'Ask user', ...target(s('question'), 70) }
    case 'remember':
      return { title: 'Save note', ...target(s('note'), 80) }
    case 'propose_trigger': {
      const config = isRecord(args.config) ? args.config : {}
      return {
        title: 'Propose trigger',
        ...target(s('workflow')),
        detail: detail(s('kind'), text(config.schedule)),
      }
    }
    case 'fs.list':
      return { title: 'List files', ...target(s('path') ?? '.') }
    case 'fs.read_text':
      return { title: 'Read file', ...target(s('path')) }
    case 'fs.write_text': {
      const content = s('content')
      return {
        title: 'Write file',
        ...target(s('path')),
        detail: content ? `${content.length} chars` : undefined,
      }
    }
    case 'fs.stat':
      return { title: 'File info', ...target(s('path')) }
    case 'fs.delete':
      return { title: 'Delete file', ...target(s('path')) }
    case 'http.fetch':
      return {
        title: 'Fetch URL',
        ...target(s('url')),
        detail: s('method')?.toUpperCase(),
      }
    case 'shell.exec': {
      const argv = Array.isArray(args.args)
        ? args.args.map((part) => text(part)).filter((part): part is string => !!part).join(' ')
        : ''
      const cwd = s('cwd')
      return {
        title: 'Run command',
        ...target([s('command'), argv].filter((part): part is string => !!part).join(' '), 110),
        detail: cwd && cwd !== '.' ? `in ${cwd}` : undefined,
      }
    }
    case 'text.stats':
      return { title: 'Text stats', ...target(s('text'), 70) }
    case 'workspace.info':
      return { title: 'Workspace info' }
    default:
      return { title: humanName(name), ...target(fallbackTarget(args)) }
  }
}

function errorsAndWarnings(result: Args) {
  const errors = Array.isArray(result.diagnostics) ? result.diagnostics.length : undefined
  const warnings = Array.isArray(result.warnings) ? result.warnings.length : undefined
  return { errors, warnings }
}

function array(value: unknown): unknown[] | undefined {
  return Array.isArray(value) ? value : undefined
}

export function toolCallOutcome(name: string, rawResult: unknown): ToolOutcome | undefined {
  const result = isRecord(rawResult) ? rawResult : undefined
  if (!result) return undefined

  switch (name) {
    case 'validate_workflow': {
      const { errors, warnings } = errorsAndWarnings(result)
      if (result.ok === false || (errors ?? 0) > 0) {
        return errors ? { text: count(errors, 'error'), tone: 'bad' } : { text: 'invalid', tone: 'bad' }
      }
      if (warnings) return { text: `valid · ${count(warnings, 'warning')}`, tone: 'warn' }
      return { text: 'valid', tone: 'ok' }
    }
    case 'save_workflow_draft': {
      const { errors } = errorsAndWarnings(result)
      if (result.saved === false) {
        return errors ? { text: count(errors, 'error'), tone: 'bad' } : { text: 'not saved', tone: 'bad' }
      }
      if (result.saved === true) {
        const version = integer(result.version)
        return { text: version ? `saved v${version}` : 'saved', tone: 'ok' }
      }
      return undefined
    }
    case 'run_tool_tests': {
      const tests = array(result.tests)
      if (!tests) return undefined
      if (tests.length === 0) return { text: 'no tests', tone: 'warn' }
      const passed = tests.filter((item) => isRecord(item) && item.passed === true).length
      return {
        text: `${passed}/${tests.length} passed`,
        tone: passed === tests.length ? 'ok' : 'bad',
      }
    }
    case 'probe_permissions': {
      const denied = array(result.denied)
      if (!denied) return undefined
      return denied.length > 0
        ? { text: count(denied.length, 'denial'), tone: 'warn' }
        : { text: 'no denials', tone: 'ok' }
    }
    case 'run_workflow':
    case 'dry_run_workflow':
    case 'get_run':
    case 'wait_run':
    case 'read_run': {
      const runStatus = text(result.status)
      if (!runStatus) return undefined
      const label = runStatus.replace(/_/g, ' ')
      const textValue = label === 'running' ? 'run in progress' : `run ${label}`
      return { text: textValue, tone: statusTone(runStatus) as ToolTone }
    }
    case 'list_workflows': {
      const items = array(result.workflows)
      return items ? { text: count(items.length, 'workflow'), tone: 'neutral' } : undefined
    }
    case 'list_folders': {
      const items = array(result.folders)
      return items ? { text: count(items.length, 'folder'), tone: 'neutral' } : undefined
    }
    case 'search_tools': {
      const items = array(result.tools)
      return items ? { text: `${items.length} found`, tone: 'neutral' } : undefined
    }
    case 'list_runs': {
      const items = array(result.runs)
      return items ? { text: count(items.length, 'run'), tone: 'neutral' } : undefined
    }
    case 'list_secrets': {
      const items = array(result.secrets)
      return items ? { text: count(items.length, 'secret'), tone: 'neutral' } : undefined
    }
    case 'list_tool_files': {
      const items = array(result.files)
      return items ? { text: count(items.length, 'file'), tone: 'neutral' } : undefined
    }
    default:
      return undefined
  }
}
