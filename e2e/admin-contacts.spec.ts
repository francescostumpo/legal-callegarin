import { expect, test, type Page, type Route } from "@playwright/test"

import {
  adminOrigin,
  auditExpectedAdminHTTPErrors,
  installAdminAudit,
} from "./support/admin-audit"

test.use({ screenshot: "off", trace: "off", video: "off" })

const contactID = "e2e-contact-01"
const contactName = "Persona Nuova E2E"

test.describe.configure({ mode: "serial" })

test("admin authentication, dashboard, and contact lifecycle", async ({
  page,
}) => {
  test.skip(
    test.info().project.name !== "chromium",
    "Chromium-only admin journey",
  )
  test.setTimeout(180_000)

  await test.step("HTTP console errors require an exact observed response allowance", () => {
    const allowances = [
      { method: "GET", pathname: "/api/admin/dashboard", status: 503 as const },
      { method: "GET", pathname: "/api/admin/contacts", status: 401 as const },
    ]
    const chromium503 =
      "Failed to load resource: the server responded with a status of 503 (Service Unavailable)"
    const chromium401 =
      "Failed to load resource: the server responded with a status of 401 (Unauthorized)"
    expect(
      auditExpectedAdminHTTPErrors(
        allowances,
        [
          { method: "GET", target: "/api/admin/dashboard", status: 503 },
          { method: "GET", target: "/api/admin/contacts", status: 401 },
        ],
        [chromium503, chromium401],
      ),
    ).toEqual([])
    for (const [allowed, observed, consoleError] of [
      [
        allowances[0],
        { method: "GET", target: "/api/admin/dashboard", status: 503 },
        "Failed to load resource: the server responded with a status of 503 ()",
      ],
      [
        allowances[1],
        { method: "GET", target: "/api/admin/contacts", status: 401 },
        "Failed to load resource: the server responded with a status of 401 ()",
      ],
    ] as const) {
      expect(
        auditExpectedAdminHTTPErrors([allowed], [observed], [consoleError]),
      ).toEqual([])
    }

    const unexpected = auditExpectedAdminHTTPErrors(
      [allowances[0]],
      [{ method: "GET", target: "/api/admin/unexpected", status: 503 }],
      [chromium503],
    )
    expect(unexpected).toContain(
      "unexpected HTTP GET /api/admin/unexpected 503",
    )
    expect(unexpected).toContain(`console error: ${chromium503}`)
    expect(unexpected).toContain(
      "expected HTTP GET /api/admin/dashboard 503 1 time(s), observed 0",
    )
    expect(
      auditExpectedAdminHTTPErrors(
        [allowances[0]],
        [{ method: "GET", target: "/api/admin/dashboard", status: 503 }],
        [
          "Failed to load resource: the server responded with a status of 503 (Unauthorized)",
        ],
      ),
    ).toContain(
      "console error: Failed to load resource: the server responded with a status of 503 (Unauthorized)",
    )
  })

  const username = process.env.E2E_ADMIN_USERNAME
  const password = process.env.E2E_ADMIN_PASSWORD
  if (!username || !password) {
    throw new Error("E2E admin credential environment is required")
  }

  const audit = installAdminAudit(page, [
    { method: "POST", pathname: "/admin/login", status: 401 },
    { method: "GET", pathname: "/api/admin/dashboard", status: 503 },
    { method: "GET", pathname: "/api/admin/contacts", status: 401 },
  ])
  const mutationHeaders: Promise<{
    action: string
    csrfPresent: boolean
    ifMatchQuoted: boolean
  }>[] = []
  page.on("request", (request) => {
    const url = new URL(request.url())
    const match = url.pathname.match(
      /^\/api\/admin\/contacts\/e2e-contact-01\/(read|archive|restore|schedule-deletion|cancel-deletion)$/,
    )
    if (request.method() !== "POST" || !match) return
    mutationHeaders.push(
      Promise.all([
        request.headerValue("x-csrf-token"),
        request.headerValue("if-match"),
      ]).then(([csrf, ifMatch]) => ({
        action: match[1],
        csrfPresent: typeof csrf === "string" && csrf.length > 0,
        ifMatchQuoted:
          typeof ifMatch === "string" && /^"[^"\r\n]+"$/.test(ifMatch),
      })),
    )
  })

  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto("/admin/login")
  await expect(
    page.getByRole("heading", { level: 1, name: "Accesso amministrazione" }),
  ).toBeVisible()
  await audit.assertPage({ cookie: "none" })

  const failedLogin = waitForAdminResponse(page, "POST", "/admin/login", 401)
  await page.getByLabel("Nome utente").fill("amministratore-inesistente-e2e")
  await page.getByLabel("Password").fill("credenziale-non-valida-e2e")
  await page.getByRole("button", { name: "Accedi" }).click()
  await failedLogin
  await expect(page.getByRole("alert")).toHaveText("Credenziali non valide")
  await expect(page.getByRole("alert")).not.toContainText(/utente|password/i)

  let dashboardFailureCount = 0
  const dashboardURL = `${adminOrigin}/api/admin/dashboard`
  const failFirstDashboard = async (route: Route) => {
    if (route.request().method() !== "GET" || dashboardFailureCount > 0) {
      await route.continue()
      return
    }
    dashboardFailureCount++
    await route.fulfill({
      status: 503,
      contentType: "application/json",
      headers: { "Cache-Control": "no-store" },
      json: {
        error: {
          code: "contacts_unavailable",
          message: "Panoramica temporaneamente non disponibile.",
          requestId: "e2e-dashboard-503",
        },
      },
    })
  }
  await page.route(dashboardURL, failFirstDashboard)

  await page.getByLabel("Nome utente").fill(username)
  await page.getByLabel("Password").fill(password)
  const successfulLogin = waitForAdminResponse(
    page,
    "POST",
    "/admin/login",
    303,
  )
  const failedDashboard = waitForAdminResponse(
    page,
    "GET",
    "/api/admin/dashboard",
    503,
  )
  await page.getByRole("button", { name: "Accedi" }).click()
  await successfulLogin
  await page.waitForURL(`${adminOrigin}/admin`)
  await failedDashboard
  await expect(
    page.getByRole("heading", { level: 1, name: "Panoramica" }),
  ).toBeVisible()
  await expect(page.getByRole("alert")).toHaveText(
    "Impossibile caricare la panoramica",
  )
  await expect(page.getByRole("button", { name: "Riprova" })).toBeVisible()
  expect(dashboardFailureCount).toBe(1)
  await audit.assertPage({ authenticatedShell: true, cookie: "authenticated" })

  await page.unroute(dashboardURL, failFirstDashboard)
  const dashboardRetry = waitForAdminResponse(
    page,
    "GET",
    "/api/admin/dashboard",
    200,
  )
  await page.getByRole("button", { name: "Riprova" }).click()
  await dashboardRetry
  const newBadge = page.getByLabel(/\d+ nuovi contatti/)
  await expect(newBadge).toBeVisible()
  expect(
    Number.parseInt((await newBadge.textContent()) ?? "0", 10),
  ).toBeGreaterThanOrEqual(1)
  await expect(page.getByRole("link", { name: "Apri la coda" })).toBeVisible()
  for (const category of [
    "Letti",
    "Archiviati",
    "In eliminazione",
    "Da rivedere",
  ]) {
    await expect(page.getByText(category, { exact: true })).toBeVisible()
  }
  await audit.assertPage({ authenticatedShell: true, cookie: "authenticated" })

  const initialQueue = waitForAdminResponse(
    page,
    "GET",
    "/api/admin/contacts?state=new",
    200,
  )
  await page.getByRole("link", { name: "Apri la coda" }).click()
  await initialQueue
  await expect(page).toHaveURL(/\/admin\/contatti\?state=new$/)
  await expect(
    page.getByRole("heading", { level: 1, name: "Contatti" }),
  ).toBeVisible()
  await expect(
    page.getByRole("link", { name: new RegExp(contactName) }),
  ).toBeVisible()

  const searchRequest = waitForAdminResponse(
    page,
    "GET",
    "/api/admin/contacts?q=Persona+Nuova+E2E&state=new",
    200,
  )
  await page.getByLabel("Cerca per nome o email").fill(contactName)
  await searchRequest
  await expect(
    page.getByRole("link", { name: new RegExp(contactName) }),
  ).toBeVisible()

  const readFilterRequest = waitForAdminResponse(
    page,
    "GET",
    "/api/admin/contacts?q=Persona+Nuova+E2E&state=read",
    200,
  )
  await page.getByLabel("Stato").selectOption("read")
  await readFilterRequest
  await expect(
    page.getByRole("heading", { name: "Nessun risultato" }),
  ).toBeVisible()

  const newFilterRequest = waitForAdminResponse(
    page,
    "GET",
    "/api/admin/contacts?q=Persona+Nuova+E2E&state=new",
    200,
  )
  await page.getByLabel("Stato").selectOption("new")
  await newFilterRequest
  await expect(
    page.getByRole("link", { name: new RegExp(contactName) }),
  ).toBeVisible()

  const deletionFilterRequest = waitForAdminResponse(
    page,
    "GET",
    "/api/admin/contacts?q=Persona+Nuova+E2E&state=new&deletionScheduled=true",
    200,
  )
  await page.getByLabel("In eliminazione").check()
  await deletionFilterRequest
  await expect(
    page.getByRole("heading", { name: "Nessun risultato" }),
  ).toBeVisible()
  const clearDeletionRequest = waitForAdminResponse(
    page,
    "GET",
    "/api/admin/contacts?q=Persona+Nuova+E2E&state=new",
    200,
  )
  await page.getByLabel("In eliminazione").uncheck()
  await clearDeletionRequest

  const retentionFilterRequest = waitForAdminResponse(
    page,
    "GET",
    "/api/admin/contacts?q=Persona+Nuova+E2E&state=new&retentionReview=true",
    200,
  )
  await page.getByLabel("Revisione conservazione").check()
  await retentionFilterRequest
  await expect(
    page.getByRole("heading", { name: "Nessun risultato" }),
  ).toBeVisible()
  const clearRetentionRequest = waitForAdminResponse(
    page,
    "GET",
    "/api/admin/contacts?q=Persona+Nuova+E2E&state=new",
    200,
  )
  await page.getByLabel("Revisione conservazione").uncheck()
  await clearRetentionRequest

  const clearSearchRequest = waitForAdminResponse(
    page,
    "GET",
    "/api/admin/contacts?state=new",
    200,
  )
  await page.getByLabel("Cerca per nome o email").fill("")
  await clearSearchRequest
  const clearStateRequest = waitForAdminResponse(
    page,
    "GET",
    "/api/admin/contacts",
    200,
  )
  await page.getByLabel("Stato").selectOption("")
  await clearStateRequest
  await expect(page.getByLabel("Cerca per nome o email")).toHaveValue("")
  await expect(page.getByLabel("Stato")).toHaveValue("")
  await expect(page.getByLabel("In eliminazione")).not.toBeChecked()
  await expect(page.getByLabel("Revisione conservazione")).not.toBeChecked()
  await audit.assertPage({ authenticatedShell: true, cookie: "authenticated" })

  await page.getByRole("button", { name: "Apri navigazione" }).click()
  const closeNavigation = page.getByRole("button", {
    name: "Chiudi navigazione",
  })
  await expect(closeNavigation).toBeFocused()
  await audit.assertPage({
    authenticatedShell: true,
    cookie: "authenticated",
    focusWithin: ".mobile-panel",
  })
  await page.keyboard.press("Escape")
  await expect(
    page.getByRole("button", { name: "Apri navigazione" }),
  ).toBeFocused()

  const markRead = waitForAdminResponse(
    page,
    "POST",
    `/api/admin/contacts/${contactID}/read`,
    200,
  )
  await page.getByRole("link", { name: new RegExp(contactName) }).click()
  await markRead
  await expect(
    page.getByRole("heading", { level: 1, name: contactName }),
  ).toBeVisible()
  await expect(page.getByText("Letto", { exact: true })).toBeVisible()
  await expect(page.getByRole("button", { name: "Archivia" })).toBeVisible()
  await audit.assertPage({ authenticatedShell: true, cookie: "authenticated" })

  await mutateAndExpect(page, contactID, "archive", "Archivia")
  await expect(page.getByText("Archiviato", { exact: true })).toBeVisible()
  await expect(page.getByRole("button", { name: "Ripristina" })).toBeVisible()
  await mutateAndExpect(page, contactID, "restore", "Ripristina")
  await expect(page.getByText("Letto", { exact: true })).toBeVisible()

  await page.getByRole("button", { name: "Programma eliminazione" }).click()
  const dialog = page.getByRole("dialog", {
    name: "Conferma eliminazione programmata",
  })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole("button", { name: "Annulla" })).toBeFocused()
  await audit.assertPage({
    authenticatedShell: true,
    cookie: "authenticated",
    focusWithin: "[role=dialog]",
  })
  const scheduleDeletion = waitForAdminResponse(
    page,
    "POST",
    `/api/admin/contacts/${contactID}/schedule-deletion`,
    200,
  )
  await dialog
    .getByRole("button", { name: "Conferma, elimina tra 30 giorni" })
    .click()
  await scheduleDeletion
  await expect(
    page.getByText("Eliminazione programmata", { exact: true }),
  ).toBeVisible()
  await expect(
    page.getByRole("button", { name: "Annulla eliminazione programmata" }),
  ).toBeVisible()
  await mutateAndExpect(
    page,
    contactID,
    "cancel-deletion",
    "Annulla eliminazione programmata",
  )
  await expect(
    page.getByText("Eliminazione programmata", { exact: true }),
  ).toHaveCount(0)
  await expect(
    page.getByRole("button", { name: "Programma eliminazione" }),
  ).toBeVisible()

  const observedMutations = await Promise.all(mutationHeaders)
  expect(observedMutations.map((observation) => observation.action)).toEqual([
    "read",
    "archive",
    "restore",
    "schedule-deletion",
    "cancel-deletion",
  ])
  expect(
    observedMutations.every(
      (observation) => observation.csrfPresent && observation.ifMatchQuoted,
    ),
  ).toBe(true)

  await page.setViewportSize({ width: 1440, height: 900 })
  await audit.assertPage({ authenticatedShell: true, cookie: "authenticated" })

  const desktopDashboard = waitForAdminResponse(
    page,
    "GET",
    "/api/admin/dashboard",
    200,
  )
  await page.getByRole("link", { name: "Panoramica" }).click()
  await desktopDashboard
  await audit.assertPage({ authenticatedShell: true, cookie: "authenticated" })

  const desktopContactList = waitForAdminResponse(
    page,
    "GET",
    "/api/admin/contacts",
    200,
  )
  await page.getByRole("link", { name: "Contatti" }).click()
  await desktopContactList
  await audit.assertPage({ authenticatedShell: true, cookie: "authenticated" })

  let retentionRouteCount = 0
  await page.route(
    `${adminOrigin}/api/admin/contacts/${contactID}`,
    async (route) => {
      retentionRouteCount++
      const response = await route.fetch()
      const body = (await response.json()) as Record<string, unknown>
      await route.fulfill({
        response,
        json: { ...body, reviewDueAt: "2000-01-01T00:00:00Z" },
      })
    },
    { times: 1 },
  )
  await page.goto(`/admin/contatti/${contactID}`)
  await expect(
    page.getByText("Revisione conservazione richiesta", { exact: true }),
  ).toBeVisible()
  expect(retentionRouteCount).toBe(1)
  await audit.assertPage({ authenticatedShell: true, cookie: "authenticated" })

  const scheduleButton = page.getByRole("button", {
    name: "Programma eliminazione",
  })
  await scheduleButton.click()
  await expect(
    page.getByRole("dialog", { name: "Conferma eliminazione programmata" }),
  ).toBeVisible()
  await audit.assertPage({
    authenticatedShell: true,
    cookie: "authenticated",
    focusWithin: "[role=dialog]",
  })
  await page.keyboard.press("Escape")
  await expect(scheduleButton).toBeFocused()

  await page.route(
    `${adminOrigin}/api/admin/contacts`,
    async (route) => {
      await route.fulfill({
        status: 401,
        contentType: "application/json",
        headers: { "Cache-Control": "no-store" },
        json: {
          error: {
            code: "authentication_required",
            message: "Autenticazione richiesta.",
            requestId: "e2e-session-expired",
          },
        },
      })
    },
    { times: 1 },
  )
  const expiredContacts = waitForAdminResponse(
    page,
    "GET",
    "/api/admin/contacts",
    401,
  )
  const loginNavigation = page.waitForURL(`${adminOrigin}/admin/login`, {
    waitUntil: "domcontentloaded",
  })
  await page.getByRole("link", { name: "Tutti i contatti" }).click()
  await Promise.all([expiredContacts, loginNavigation])
  await expect(
    page.getByRole("heading", { level: 1, name: "Accesso amministrazione" }),
  ).toBeVisible()
  await expect(page.getByRole("heading", { name: contactName })).toHaveCount(0)
  await audit.assertPage({ cookie: "authenticated" })
  await audit.assertClean()
})

async function mutateAndExpect(
  page: Page,
  id: string,
  action: string,
  buttonName: string,
) {
  const response = waitForAdminResponse(
    page,
    "POST",
    `/api/admin/contacts/${id}/${action}`,
    200,
  )
  await page.getByRole("button", { name: buttonName, exact: true }).click()
  await response
}

function waitForAdminResponse(
  page: Page,
  method: string,
  target: string,
  status: number,
) {
  return page.waitForResponse((response) => {
    const url = new URL(response.url())
    return (
      response.request().method() === method &&
      `${url.pathname}${url.search}` === target &&
      response.status() === status
    )
  })
}
