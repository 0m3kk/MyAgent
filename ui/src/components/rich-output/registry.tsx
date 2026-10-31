import { useId, type CSSProperties, type ReactNode } from 'react'
import clsx from 'clsx'
import { AlertCircle, CheckCircle2, Info, XCircle } from 'lucide-react'
import { defineRegistry } from '@json-render/react'
import type { InferComponentProps } from '@json-render/core'
import {
  Area,
  AreaChart as ReAreaChart,
  Bar,
  BarChart as ReBarChart,
  CartesianGrid,
  Cell,
  Legend,
  Line,
  LineChart as ReLineChart,
  Pie,
  PieChart as RePieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { Markdown } from '../markdown'
import { richCatalog } from './catalog'

type ChartProps = InferComponentProps<typeof richCatalog, 'BarChart'>
type TableProps = InferComponentProps<typeof richCatalog, 'Table'>
type MetricProps = InferComponentProps<typeof richCatalog, 'Metric'>
type CalloutProps = InferComponentProps<typeof richCatalog, 'Callout'>

const CHART_COLORS = [
  'var(--accent)',
  'var(--blue)',
  'var(--mint)',
  'var(--amber)',
  'var(--red)',
  '#8b7cf6',
  '#e879b9',
  '#38bdf8',
]

function formatValue(value: number, format?: ChartProps['valueFormat']) {
  if (format === 'currency') {
    return new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency: 'USD',
      notation: 'compact',
      maximumFractionDigits: 1,
    }).format(value)
  }
  if (format === 'percent') {
    return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(value)}%`
  }
  return new Intl.NumberFormat(undefined, {
    notation: 'compact',
    maximumFractionDigits: 1,
  }).format(value)
}

const tooltipStyle = {
  background: 'var(--surface-2)',
  border: '1px solid var(--border)',
  borderRadius: 8,
  boxShadow: 'var(--shadow-sm)',
  color: 'var(--text)',
  fontSize: 11,
  padding: '6px 9px',
}

function ChartFrame({ title, children }: { title?: string; children: ReactNode }) {
  return (
    <figure className="rich-chart">
      {title && <figcaption>{title}</figcaption>}
      <div className="rich-chart-canvas">{children}</div>
    </figure>
  )
}

function chartAxes(format?: ChartProps['valueFormat']) {
  return (
    <>
      <CartesianGrid stroke="var(--border-soft)" strokeDasharray="3 3" vertical={false} />
      <XAxis
        dataKey="label"
        tickLine={false}
        axisLine={false}
        tick={{ fill: 'var(--muted)', fontSize: 10 }}
        interval="preserveStartEnd"
      />
      <YAxis
        width={46}
        tickLine={false}
        axisLine={false}
        tick={{ fill: 'var(--muted)', fontSize: 10 }}
        tickFormatter={(value: number) => formatValue(value, format)}
      />
      <Tooltip
        cursor={{ fill: 'color-mix(in oklab, var(--accent) 7%, transparent)' }}
        contentStyle={tooltipStyle}
        labelStyle={{ color: 'var(--muted)' }}
        formatter={(value) => formatValue(Number(value), format)}
      />
    </>
  )
}

function Chart(props: ChartProps) {
  return (
    <ResponsiveContainer width="100%" height={220}>
      <ReBarChart data={props.data} margin={{ top: 4, right: 8, bottom: 0, left: 0 }}>
        {chartAxes(props.valueFormat)}
        <Bar dataKey="value" fill="var(--accent)" radius={[4, 4, 0, 0]} maxBarSize={46} />
      </ReBarChart>
    </ResponsiveContainer>
  )
}

function LineChart(props: ChartProps) {
  return (
    <ResponsiveContainer width="100%" height={220}>
      <ReLineChart data={props.data} margin={{ top: 4, right: 8, bottom: 0, left: 0 }}>
        {chartAxes(props.valueFormat)}
        <Line
          type="monotone"
          dataKey="value"
          stroke="var(--accent)"
          strokeWidth={2}
          dot={false}
          activeDot={{ r: 4 }}
        />
      </ReLineChart>
    </ResponsiveContainer>
  )
}

function AreaChart(props: ChartProps) {
  const gradientId = `rich-area-${useId().replace(/[^a-zA-Z0-9_-]/g, '')}`
  return (
    <ResponsiveContainer width="100%" height={220}>
      <ReAreaChart data={props.data} margin={{ top: 4, right: 8, bottom: 0, left: 0 }}>
        <defs>
          <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="var(--accent)" stopOpacity={0.32} />
            <stop offset="100%" stopColor="var(--accent)" stopOpacity={0.02} />
          </linearGradient>
        </defs>
        {chartAxes(props.valueFormat)}
        <Area
          type="monotone"
          dataKey="value"
          stroke="var(--accent)"
          strokeWidth={2}
          fill={`url(#${gradientId})`}
        />
      </ReAreaChart>
    </ResponsiveContainer>
  )
}

