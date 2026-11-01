import { expect, test } from '@playwright/test'
import { richCatalog } from '../src/components/rich-output/catalog'
import { promoteRichBlocks, unclosedSpecStart } from '../src/components/rich-output/detect'
import { normalizeSpec } from '../src/components/rich-output/normalize'

const spec = {
  root: 'inp',
  elements: {
    inp: {
      type: 'Table',
      props: {
        columns: ['Input', 'Bắt buộc', 'Ý nghĩa'],
        rows: [
          ['url', 'Có', 'URL trang cần tổng hợp'],
          ['do_dai', 'Không (mặc định: vừa)', 'ngắn / vừa / chi tiết'],
          ['trong_tam', 'Không', "Tập trung vào gì, vd: 'giá và thông số'"],
        ],
        caption: 'Không có đường dẫn, email hay ngày nào bị hardcode',
      },
      children: [],
    },
  },
}

const specJSON = JSON.stringify(spec)

test('bare spec objects are promoted to ui blocks', () => {
  const markdown = `Here is the input table:\n\n${specJSON}\n\nThat is all.`
  const promoted = promoteRichBlocks(markdown)
  expect(promoted).toContain('```ui\n' + specJSON + '\n```')
  expect(promoted).toContain('That is all.')
})

test('json fences containing a spec are promoted to ui blocks', () => {
  const promoted = promoteRichBlocks('```json\n' + specJSON + '\n```')
  expect(promoted).toContain('```ui\n' + specJSON + '\n```')
})

test('ordinary json stays a code block', () => {
  const markdown = '```json\n{"hello":"world"}\n```'
  expect(promoteRichBlocks(markdown)).toBe(markdown)
})

test('json inside another fenced block is left alone', () => {
  const markdown = '```md\nSpec example:\n' + specJSON + '\n```'
  expect(promoteRichBlocks(markdown)).toBe(markdown)
})

test('unfinished bare specs are hidden while streaming', () => {
  expect(unclosedSpecStart('{"root":"inp","elements":{"inp":{"type":"Table",')).toBe(0)
  expect(unclosedSpecStart(specJSON)).toBe(-1)
  expect(unclosedSpecStart('{"hello":"world"')).toBe(-1)
})

test('a stray children key inside elements does not break validation', () => {
  const broken = {
    root: 'r',
    elements: {
      r: {
        type: 'Table',
        props: {
          columns: ['Mục', 'Hành động', 'Còn lại'],
          rows: [
            ['probe_tpl (workflow nháp)', 'Đã xoá', '—'],
            ['probe_out (workflow nháp)', 'Đã xoá', '—'],
            ['web.probe_two (tool nháp)', 'Đã xoá', '—'],
            ['web.page_fetch (bản Python thử)', 'Đã xoá', '—'],
            ['tom_tat_trang_web', 'Giữ', 'draft, v1 — validate lại OK'],
            ['web.page_text', 'Giữ', 'active, v1'],
          ],
          caption: 'Không tool/workflow nào còn tham chiếu tới các mục đã xoá',
        },
      },
      children: [],
    },
  }
  const normalized = normalizeSpec(broken)
  const result = richCatalog.validate(normalized)
  expect(result.success).toBe(true)
  expect('children' in (normalized as { elements: Record<string, unknown> }).elements).toBe(false)
})
