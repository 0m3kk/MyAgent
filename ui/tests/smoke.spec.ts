import { expect, test, type Page } from '@playwright/test'

const FS_LIST_MANIFEST = `schema: 1
name: fs.list
description: List files and folders inside the workspace.
kind: starlark
runtime: {entry: main.star}
side_effect: read
input_schema:
  type: object
  additionalProperties: false
  properties:
    path: {type: string, description: Folder path, default: "."}
    limit: {type: integer, description: Maximum entries to return, default: 200}
output_schema: {type: object}
permissions:
  network: {domains: []}
  fs: {read: ["."], write: []}
  exec: []
limits: {timeout_s: 30, memory_mb: 256, output_kb: 1024}
`

const FS_LIST_CODE = `def run(input, ctx):
    return fs.list(input.get("path", "."), input.get("limit", 200))
`

// Fresh workspaces no longer seed built-in tools, so tests that inspect the
// tools UI create the fixture they need through the same API the app uses.
async function ensureFsListTool(page: Page) {
  const tokenRes = await page.request.get('/api/dev-token')
  const { token } = (await tokenRes.json()) as { token: string }
  const headers = { Authorization: `Bearer ${token}` }
  const current = await page.request.get('/api/tools', { headers })
  const body = (await current.json()) as { tools?: { name: string }[] }
  if (body.tools?.some((tool) => tool.name === 'fs.list')) return
  const created = await page.request.post('/api/tools', {
    headers,
    data: { manifest_yaml: FS_LIST_MANIFEST, files: { 'main.star': FS_LIST_CODE } },
  })
  expect(created.ok()).toBeTruthy()
}

