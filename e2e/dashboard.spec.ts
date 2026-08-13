import { expect, test } from '@playwright/test'

test.describe.configure({ mode: 'serial' })

test('serves the patient page over HTTP/2 and upgrades the Web Component', async ({ page }) => {
  await page.goto('/patients')

  await expect(page.getByRole('heading', { name: 'Patient dashboard' })).toBeVisible()
  await expect(page.getByText('Maya Chen', { exact: true })).toBeVisible()

  const protocol = await page.evaluate(() => {
    const navigation = performance.getEntriesByType('navigation')[0] as PerformanceNavigationTiming
    return navigation.nextHopProtocol
  })
  expect(protocol).toBe('h2')

  const avatar = page.locator('patient-avatar').first()
  await expect.poll(() => avatar.evaluate((element) => element.shadowRoot?.textContent?.includes('EB'))).toBe(true)
})

test('morphs between patient views without replacing the document', async ({ page }) => {
  await page.goto('/patients?patient=019fbd32-0602-7002-8000-000000000002&status=open')
  await expect(page.getByRole('heading', { name: 'Elias Brooks' })).toBeVisible()

  const documentIdentity = await page.evaluate(() => {
    const identity = crypto.randomUUID()
    ;(window as typeof window & { __documentIdentity?: string }).__documentIdentity = identity
    return identity
  })
  await page.getByLabel('Task', { exact: true }).fill('Unsaved task draft')
  const mayaLink = page.locator('a.patient-link', { hasText: 'Maya Chen' })
  const box = await mayaLink.boundingBox()
  if (!box) throw new Error('Maya patient link has no bounding box')
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
  await page.mouse.down()

  // Navigation begins on mousedown, before mouseup/click.
  await expect(page).toHaveURL(/\/patients\?patient=019fbd32-0601-7001-8000-000000000001&status=open/)
  await page.mouse.up()

  // The utility keeps the original document and suppresses the native click.
  await expect(page.getByRole('heading', { name: 'Maya Chen' })).toBeVisible()
  await expect(page.getByLabel('Task', { exact: true })).toHaveValue('')
  await expect.poll(() => page.evaluate(() => (
    window as typeof window & { __documentIdentity?: string }
  ).__documentIdentity)).toBe(documentIdentity)

  await page.getByLabel('Task', { exact: true }).fill('Maya history draft')
  await page.goBack()
  await expect(page.getByRole('heading', { name: 'Elias Brooks' })).toBeVisible()
  await expect(page.getByLabel('Task', { exact: true })).toHaveValue('Unsaved task draft')
  await expect.poll(() => page.evaluate(() => (
    window as typeof window & { __documentIdentity?: string }
  ).__documentIdentity)).toBe(documentIdentity)

  // Keyboard activation remains available through the click fallback.
  await page.locator('a.patient-link', { hasText: 'Maya Chen' }).focus()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('heading', { name: 'Maya Chen' })).toBeVisible()
  await expect(page.getByLabel('Task', { exact: true })).toHaveValue('Maya history draft')

  // A write invalidation must only update the current patient stream. A stale
  // stream for the initial URL used to morph the dashboard back to Elias.
  const taskTitle = `Maya stream ownership ${Date.now()}`
  await page.getByLabel('Task', { exact: true }).fill(taskTitle)
  await page.getByLabel('Due date').fill('2026-08-20')
  await page.getByRole('button', { name: 'Add task' }).click()
  await expect(page.getByRole('heading', { name: 'Maya Chen' })).toBeVisible()
  await expect(page.getByText(taskTitle, { exact: true })).toBeVisible()

  await page.reload()
  await expect(page.getByRole('heading', { name: 'Maya Chen' })).toBeVisible()
  await expect(page.getByText(taskTitle, { exact: true })).toBeVisible()
})

test('an explicit patient choice cancels a pending search', async ({ page }) => {
  await page.goto('/patients')
  await expect(page.getByRole('heading', { name: 'Elias Brooks' })).toBeVisible()

  await page.getByRole('searchbox', { name: 'Search patients' }).fill('Noor')
  await page.waitForTimeout(100)
  await page.locator('a.patient-link', { hasText: 'Maya Chen' }).click()

  // Wait beyond the search debounce. Its old timer must not overwrite Maya.
  await page.waitForTimeout(350)
  await expect(page.getByRole('heading', { name: 'Maya Chen' })).toBeVisible()
  await expect.poll(() => page.evaluate(() => ({
    patient: new URL(window.location.href).searchParams.get('patient'),
    search: new URL(window.location.href).searchParams.get('q'),
  }))).toEqual({
    patient: '019fbd32-0601-7001-8000-000000000001',
    search: '',
  })

  await page.reload()
  await expect(page.getByRole('heading', { name: 'Maya Chen' })).toBeVisible()
})