function PieChart(props: ChartProps) {
  return (
    <ResponsiveContainer width="100%" height={220}>
      <RePieChart>
        <Pie
          data={props.data}
          dataKey="value"
          nameKey="label"
          innerRadius="48%"
          outerRadius="78%"
          paddingAngle={2}
          stroke="var(--surface)"
          strokeWidth={2}
        >
          {props.data.map((item, index) => (
            <Cell key={`${item.label}-${index}`} fill={CHART_COLORS[index % CHART_COLORS.length]} />
          ))}
        </Pie>
        <Tooltip
          contentStyle={tooltipStyle}
          formatter={(value) => formatValue(Number(value), props.valueFormat)}
        />
        <Legend
          iconType="circle"
          iconSize={7}
          wrapperStyle={{ fontSize: 10, color: 'var(--muted)' }}
        />
      </RePieChart>
    </ResponsiveContainer>
  )
}

function RichTable({ columns, rows, caption }: TableProps) {
  const numeric = columns.map((_, index) =>
    rows.length > 0 && rows.every((row) => typeof row[index] === 'number' || row[index] === null),
  )
  return (
    <figure className="rich-table">
      {caption && <figcaption>{caption}</figcaption>}
      <div className="result-table-wrap">
        <table className="result-table">
          <thead>
            <tr>
              {columns.map((column, index) => (
                <th key={column} className={numeric[index] ? 'num' : undefined}>{column}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row, rowIndex) => (
              <tr key={rowIndex}>
                {columns.map((_, cellIndex) => {
                  const cell = row[cellIndex]
                  return (
                    <td key={cellIndex} className={typeof cell === 'number' ? 'num' : undefined}>
                      {cell === null || cell === undefined
                        ? '—'
                        : typeof cell === 'number'
                          ? cell.toLocaleString()
                          : cell}
                    </td>
                  )
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </figure>
  )
}

function MetricView({ label, value, change, hint }: MetricProps) {
  const changeText = change?.trim()
  const direction = changeText?.startsWith('-')
    ? 'down'
    : changeText?.startsWith('+') || /^(up|increase|higher)\b/i.test(changeText ?? '')
      ? 'up'
      : ''
  return (
    <div className="rich-metric">
      <span className="rich-metric-label">{label}</span>
      <strong>{typeof value === 'number' ? value.toLocaleString() : value}</strong>
      {(changeText || hint) && (
        <div className="rich-metric-foot">
          {changeText && <span className={clsx('rich-metric-change', direction)}>{changeText}</span>}
          {hint && <span>{hint}</span>}
        </div>
      )}
    </div>
  )
}

function CalloutView({ tone = 'info', title, content }: CalloutProps) {
  const Icon = tone === 'ok' ? CheckCircle2 : tone === 'bad' ? XCircle : tone === 'warn' ? AlertCircle : Info
  return (
    <div className={clsx('inline-notice', 'rich-callout', `notice-${tone}`)}>
      <Icon size={16} />
      <div>
        {title && <strong>{title}</strong>}
        <div>{content}</div>
      </div>
    </div>
  )
}

export const { registry: richRegistry } = defineRegistry(richCatalog, {
  components: {
    Stack: ({ props, children }) => (
      <div
        className={clsx(
          'rich-stack',
          `gap-${props.gap ?? 'md'}`,
          props.direction === 'row' && 'row',
        )}
      >
        {children}
      </div>
    ),
    Grid: ({ props, children }) => (
      <div
        className="rich-grid"
        style={{ '--rich-columns': String(props.columns ?? 2) } as CSSProperties}
      >
        {children}
      </div>
    ),
    Card: ({ props, children }) => (
      <section className="rich-card">
        {(props.title || props.description) && (
          <header>
            {props.title && <h3>{props.title}</h3>}
            {props.description && <p>{props.description}</p>}
          </header>
        )}
        <div className="rich-card-body">{children}</div>
      </section>
    ),
    Markdown: ({ props }) => <Markdown>{props.content}</Markdown>,
    Metric: ({ props }) => <MetricView {...props} />,
    BarChart: ({ props }) => <ChartFrame title={props.title}><Chart {...props} /></ChartFrame>,
    LineChart: ({ props }) => <ChartFrame title={props.title}><LineChart {...props} /></ChartFrame>,
    AreaChart: ({ props }) => <ChartFrame title={props.title}><AreaChart {...props} /></ChartFrame>,
    PieChart: ({ props }) => <ChartFrame title={props.title}><PieChart {...props} /></ChartFrame>,
    Table: ({ props }) => <RichTable {...props} />,
    Callout: ({ props }) => <CalloutView {...props} />,
  },
})
