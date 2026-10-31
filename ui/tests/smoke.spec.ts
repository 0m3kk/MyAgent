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
