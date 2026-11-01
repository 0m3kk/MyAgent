import { useEffect, useMemo, useState } from 'react'
import { Braces, Calendar, Hash, HelpCircle, ToggleLeft, Type } from 'lucide-react'
import type { InputSpec } from '../types'
import { Field, Select, TextArea, TextInput, Toggle } from './ui'

interface JSONSchemaProperty {
  type?: string
  description?: string
  default?: unknown
  enum?: unknown[]
  format?: string
  minimum?: number
  maximum?: number
  items?: JSONSchemaProperty
}

export interface JSONSchema {
  type?: string
  properties?: Record<string, JSONSchemaProperty>
  required?: string[]
  additionalProperties?: boolean
}

export function schemaDefaults(schema?: JSONSchema | Record<string, unknown> | null): Record<string, unknown> {
  const s = (schema || {}) as JSONSchema
  const out: Record<string, unknown> = {}
  for (const [name, prop] of Object.entries(s.properties || {})) {
    if (prop.default !== undefined) out[name] = prop.default
    else if (prop.type === 'boolean') out[name] = false
  }
  return out
}

export function validateInputs(schema: JSONSchema | Record<string, unknown> | null | undefined, values: Record<string, unknown>): Record<string, string> {
  const s = (schema || {}) as JSONSchema
  const errors: Record<string, string> = {}
  for (const [name, prop] of Object.entries(s.properties || {})) {
    const v = values[name]
    const missing = v === undefined || v === null || v === ''
    if ((s.required || []).includes(name) && missing) {
      errors[name] = 'Required'
      continue
    }
    if (missing) continue
    if (prop.type === 'integer' && (!Number.isInteger(Number(v)))) errors[name] = 'Enter a whole number'
    if (prop.type === 'number' && Number.isNaN(Number(v))) errors[name] = 'Enter a number'
    if (prop.enum && !prop.enum.map(String).includes(String(v))) errors[name] = 'Choose one of the allowed values'
    if (prop.minimum !== undefined && Number(v) < prop.minimum) errors[name] = `Must be ≥ ${prop.minimum}`
    if (prop.maximum !== undefined && Number(v) > prop.maximum) errors[name] = `Must be ≤ ${prop.maximum}`
  }
  return errors
}

export function InputForm({
  schema,
  value,
  onChange,
  disabled,
}: {
  schema?: JSONSchema | Record<string, unknown> | null
  value: Record<string, unknown>
  onChange: (next: Record<string, unknown>) => void
  disabled?: boolean
}) {
  const s = (schema || {}) as JSONSchema
  const entries = Object.entries(s.properties || {})
  const errors = validateInputs(s, value)

  if (entries.length === 0) {
    return <div className="input-empty">This target has no inputs.</div>
  }

  function set(name: string, v: unknown) {
    onChange({ ...value, [name]: v })
  }

  return (
    <div className="input-form">
      {entries.map(([name, prop]) => {
        const required = (s.required || []).includes(name)
        const v = value[name]
        const label = name.replace(/_/g, ' ')
        const hint = prop.description || (prop.format ? `format: ${prop.format}` : undefined)
        const icon = prop.type === 'integer' || prop.type === 'number' ? <Hash size={12} /> : prop.type === 'boolean' ? <ToggleLeft size={12} /> : prop.format === 'datetime' ? <Calendar size={12} /> : prop.type === 'object' || prop.type === 'array' ? <Braces size={12} /> : <Type size={12} />
        if (prop.type === 'boolean') {
          return (
            <Toggle
              key={name}
              checked={!!v}
              onChange={(next) => set(name, next)}
              label={<span className="input-label">{icon} {label}{required && <span className="required"> *</span>}</span>}
              description={hint}
              disabled={disabled}
            />
          )
        }
        if (prop.enum && prop.enum.length > 0) {
          return (
            <Field key={name} label={<span className="input-label">{icon} {label}</span>} hint={hint} error={errors[name]} required={required}>
              <Select
                value={v === undefined ? '' : String(v)}
                onChange={(next) => set(name, next)}
                disabled={disabled}
                aria-label={label}
                options={[
                  { value: '', label: 'Choose…' },
                  ...prop.enum.map((opt) => ({ value: String(opt), label: String(opt) })),
                ]}
              />
            </Field>
          )
        }
        if (prop.type === 'object' || prop.type === 'array') {
          return (
            <JSONInput
              key={name}
              name={name}
              icon={icon}
              label={label}
              hint={hint}
              required={required}
              error={errors[name]}
              value={v}
              disabled={disabled}
              array={prop.type === 'array'}
              onChange={(next) => set(name, next)}
            />
          )
        }
        const inputType = prop.type === 'integer' || prop.type === 'number' ? 'number' : prop.format === 'datetime' ? 'datetime-local' : 'text'
        return (
          <Field key={name} label={<span className="input-label">{icon} {label}</span>} hint={hint} error={errors[name]} required={required}>
            <TextInput
              type={inputType}
              value={v === undefined || v === null ? '' : String(v)}
              placeholder={prop.default !== undefined ? String(prop.default) : undefined}
              min={prop.minimum}
              max={prop.maximum}
              step={prop.type === 'integer' ? 1 : 'any'}
              onChange={(e) => {
                const raw = e.target.value
                if (raw === '') set(name, undefined)
                else if (prop.type === 'integer') set(name, Number.parseInt(raw, 10))
                else if (prop.type === 'number') set(name, Number(raw))
                else set(name, raw)
              }}
              disabled={disabled}
            />
          </Field>
        )
      })}
    </div>
  )
}

function JSONInput({
  name,
  icon,
  label,
  hint,
  required,
  error,
  value,
  disabled,
  array,
  onChange,
}: {
  name: string
  icon: React.ReactNode
  label: string
  hint?: string
  required: boolean
  error?: string
  value: unknown
  disabled?: boolean
  array: boolean
  onChange: (next: unknown) => void
}) {
  const [draft, setDraft] = useState(() => value === undefined ? '' : JSON.stringify(value, null, 2))
  const [jsonError, setJsonError] = useState('')
  useEffect(() => {
    if (value === undefined) setDraft('')
    else setDraft(JSON.stringify(value, null, 2))
  }, [value])
  return (
    <Field key={name} label={<span className="input-label">{icon} {label}</span>} hint={hint} error={jsonError || error} required={required}>
      <TextArea
        rows={4}
        className="mono"
        value={draft}
        disabled={disabled}
        onChange={(e) => {
          setDraft(e.target.value)
          if (!e.target.value.trim()) {
            setJsonError('')
            onChange(array ? [] : {})
            return
          }
          try {
            onChange(JSON.parse(e.target.value))
            setJsonError('')
          } catch {
            setJsonError('Invalid JSON')
          }
        }}
      />
    </Field>
  )
}

export function InputSpecList({ inputs }: { inputs?: Record<string, InputSpec> }) {
  const entries = useMemo(() => Object.entries(inputs || {}), [inputs])
  if (!entries.length) return <span className="muted">No inputs</span>
  return (
    <div className="spec-chips">
      {entries.map(([name, spec]) => (
        <span key={name} className="spec-chip" title={spec.description}>
          <HelpCircle size={11} /> {name} <em>{spec.type}</em>
        </span>
      ))}
    </div>
  )
}