test('debounces interactive patient search until typing pauses', async ({ page }) => {
  await page.goto('/patients')
  await expect(page.getByRole('heading', { name: 'Patient dashboard' })).toBeVisible()

  const searchRequests: string[] = []
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (request.method() === 'POST' && url.pathname === '/patients' && url.searchParams.has('q')) {
      searchRequests.push(url.searchParams.get('q') ?? '')
    }
  })

  const search = page.getByRole('searchbox', { name: 'Search patients' })
  await search.focus()
  for (const character of 'Noor') {
    await page.keyboard.type(character)
    await page.waitForTimeout(150)
    expect(searchRequests).toHaveLength(0)
  }

  await expect.poll(() => searchRequests).toEqual(['Noor'])
  await expect.poll(() => page.evaluate(() => ({
    patient: new URL(window.location.href).searchParams.get('patient'),
    search: new URL(window.location.href).searchParams.get('q'),
  }))).toEqual({
    patient: null,
    search: 'Noor',
  })
  await expect(page.locator('a.patient-link')).toHaveCount(1)
  await expect(page.getByText('Select or create a patient', { exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Elias Brooks' })).not.toBeVisible()
  await expect(search).toHaveValue('Noor')

  await page.reload()
  await expect(page.getByText('Select or create a patient', { exact: true })).toBeVisible()
  await page.locator('a.patient-link', { hasText: 'Noor Ahmed' }).click()
  await expect(page.getByRole('heading', { name: 'Noor Ahmed' })).toBeVisible()
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Noor Ahmed' })).toBeVisible()

  await page.getByRole('searchbox', { name: 'Search patients' }).fill('')
  await expect.poll(() => page.evaluate(() => ({
    patient: new URL(window.location.href).searchParams.get('patient'),
    search: new URL(window.location.href).searchParams.get('q'),
  }))).toEqual({
    patient: '019fbd32-0603-7003-8000-000000000003',
    search: null,
  })
  await expect(page.locator('a.patient-link')).toHaveCount(4)
  await expect(page.getByRole('heading', { name: 'Noor Ahmed' })).toBeVisible()
})

test('persists independent task drafts per patient and clears a submitted draft', async ({ page }) => {
  const noorID = '019fbd32-0603-7003-8000-000000000003'
  const eliasID = '019fbd32-0602-7002-8000-000000000002'
  await page.goto(`/patients?patient=${noorID}&status=open`)
  await expect(page.getByRole('heading', { name: 'Noor Ahmed' })).toBeVisible()

  const draftSavedFor = (patientID: string) => page.waitForResponse((response) => {
    const url = new URL(response.url())
    return response.request().method() === 'POST' &&
      url.pathname === '/commands' &&
      url.searchParams.get('command') === 'save-task-draft' &&
      url.searchParams.get('patient') === patientID &&
      response.status() === 204
  })
  const patientStreams: string[] = []
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (request.method() === 'POST' && url.pathname === '/patients') {
      patientStreams.push(`${url.pathname}${url.search}`)
    }
  })

  const noorSaved = draftSavedFor(noorID)
  await page.getByLabel('Task', { exact: true }).fill('Call Noor about')
  await page.locator('a.patient-link', { hasText: 'Elias Brooks' }).click()
  await noorSaved

  await expect(page.getByRole('heading', { name: 'Elias Brooks' })).toBeVisible()
  const eliasSaved = draftSavedFor(eliasID)
  await page.getByLabel('Task', { exact: true }).fill('Review Elias paperwork')
  await page.getByLabel('Due date').fill('2026-08-22')
  await page.getByLabel('Priority').selectOption('urgent')
  await page.locator('a.patient-link', { hasText: 'Noor Ahmed' }).click()
  await eliasSaved
  await expect(page.getByRole('heading', { name: 'Noor Ahmed' })).toBeVisible()
  await expect(page.getByLabel('Task', { exact: true })).toHaveValue('Call Noor about')

  const completedTitle = `Call Noor about sleep journal ${Date.now()}`
  await page.getByLabel('Task', { exact: true }).fill(completedTitle)
  await page.getByLabel('Due date').fill('2026-08-20')
  await page.getByLabel('Priority').selectOption('important')

  await page.getByRole('button', { name: 'Add task' }).click()

  await expect(page.getByText(completedTitle, { exact: true })).toBeVisible()
  await page.waitForTimeout(250)
  expect(patientStreams).toEqual([
    `/patients?patient=${eliasID}&status=open&q=`,
    `/patients?patient=${noorID}&status=open&q=`,
  ])
  await expect(page).toHaveURL(new RegExp(`patient=${noorID}`))
  await expect(page.getByRole('heading', { name: 'Noor Ahmed' })).toBeVisible()
  await expect(page.getByLabel('Task', { exact: true })).toHaveValue('')
  await expect(page.getByLabel('Due date')).toHaveValue('')
  await expect(page.getByLabel('Priority')).toHaveValue('routine')

  await page.locator('a.patient-link', { hasText: 'Elias Brooks' }).click()
  await expect(page.getByRole('heading', { name: 'Elias Brooks' })).toBeVisible()
  await expect(page.getByLabel('Task', { exact: true })).toHaveValue('Review Elias paperwork')
  await expect(page.getByLabel('Due date')).toHaveValue('2026-08-22')
  await expect(page.getByLabel('Priority')).toHaveValue('urgent')

  // sessionStorage keeps this tab identity across reloads; the values below are
  // hydrated from PostgreSQL rather than retained by the old DOM.
  await page.reload()
  await expect(page.getByLabel('Task', { exact: true })).toHaveValue('Review Elias paperwork')
  await expect(page.getByLabel('Due date')).toHaveValue('2026-08-22')
  await expect(page.getByLabel('Priority')).toHaveValue('urgent')
})

