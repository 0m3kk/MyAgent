import { useCallback, useMemo, useRef } from 'react'
import {
  Background,
  Handle,
  MarkerType,
  Position,
  ReactFlow,
  type Edge,
  type Node,
  type NodeProps,
  type OnNodesChange,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import clsx from 'clsx'
import {
  Bot,
  Boxes,
  Columns3,
  GitBranch,
  GitFork,
  List,
  ShieldCheck,
  Sparkles,
  Variable,
  Wrench,
} from 'lucide-react'
import type { Approval, Run, StepSpec, WorkflowSpec } from '../types'
import { Badge } from './ui'

type NodeData = {
  step: StepSpec
  status: string
  error?: string
  selected?: boolean
}

const icons: Record<string, React.ReactNode> = {
  tool: <Wrench size={14} />,
  llm: <Sparkles size={14} />,
  agent: <Bot size={14} />,
  set: <Variable size={14} />,
  branch: <GitFork size={14} />,
  map: <List size={14} />,
  parallel: <Columns3 size={14} />,
  approval: <ShieldCheck size={14} />,
  workflow: <Boxes size={14} />,
}

function stepKind(step: StepSpec): string {
  if (step.tool) return 'tool'
  if (step.llm) return 'llm'
  if (step.agent) return 'agent'
  if (step.set) return 'set'
  if (step.branch) return 'branch'
  if (step.map) return 'map'
  if (step.parallel) return 'parallel'
  if (step.approval) return 'approval'
  if (step.workflow) return 'workflow'
  return 'step'
}

function StepNode({ data }: NodeProps<Node<NodeData>>) {
  const { step, status, error, selected } = data as NodeData
  const kind = stepKind(step)
  const summary =
    kind === 'tool' ? step.tool :
    kind === 'llm' ? `llm · ${step.llm?.role || 'executor'}` :
    kind === 'agent' ? `agent · max ${step.agent?.max_steps || 8}` :
    kind === 'map' ? `over ${String(step.map?.over || '').replace(/^\$\{|\}$/g, '')}` :
    kind === 'branch' ? `${step.branch?.cases?.length || 0} case(s)` :
    kind === 'approval' ? 'human approval' :
    kind === 'workflow' ? step.workflow :
    kind === 'parallel' ? `${step.parallel?.do?.length || 0} branches` :
    kind === 'set' ? `${Object.keys(step.set || {}).length} values` : ''
  return (
    <div className={clsx('wf-node', `wf-${status}`, selected && 'selected')}>
      <Handle type="target" position={Position.Top} />
      <div className="wf-node-top">
        <span className={clsx('wf-node-icon', `kind-${kind}`)}>{icons[kind] || <GitBranch size={14} />}</span>
        <span className="wf-node-id">{step.id}</span>
        <span className={clsx('wf-node-status', `st-${status}`)} />
      </div>
      <div className="wf-node-summary">{summary}</div>
      {error && <div className="wf-node-error" title={error}>{error}</div>}
      {step.needs && step.needs.length > 0 && (
        <div className="wf-node-needs">after {step.needs.join(', ')}</div>
      )}
      <Handle type="source" position={Position.Bottom} />
    </div>
  )
}

const nodeTypes = { step: StepNode }

function levelsFor(steps: StepSpec[]): Map<string, number> {
  const byId = new Map(steps.map((s) => [s.id, s]))
  const levels = new Map<string, number>()
  const visiting = new Set<string>()
  function visit(id: string): number {
    if (levels.has(id)) return levels.get(id)!
    if (visiting.has(id)) return 0
    visiting.add(id)
    const step = byId.get(id)
    let level = 0
    for (const need of step?.needs || []) {
      if (byId.has(need)) level = Math.max(level, visit(need) + 1)
    }
    visiting.delete(id)
    levels.set(id, level)
    return level
  }
  for (const s of steps) visit(s.id)
  return levels
}

export function WorkflowCanvas({
  spec,
  run,
  approvals = [],
  selectedStep,
  onSelectStep,
  runOutputs,
  compact,
}: {
  spec: WorkflowSpec
  run?: Run | null
  approvals?: Approval[]
  selectedStep?: string
  onSelectStep?: (id: string) => void
  runOutputs?: Record<string, unknown>
  compact?: boolean
}) {
  const steps = spec.steps || []
  const statusById = useMemo(() => {
    const map: Record<string, { status: string; error?: string }> = {}
    for (const s of steps) map[s.id] = { status: 'pending' }
    if (!run) return map
    const latest: Record<string, { status: string; error?: string; attempt: number }> = {}
    for (const sr of run.steps || []) {
      const top = sr.step_id.split(/[.[]/)[0]
      const cur = latest[top]
      if (!cur || sr.attempt >= cur.attempt) latest[top] = { status: sr.status, error: sr.error, attempt: sr.attempt }
    }
    for (const [id, value] of Object.entries(latest)) if (map[id]) map[id] = value
    if (run.status === 'waiting_approval') {
      for (const ap of approvals) {
        if (ap.status !== 'pending') continue
        const id = ap.request?.step_id || ''
        if (map[id]) map[id] = { status: 'waiting_approval', error: ap.request?.title }
      }
    }
    return map
  }, [steps, run, approvals])

  const nodeCache = useRef<Map<string, Node<NodeData>>>(new Map())
  const dimensions = useRef<Record<string, { width?: number; height?: number }>>({})
  const handleNodesChange = useCallback<OnNodesChange<Node<NodeData>>>((changes) => {
    for (const change of changes) {
      if (change.type === 'dimensions' && change.dimensions) {
        dimensions.current[change.id] = change.dimensions
      }
    }
  }, [])

  const { nodes, edges } = useMemo(() => {
    const levels = levelsFor(steps)
    const perLevel: Record<number, number> = {}
    for (const step of steps) {
      const level = levels.get(step.id) || 0
      perLevel[level] = (perLevel[level] || 0) + 1
    }
    const maxRows = Math.max(1, ...Object.values(perLevel))
    const seenLevel: Record<number, number> = {}
    const nodeList: Node<NodeData>[] = steps.map((step) => {
      const level = levels.get(step.id) || 0
      const row = seenLevel[level] || 0
      seenLevel[level] = row + 1
      const status = statusById[step.id]?.status || 'pending'
      const error = statusById[step.id]?.error
      const selected = selectedStep === step.id
      const position = {
        x: (maxRows - perLevel[level]) * 148 + row * 296 + 30,
        y: level * 154 + 30,
      }
      // Reuse the previous node object when nothing changed. React Flow keys its
      // internal (measured) node state off object identity, so recreating every
      // node on each run event makes the whole graph disappear until it has
      // measured every node again.
      const prev = nodeCache.current.get(step.id)
      if (
        prev &&
        prev.position.x === position.x &&
        prev.position.y === position.y &&
        prev.data.step === step &&
        prev.data.status === status &&
        prev.data.error === error &&
        prev.data.selected === selected
      ) {
        return prev
      }
      // Top-to-bottom DAG: dependency depth is the row, siblings fan out.
      const measured = prev?.measured ?? dimensions.current[step.id]
      return {
        id: step.id,
        type: 'step',
        position,
        data: { step, status, error, selected },
        draggable: false,
        selectable: true,
        ...(measured ? { measured } : {}),
      }
    })
    nodeCache.current = new Map(nodeList.map((node) => [node.id, node]))
    const edgeList: Edge[] = []
    for (const step of steps) {
      for (const need of step.needs || []) {
        const targetStatus = statusById[step.id]?.status
        edgeList.push({
          id: `${need}-${step.id}`,
          source: need,
          target: step.id,
          type: 'smoothstep',
          animated: targetStatus === 'running' || targetStatus === 'waiting_approval',
          style: { strokeWidth: 1.5 },
          markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16 },
        })
      }
    }
    return { nodes: nodeList, edges: edgeList }
  }, [steps, statusById, selectedStep])

  return (
    <div className={clsx('canvas-wrap', compact && 'compact')}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        onNodesChange={handleNodesChange}
        fitView
        fitViewOptions={{ padding: 0.25 }}
        minZoom={0.35}
        maxZoom={1.4}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable
        onNodeClick={(_, node) => onSelectStep?.(node.id)}
        proOptions={{ hideAttribution: true }}
      >
        <Background gap={22} size={1} color="var(--canvas-grid)" />
      </ReactFlow>
      {steps.length === 0 && <div className="canvas-empty">No steps yet</div>}
    </div>
  )
}

export function WorkflowMiniGraph({ spec, run }: { spec: WorkflowSpec; run?: Run | null }) {
  if (!spec?.steps?.length) return null
  const statuses = new Map<string, string>()
  for (const sr of run?.steps || []) {
    const top = sr.step_id.split(/[.[]/)[0]
    if (!statuses.has(top) || sr.attempt > 1) statuses.set(top, sr.status)
  }
  return (
    <div className="mini-graph">
      {spec.steps.map((s) => (
        <span key={s.id} className={clsx('mini-node', `st-${statuses.get(s.id) || 'pending'}`)} title={s.id}>
          {icons[stepKind(s)] || <GitBranch size={11} />}
        </span>
      ))}
    </div>
  )
}

export function StepKindBadge({ step }: { step: StepSpec }) {
  const kind = stepKind(step)
  return <Badge tone="neutral">{kind}</Badge>
}
