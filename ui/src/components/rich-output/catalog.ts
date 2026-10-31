import { defineCatalog } from '@json-render/core'
import { schema } from '@json-render/react/schema'
import { z } from 'zod'

const dataPoint = z.object({
  label: z.string(),
  value: z.number(),
})

const chartProps = z.object({
  title: z.string().optional(),
  data: z.array(dataPoint).min(1).max(60),
  valueFormat: z.enum(['number', 'currency', 'percent']).optional(),
})

const children = { slots: ['default'] }

export const richCatalog = defineCatalog(schema, {
  components: {
    Stack: {
      props: z.object({
        gap: z.enum(['sm', 'md', 'lg']).optional(),
        direction: z.enum(['row', 'column']).optional(),
      }),
      ...children,
      description: 'Vertical or horizontal layout container for other components.',
    },
    Grid: {
      props: z.object({
        columns: z.number().int().min(1).max(4).optional(),
      }),
      ...children,
      description: 'Responsive column grid for metrics or charts.',
    },
    Card: {
      props: z.object({
        title: z.string().optional(),
        description: z.string().optional(),
      }),
      ...children,
      description: 'Titled card container for a group of related components.',
    },
    Markdown: {
      props: z.object({
        content: z.string(),
      }),
      slots: [],
      description: 'Markdown prose, lists, links, code and GFM tables.',
    },
    Metric: {
      props: z.object({
        label: z.string(),
        value: z.union([z.string(), z.number()]),
        change: z.string().optional(),
        hint: z.string().optional(),
      }),
      slots: [],
      description: 'A single KPI with a label, value and optional change or hint.',
    },
    BarChart: {
      props: chartProps,
      slots: [],
      description: 'Vertical bar chart for comparing values across categories.',
    },
    LineChart: {
      props: chartProps,
      slots: [],
      description: 'Line chart for trends over time.',
    },
    AreaChart: {
      props: chartProps,
      slots: [],
      description: 'Filled area chart for trends or cumulative totals.',
    },
    PieChart: {
      props: chartProps,
      slots: [],
      description: 'Pie chart for parts of a whole. Use 2-8 slices.',
    },
    Table: {
      props: z.object({
        columns: z.array(z.string()).min(1).max(8),
        rows: z.array(z.array(z.union([z.string(), z.number(), z.null()]))).min(1).max(100),
        caption: z.string().optional(),
      }),
      slots: [],
      description: 'Compact data table. Each row must have the same number of cells as columns.',
    },
    Callout: {
      props: z.object({
        tone: z.enum(['info', 'ok', 'warn', 'bad']).optional(),
        title: z.string().optional(),
        content: z.string(),
      }),
      slots: [],
      description: 'Highlighted note, warning or conclusion.',
    },
  },
  actions: {},
})
