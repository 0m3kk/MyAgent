import { useEffect, useState } from 'react'
import { CirclePlay, Workflow as WorkflowIcon } from 'lucide-react'
import { api } from '../api'
import { navigate } from '../router'
import type { Run, Workflow as WorkflowModel } from '../types'
import { Badge, Button, EmptyState, Field, InlineNotice, Modal, Select, Spinner, StatusBadge } from './ui'
import { InputForm, schemaDefaults, validateInputs } from './input-form'
import { useToast } from './toast'

export function ChatWorkflowDialog({
  open,
  onClose,
  conversationId,
  workflows,
  onStarted,
}: {
  open: boolean
  onClose: () => void
  conversationId: string
  workflows: WorkflowModel[]
  onStarted: (run: Run, name: string) => void
}) {
  const toast = useToast()
  const [available, setAvailable] = useState<WorkflowModel[]>([])
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [selectedId, setSelectedId] = useState('')
  const [inputs, setInputs] = useState<Record<string, unknown>>({})
  const [busy, setBusy] = useState(false)

  const selected = available.find((w) => w.id === selectedId)
  const schema = selected?.version?.input_schema
  const errors = validateInputs(schema, inputs)

  useEffect(() => {
    if (!open) return
    let cancelled = false
    setLoading(true)
    setLoadError('')
    api.workflows()
      .then((res) => {
        if (cancelled) return
        const list = res.workflows.filter((w) => !!w.version)
        setAvailable(list)
        const first = list[0]
        setSelectedId(first?.id || '')
        setInputs(schemaDefaults(first?.version?.input_schema))
      })
      .catch((err) => {
        if (cancelled) return
        // Keep the modal usable if the refresh fails: the bootstrap list is
        // usually a moment old, which is fine for choosing a workflow.
        const fallback = workflows.filter((w) => !!w.version)
        setAvailable(fallback)
        setLoadError(err instanceof Error ? err.message : 'Could not refresh workflows')
        const first = fallback[0]
        setSelectedId(first?.id || '')
        setInputs(schemaDefaults(first?.version?.input_schema))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
    // The refresh runs once per open; the passed-in list only seeds failures.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  function chooseWorkflow(id: string) {
    setSelectedId(id)
    const next = available.find((w) => w.id === id)
    setInputs(schemaDefaults(next?.version?.input_schema))
  }

  async function start() {
    if (!selected?.version || busy) return
    setBusy(true)
    try {
      const res = await api.runWorkflowInChat(
        conversationId,
        selected.id,
        inputs,
        selected.version.id,
      )
      toast('Workflow started', selected.name, 'success')
      onStarted(res.run, selected.name)
      onClose()
    } catch (err) {
      toast('Could not start the workflow', err instanceof Error ? err.message : undefined, 'error')
    } finally {
      setBusy(false)
    }
  }

  const noWorkflows = !loading && available.length === 0
  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Run a workflow in this chat"
      subtitle="Inputs, approvals and the live run stay in the conversation."
      width={620}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          <Button
            variant="primary"
            icon={<CirclePlay size={15} />}
            onClick={start}
            loading={busy}
            disabled={loading || noWorkflows || !selected?.version || Object.keys(errors).length > 0}
          >
            Start run
          </Button>
        </>
      }
    >
      {loading ? (
        <div className="workflow-run-loading"><Spinner size={15} /> Loading workflows…</div>
      ) : noWorkflows ? (
        <EmptyState
          compact
          icon={<WorkflowIcon size={24} />}
          title="No workflows yet"
          description="Create a workflow first, then run it from any chat."
          action={
            <Button
              variant="subtle"
              size="sm"
              onClick={() => {
                onClose()
                navigate('/workflows')
              }}
            >
              Open workflows
            </Button>
          }
        />
      ) : (
        <div className="form-grid">
          {loadError && (
            <InlineNotice tone="warn" title="Could not refresh the workflow list">
              {loadError}
            </InlineNotice>
          )}
          <Field label="Workflow">
            <Select
              value={selectedId}
              onChange={chooseWorkflow}
              aria-label="Workflow"
              options={available.map((w) => ({
                value: w.id,
                label: `${w.name} · v${w.version?.version ?? '—'}`,
              }))}
            />
          </Field>
          {selected?.version && (
            <div className="run-dialog-target">
              <span className="run-dialog-target-icon"><WorkflowIcon size={15} /></span>
              <div>
                <strong>{selected.name}</strong>
                <span>
                  <StatusBadge status={selected.version.status} />
                  <span className="muted tiny">{selected.steps?.length ?? 0} steps</span>
                  {selected.pinned && <Badge tone="neutral">pinned</Badge>}
                </span>
              </div>
            </div>
          )}
          {selected?.version && selected.version.status !== 'approved' && (
            <InlineNotice tone="warn" title="This version is not approved">
              Chat runs may use the current draft. Any write or external step still pauses for approval.
            </InlineNotice>
          )}
          <InputForm
            key={selectedId}
            schema={schema}
            value={inputs}
            onChange={setInputs}
            disabled={busy}
          />
          <div className="run-dialog-note">
            <CirclePlay size={14} />
            The run is linked to this conversation; approvals and progress appear in the chat.
          </div>
        </div>
      )}
    </Modal>
  )
}