test('home, command palette and workflow list', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'What should we get done?' })).toBeVisible({ timeout: 15_000 })
  await expect(page.getByText('Add a model to start')).toBeVisible()

  await page.keyboard.press('Meta+k')
  const palette = page.getByPlaceholder('Search chats, workflows, tools or actions…')
  await expect(palette).toBeVisible()
  await palette.fill('settings')
  await page.keyboard.press('Escape')

  await page.getByRole('link', { name: /Workflows/ }).click()
  await expect(page.getByRole('heading', { name: 'Workflows', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Organize with agent' })).toBeVisible()
  await expect(page.getByText('Build your first workflow')).toBeVisible()
})

test('home and all chats are separate menus', async ({ page }) => {
  await page.goto('/#/home')
  await expect(page.getByRole('heading', { name: 'What should we get done?' })).toBeVisible({ timeout: 15_000 })
  await page.getByRole('link', { name: /^Chats/ }).click()
  await expect(page.getByRole('heading', { name: 'All chats' })).toBeVisible()
  await expect(page.getByText('No chats yet')).toBeVisible()
  await page.getByRole('link', { name: 'Home' }).click()
  await expect(page.getByRole('heading', { name: 'What should we get done?' })).toBeVisible()
})

test('new chat opens a chat-only composer', async ({ page }) => {
  await page.goto('/#/chats')
  await expect(page.getByRole('heading', { name: 'All chats' })).toBeVisible({ timeout: 15_000 })
  await page.locator('.page-header').getByRole('button', { name: 'New chat' }).click()
  await expect(page.getByRole('heading', { name: 'New chat' })).toBeVisible()
  await expect(page.locator('.new-chat-page .composer textarea')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Recent chats' })).toHaveCount(0)
})

test('chat title can be renamed and reset', async ({ page }) => {
  await page.goto('/#/chats')
  await expect(page.getByRole('heading', { name: 'All chats' })).toBeVisible({ timeout: 15_000 })
  const token = await page.evaluate(async () => {
    const res = await fetch('/api/dev-token')
    return ((await res.json()) as { token: string }).token
  })
  const response = await page.request.post('/api/chat', {
    headers: { Authorization: `Bearer ${token}` },
    data: { text: 'first chat' },
  })
  const payload = (await response.json()) as { conversation: { id: string } }
  await page.goto(`/#/chats/${payload.conversation.id}`)

  const title = page.locator('.chat-title-button span')
  await expect(title).toHaveText('first chat', { timeout: 10_000 })

  await page.locator('.chat-title-button').click()
  await page.getByLabel('Chat title').fill('Custom title')
  await page.getByLabel('Chat title').press('Enter')
  await expect(title).toHaveText('Custom title')

  await page.getByRole('button', { name: 'Conversation menu' }).click()
  await page.getByRole('button', { name: 'Reset title' }).click()
  await expect(title).toHaveText('first chat')
})

test('delete uses the in-app confirm dialog', async ({ page }) => {
  await page.goto('/#/chats')
  await expect(page.getByRole('heading', { name: 'All chats' })).toBeVisible({ timeout: 15_000 })
  const token = await page.evaluate(async () => {
    const res = await fetch('/api/dev-token')
    return ((await res.json()) as { token: string }).token
  })
  const headers = { Authorization: `Bearer ${token}` }
  const response = await page.request.post('/api/chat', {
    headers,
    data: { text: 'Confirm test' },
  })
  const conversationID = ((await response.json()) as { conversation: { id: string } }).conversation.id
  await page.goto(`/#/chats/${conversationID}`)
  await expect(page.getByRole('heading', { name: 'Confirm test' })).toBeVisible()

  await page.getByRole('button', { name: 'Conversation menu' }).click()
  await page.getByRole('button', { name: 'Delete conversation' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await expect(dialog.getByText('Delete conversation')).toBeVisible()
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expect(dialog).toHaveCount(0)

  await page.getByRole('button', { name: 'Conversation menu' }).click()
  await page.getByRole('button', { name: 'Delete conversation' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Delete chat' }).click()
  await expect(page.getByRole('heading', { name: 'All chats' })).toBeVisible()
  await expect(page.getByText('Confirm test', { exact: true })).toHaveCount(0)
})

test('structured workflow output renders readable content', async ({ page }) => {
  await page.goto('/#/runs')
  await expect(page.getByRole('heading', { name: 'Runs', exact: true })).toBeVisible({ timeout: 15_000 })
  const token = await page.evaluate(async () => {
    const res = await fetch('/api/dev-token')
    return ((await res.json()) as { token: string }).token
  })
  const headers = { Authorization: `Bearer ${token}` }
  const yaml = `schema: 1
name: result-view-test
description: Structured output test.
inputs: {}
outputs:
  summary: \${steps.compose.output.summary}
  sources: \${steps.compose.output.sources}
steps:
  - id: compose
    set:
      summary: |
        # Internet summary

        The agent found **useful sources**.
      sources:
        - title: Local-first software
          url: https://www.inkandswitch.com/local-first/
          snippet: Seven ideals for local-first software.
`
  const saved = await page.request.post('/api/workflows', {
    headers,
    data: { yaml, change_note: 'e2e', approve: true },
  })
  const workflowID = ((await saved.json()) as { workflow: { id: string } }).workflow.id
  const runResponse = await page.request.post(`/api/workflows/${workflowID}/run`, {
    headers,
    data: { inputs: {} },
  })
  const runID = ((await runResponse.json()) as { run: { id: string } }).run.id
  await expect.poll(async () => {
    const res = await page.request.get(`/api/runs/${runID}`, { headers })
    return ((await res.json()) as { run: { status: string } }).run.status
  }, { timeout: 10_000 }).toBe('succeeded')

  await page.goto(`/#/runs/${runID}`)
  await expect(page.getByRole('heading', { name: 'Internet summary' })).toBeVisible()
  await expect(page.getByRole('link', { name: /Local-first software/ })).toBeVisible()
  await expect(page.getByText('Raw JSON')).toBeVisible()
})

test('workflow can be started from chat and stays in the conversation', async ({ page }) => {
  await page.goto('/#/chats')
  await expect(page.getByRole('heading', { name: 'All chats' })).toBeVisible({ timeout: 15_000 })
  const token = await page.evaluate(async () => {
    const res = await fetch('/api/dev-token')
    return ((await res.json()) as { token: string }).token
  })
  const headers = { Authorization: `Bearer ${token}` }
  const yaml = `schema: 1
name: chat-run-test
description: Workflow started from a chat.
inputs: {}
outputs:
  message: \${steps.compose.output.message}
steps:
  - id: compose
    set:
      message: Hello from the workflow
`
  const saved = await page.request.post('/api/workflows', {
    headers,
    data: { yaml, change_note: 'e2e', approve: true },
  })
  expect(saved.ok()).toBeTruthy()
  const conversation = await page.request.post('/api/conversations', {
    headers,
    data: { title: 'Workflow chat' },
  })
  const conversationID = ((await conversation.json()) as { id: string }).id
  await page.goto(`/#/chats/${conversationID}`)
  await expect(page.getByRole('heading', { name: 'Workflow chat' })).toBeVisible()

  await page.getByRole('button', { name: 'Run workflow' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText('Run a workflow in this chat')).toBeVisible()
  await dialog.getByRole('combobox', { name: 'Workflow' }).click()
  await page.getByRole('option', { name: /chat-run-test/ }).click()
  await dialog.getByRole('button', { name: 'Start run' }).click()

  const card = page.locator('.workflow-run-card')
  await expect(card).toContainText('chat-run-test')
  await expect(card.locator('.workflow-run-head .badge').filter({ hasText: 'succeeded' })).toBeVisible({ timeout: 10_000 })
  await page.reload()
  await expect(page.locator('.workflow-run-card')).toContainText('chat-run-test')
})

test('text size can be increased and reset in settings', async ({ page }) => {
  await page.goto('/#/settings')
  const control = page.locator('.settings-page .text-size-control')
  await expect(control).toBeVisible({ timeout: 15_000 })
  const before = await page.evaluate(() => getComputedStyle(document.body).fontSize)
  await control.getByRole('button', { name: 'Increase text size' }).click()
  await expect(control.locator('.text-size-value')).toHaveText('105%')
  const after = await page.evaluate(() => getComputedStyle(document.body).fontSize)
  expect(after).not.toBe(before)
  await control.locator('.text-size-value').click()
  await expect(control.locator('.text-size-value')).toHaveText('100%')

  // The setting lives in the Go core, so a reload (and a native app restart
  // on a different random port) restores it.
  await control.getByRole('button', { name: 'Increase text size' }).click()
  await page.reload()
  await expect(page.locator('.settings-page .text-size-value')).toHaveText('105%')
  await page.locator('.settings-page .text-size-value').click()
})

test('text size scales page chrome and content', async ({ page }) => {
  await page.goto('/#/tools')
  await expect(page.getByRole('heading', { name: 'Tools', exact: true })).toBeVisible({ timeout: 15_000 })
  await page.keyboard.press('Meta+0')

  const selectors = [
    '.page-header h1',
    '.page-description',
    '.search-box input',
    '.btn-primary',
    '.library-nav-row',
    '.library-group-head h3',
    '.tool-tile-open',
    '.tool-tile p',
    '.badge',
  ]
  const sizes = async () => page.evaluate((list) => Object.fromEntries(list.map((selector) => {
    const el = document.querySelector(selector)
    return [selector, el ? parseFloat(getComputedStyle(el).fontSize) : 0]
  })), selectors)

  const before = await sizes()
  await page.keyboard.press('Meta+=')
  await expect.poll(async () => (await sizes())['.page-header h1']).toBeGreaterThan(before['.page-header h1'])
  const after = await sizes()
  const unscaled = selectors.filter((selector) => before[selector] > 0 && after[selector] <= before[selector])
  expect(unscaled).toEqual([])
  await page.keyboard.press('Meta+0')
})

test('agent steps can be unlimited', async ({ page }) => {
  await page.goto('/#/settings')
  const unlimited = page.getByRole('checkbox', { name: 'Unlimited' })
  await expect(unlimited).toBeVisible({ timeout: 15_000 })
  await unlimited.check()
  await page.getByRole('button', { name: 'Save settings' }).click()
  await page.reload()
  await expect(page.getByRole('checkbox', { name: 'Unlimited' })).toBeChecked()
  await page.getByRole('checkbox', { name: 'Unlimited' }).uncheck()
  await page.getByRole('button', { name: 'Save settings' }).click()
})

test('appearance has light, dark and system modes', async ({ page }) => {
  await page.goto('/#/settings')
  await expect(page.getByRole('radiogroup', { name: 'Appearance' })).toBeVisible({ timeout: 15_000 })
  await page.getByRole('radio', { name: 'Dark' }).click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await page.getByRole('radio', { name: 'System' }).click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', /light|dark/)
  await page.getByRole('radio', { name: 'Light' }).click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await expect(page.getByRole('radio', { name: 'Light' })).toHaveAttribute('aria-checked', 'true')
  await page.getByRole('radio', { name: 'System' }).click()
})

test('tool inspector renders schema and tests', async ({ page }) => {
  await ensureFsListTool(page)
  await page.goto('/#/tools')
  await expect(page.getByRole('heading', { name: 'Tools', exact: true })).toBeVisible({ timeout: 15_000 })
  await page.getByText('fs.list').click()
  await expect(page.getByRole('heading', { name: 'fs.list' })).toBeVisible()
  await expect(page.getByText('Input schema')).toBeVisible()
  await expect(page.getByText('List files and folders inside the workspace.')).toBeVisible()
})

test('selects use an in-app dropdown', async ({ page }) => {
  await page.goto('/#/runs')
  await expect(page.getByRole('heading', { name: 'Runs', exact: true })).toBeVisible({ timeout: 15_000 })

  const select = page.getByRole('combobox', { name: 'Filter by status' })
  await select.click()
  const menu = page.getByRole('listbox')
  await expect(menu).toBeVisible()
  await expect(menu.getByRole('option', { name: 'All statuses' })).toHaveAttribute('aria-selected', 'true')
  await menu.getByRole('option', { name: 'failed' }).click()
  await expect(select).toContainText('failed')
  await expect(select).toHaveAttribute('aria-expanded', 'false')

  await select.focus()
  await page.keyboard.press('ArrowDown')
  await expect(page.getByRole('listbox')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('listbox')).toHaveCount(0)
})

test('tools can be organized into folders and pinned', async ({ page }) => {
  await ensureFsListTool(page)
  await page.goto('/#/tools')
  await expect(page.getByRole('heading', { name: 'Tools', exact: true })).toBeVisible({ timeout: 15_000 })

  await page.getByRole('button', { name: 'New folder' }).click()
  const folderDialog = page.getByRole('dialog')
  await expect(folderDialog.getByText('New folder')).toBeVisible()
  await folderDialog.getByPlaceholder('e.g. Research').fill('Files')
  await folderDialog.getByRole('button', { name: 'Create folder' }).click()
  await expect(page.locator('.library-title h2')).toHaveText('Files')

  await page.getByRole('button', { name: /^All/ }).click()
  const tile = page.locator('.tool-tile', { hasText: 'fs.list' })
  await tile.getByRole('button', { name: 'Move fs.list to folder' }).click()
  const moveDialog = page.getByRole('dialog')
  await moveDialog.locator('.move-row', { hasText: 'Files' }).click()
  await moveDialog.getByRole('button', { name: 'Move' }).click()

  await page.locator('.folder-hit', { hasText: 'Files' }).click()
  await expect(page.locator('.library-title h2')).toHaveText('Files')
  await expect(page.locator('.tool-tile', { hasText: 'fs.list' })).toBeVisible()

  await page.getByRole('button', { name: 'Pin fs.list' }).click()
  await page.getByRole('button', { name: /^Pinned/ }).click()
  await expect(page.locator('.tool-tile', { hasText: 'fs.list' })).toBeVisible()

  // Leave the shared e2e database tidy for the other tests.
  await page.getByRole('button', { name: /^All/ }).click()
  await page.getByRole('button', { name: 'Unpin fs.list' }).click()
  await page.getByRole('button', { name: 'Delete Files' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Delete folder' }).click()
  await expect(page.locator('.library-title h2')).toHaveText('All tools')
})