test('creates a patient and rejects a forged command', async ({ page }) => {
  await page.goto('/patients')

  const patientName = `Realtime Patient ${Date.now()}`
  await page.getByText('Add a patient', { exact: true }).click()
  await page.getByLabel('Full name').fill(patientName)
  await page.getByLabel('Date of birth').fill('1990-06-15')
  await page.getByLabel('Pronouns').fill('they/them')
  await page.getByLabel('Care team').fill('Demo Clinician, LMFT')
  await page.getByRole('button', { name: 'Create patient' }).click()

  await expect(page.getByText(patientName, { exact: true })).toBeVisible()
  await expect(page.getByText(/Patient created/)).toBeVisible()

  const forgedStatus = await page.evaluate(async () => {
    const response = await fetch('/commands?command=create-patient', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ csrf: 'forged', patientName: 'Should Not Exist', patientDob: '1990-01-01' }),
    })
    return response.status
  })
  expect(forgedStatus).toBe(403)
})

test('pushes one database change into clients on two different page views', async ({ browser }) => {
  const context = await browser.newContext({ ignoreHTTPSErrors: true })
  const patientPage = await context.newPage()
  const duePage = await context.newPage()

  await patientPage.goto('/patients?patient=019fbd32-0601-7001-8000-000000000001&status=open')
  await duePage.goto('/tasks/due?window=30')
  await expect(duePage.getByRole('heading', { name: 'Due tasks' })).toBeVisible()

  const taskTitle = `Cross-view follow-up ${Date.now()}`
  const taskInput = patientPage.getByLabel('Task', { exact: true })
  await taskInput.fill(taskTitle)
  await taskInput.evaluate((element) => (element as HTMLInputElement).setSelectionRange(3, 8))

  // A full-main morph caused by another client must preserve the actual input
  // node, its live value, focus, and selection.
  const completionButton = duePage.locator('button.toggle').first()
  await expect(completionButton).toBeVisible()
  await completionButton.click()
  await expect(taskInput).toBeFocused()
  await expect(taskInput).toHaveValue(taskTitle)
  await expect.poll(() => taskInput.evaluate((element) => {
    const input = element as HTMLInputElement
    return [input.selectionStart, input.selectionEnd]
  })).toEqual([3, 8])

  const dueDate = new Date(Date.now() + 2 * 24 * 60 * 60 * 1_000).toISOString().slice(0, 10)
  await patientPage.getByLabel('Due date').fill(dueDate)
  await patientPage.getByLabel('Priority').selectOption('important')
  await patientPage.getByRole('button', { name: 'Add task' }).click()

  await expect(patientPage.getByText(taskTitle, { exact: true })).toBeVisible()
  await expect(duePage.getByText(taskTitle, { exact: true })).toBeVisible()

  await duePage.goto('/tasks/due?window=7')
  await expect(duePage.getByRole('link', { name: '7 days' })).toHaveAttribute('aria-current', 'true')
  await expect(duePage.getByText(taskTitle, { exact: true })).toBeVisible()

  await context.close()
})
