import { AlertTriangle, CheckCircle2, CircleAlert, Lightbulb } from 'lucide-react'
import type { ValidationResult } from '../types'
import { Badge } from './ui'

export function DiagnosticsList({ result, compact }: { result?: ValidationResult; compact?: boolean }) {
  if (!result) return null
  const errors = result.errors || []
  const warnings = result.warnings || []
  if (!errors.length && !warnings.length) {
    return (
      <div className="diagnostics clean">
        <CheckCircle2 size={15} />
        <span>Valid workflow. No errors or warnings.</span>
      </div>
    )
  }
  return (
    <div className={`diagnostics ${compact ? 'compact' : ''}`}>
      {errors.map((d, i) => (
        <div key={`e-${d.code}-${i}`} className="diagnostic error">
          <CircleAlert size={15} />
          <div>
            <div className="diagnostic-head">
              <Badge tone="bad">{d.code}</Badge>
              <span className="mono small">{d.path || 'workflow'}</span>
            </div>
            <p>{d.message}</p>
            {d.hint && <div className="diagnostic-hint"><Lightbulb size={12} /> {d.hint}</div>}
          </div>
        </div>
      ))}
      {warnings.map((d, i) => (
        <div key={`w-${d.code}-${i}`} className="diagnostic warning">
          <AlertTriangle size={15} />
          <div>
            <div className="diagnostic-head">
              <Badge tone="warn">{d.code}</Badge>
              <span className="mono small">{d.path || 'workflow'}</span>
            </div>
            <p>{d.message}</p>
            {d.hint && <div className="diagnostic-hint"><Lightbulb size={12} /> {d.hint}</div>}
          </div>
        </div>
      ))}
    </div>
  )
}
