import { expect, test } from '@playwright/test'

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
  await expect(page.getByRole('heading', { name: 'Workflows' })).toBeVisible()
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
  await page.goto('/#/tools')
  await expect(page.getByRole('heading', { name: 'Tools' })).toBeVisible({ timeout: 15_000 })
  await page.getByText('fs.list').click()
  await expect(page.getByRole('heading', { name: 'fs.list' })).toBeVisible()
  await expect(page.getByText('Input schema')).toBeVisible()
  await expect(page.getByText('List files and folders inside the workspace.')).toBeVisible()
})
