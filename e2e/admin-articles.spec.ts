import { expect, test, type Page } from "@playwright/test"

import {
  adminOrigin,
  auditExpectedAdminPostResponseAborts,
  auditExpectedAdminHTTPErrors,
  installAdminAudit,
} from "./support/admin-audit"
import { installPublicAudit } from "./support/public-audit"

test.use({ screenshot: "off", trace: "off", video: "off" })

const notebookCoverAlt =
  "Quaderno aperto con pagine bianche e una penna su un piano in pietra scura."
const seededArticlePath =
  "/sentenze-e-riflessioni/affidamento-condiviso-guida-e2e"
const seededArticleTitle = "Affidamento condiviso: guida essenziale"
const constrainedAdminViewport = { width: 360, height: 800 }
const mobileViewport = { width: 390, height: 844 }
const desktopViewport = { width: 1440, height: 900 }

type MutationObservation = {
  action: string
  csrfPresent: boolean
  ifMatchQuoted: boolean
}

test.describe.configure({ mode: "serial" })

test("article draft, preview, publication, conflict, and logout lifecycle", async ({
  browser,
  context,
  page,
}) => {
  test.skip(
    test.info().project.name !== "chromium",
    "Chromium-only admin article journey",
  )
  test.setTimeout(180_000)

  await test.step("the admin audit grants only an exact observed 409", () => {
    const allowance = {
      method: "PUT",
      pathname: "/api/admin/articles/article-01/draft",
      search: "?source=editor",
      status: 409 as const,
    }
    const observed = {
      method: "PUT",
      target: "/api/admin/articles/article-01/draft?source=editor",
      status: 409,
    }
    const canonical =
      "Failed to load resource: the server responded with a status of 409 (Conflict)"
    const empty =
      "Failed to load resource: the server responded with a status of 409 ()"

    expect(
      auditExpectedAdminHTTPErrors([allowance], [observed], [canonical]),
    ).toEqual([])
    expect(
      auditExpectedAdminHTTPErrors([allowance], [observed], [empty]),
    ).toEqual([])

    for (const mismatched of [
      {
        ...observed,
        target: "/api/admin/articles/article-02/draft?source=editor",
      },
      { ...observed, target: "/api/admin/articles/article-01/draft" },
      { ...observed, status: 503 },
    ]) {
      expect(
        auditExpectedAdminHTTPErrors([allowance], [mismatched], [canonical]),
      ).not.toEqual([])
    }
    expect(
      auditExpectedAdminHTTPErrors(
        [allowance],
        [observed, observed],
        [canonical],
      ),
    ).toContain(
      "unexpected HTTP PUT /api/admin/articles/article-01/draft?source=editor 409",
    )
    expect(
      auditExpectedAdminHTTPErrors(
        [allowance],
        [observed],
        [
          "Failed to load resource: the server responded with a status of 409 (Unauthorized)",
        ],
      ),
    ).toContain(
      "console error: Failed to load resource: the server responded with a status of 409 (Unauthorized)",
    )
    expect(createdArticleIDFromURL(`${adminOrigin}/admin/articoli/nuovo`)).toBe(
      "",
    )
    expect(
      createdArticleIDFromURL(`${adminOrigin}/admin/articoli/article-01`),
    ).toBe("article-01")

    const abortAllowance = {
      method: "DELETE",
      pathname: "/api/admin/session",
      search: "?source=logout",
      status: 204,
      errorText: "net::ERR_ABORTED",
      count: 1,
    }
    const requestA = {}
    const requestB = {}
    const logoutResponse = {
      request: requestA,
      sequence: 1,
      method: "DELETE",
      target: "/api/admin/session?source=logout",
      status: 204,
    }
    const logoutAbort = {
      request: requestA,
      sequence: 2,
      method: "DELETE",
      target: "/api/admin/session?source=logout",
      errorText: "net::ERR_ABORTED",
    }
    expect(
      auditExpectedAdminPostResponseAborts(
        [abortAllowance],
        [logoutResponse],
        [],
      ),
    ).toEqual([])
    expect(
      auditExpectedAdminPostResponseAborts(
        [abortAllowance],
        [logoutResponse],
        [{ ...logoutAbort, request: requestB }],
      ),
    ).not.toEqual([])
    expect(
      auditExpectedAdminPostResponseAborts(
        [abortAllowance],
        [{ ...logoutResponse, sequence: 2 }],
        [{ ...logoutAbort, sequence: 1 }],
      ),
    ).not.toEqual([])
    expect(
      auditExpectedAdminPostResponseAborts(
        [abortAllowance],
        [logoutResponse],
        [logoutAbort],
      ),
    ).toEqual([])
    for (const mismatch of [
      { ...logoutAbort, method: "POST" },
      { ...logoutAbort, target: "/api/admin/other?source=logout" },
      { ...logoutAbort, target: "/api/admin/session" },
      { ...logoutAbort, errorText: "net::ERR_FAILED" },
    ]) {
      expect(
        auditExpectedAdminPostResponseAborts(
          [abortAllowance],
          [logoutResponse],
          [mismatch],
        ),
      ).not.toEqual([])
    }
    expect(
      auditExpectedAdminPostResponseAborts(
        [{ ...abortAllowance, status: 200 }],
        [logoutResponse],
        [logoutAbort],
      ),
    ).not.toEqual([])
    expect(
      auditExpectedAdminPostResponseAborts(
        [abortAllowance],
        [logoutResponse],
        [logoutAbort, logoutAbort],
      ),
    ).not.toEqual([])
  })

  const username = process.env.E2E_ADMIN_USERNAME
  const password = process.env.E2E_ADMIN_PASSWORD
  if (!username || !password) {
    throw new Error("E2E admin credential environment is required")
  }

  const retry = test.info().retry
  const slug = `ciclo-articolo-e2e-${retry}`
  const publicPath = `/sentenze-e-riflessioni/${slug}`
  const v1Title = `Ciclo articolo E2E V1 ${retry}`
  const v1Body = "Corpo sintetico V1 verificato in anteprima e pubblicazione."
  const v2Title = `Ciclo articolo E2E V2 ${retry}`
  const v2Body =
    "Corpo sintetico V2 salvato senza modificare subito il pubblico."
  const v3Title = `Ciclo articolo E2E V3 ${retry}`
  const v3Body = "Corpo sintetico V3 che rende obsoleta la seconda pagina."
  const staleTitle = `Tit locale preservato E2E ${retry}`
  const staleBody = "Corpo locale preservato dopo il conflitto ETag reale."

  const audit = installAdminAudit(
    page,
    [],
    [
      {
        method: "DELETE",
        pathname: "/api/admin/session",
        status: 204,
        errorText: "net::ERR_ABORTED",
      },
    ],
  )
  const mutations: MutationObservation[] = []
  context.on("request", (request) => {
    const url = new URL(request.url())
    if (url.origin !== adminOrigin) return
    const create =
      request.method() === "POST" && url.pathname === "/api/admin/articles"
    const articleMutation = url.pathname.match(
      /^\/api\/admin\/articles\/[^/]+\/(draft|publish|withdraw)$/,
    )
    const logout =
      request.method() === "DELETE" && url.pathname === "/api/admin/session"
    if (!create && !articleMutation && !logout) return

    const headers = request.headers()
    mutations.push({
      action: create
        ? "create"
        : logout
          ? "logout"
          : (articleMutation?.[1] ?? "unknown"),
      csrfPresent:
        typeof headers["x-csrf-token"] === "string" &&
        headers["x-csrf-token"].length > 0,
      ifMatchQuoted:
        typeof headers["if-match"] === "string" &&
        /^"[^"\r\n]+"$/.test(headers["if-match"]),
    })
  })

  const publicContext = await browser.newContext({
    baseURL: adminOrigin,
    ignoreHTTPSErrors: true,
  })
  const publicPage = await publicContext.newPage()
  const publicAudit = installPublicAudit(publicPage, [
    { method: "GET", pathname: publicPath, status: 404 },
  ])

  try {
    await test.step("the seeded public article is visible without cookies", async () => {
      await publicPage.setViewportSize(mobileViewport)
      const response = await publicPage.goto(seededArticlePath)
      expect(response?.status()).toBe(200)
      await expect(
        publicPage.getByRole("heading", {
          level: 1,
          name: seededArticleTitle,
        }),
      ).toBeVisible()
      await publicAudit.assertPage()
      expect(await publicContext.cookies()).toEqual([])
    })

    await test.step("one real login opens the article editor", async () => {
      await page.setViewportSize(mobileViewport)
      await page.goto("/admin/login")
      await expect(
        page.getByRole("heading", {
          level: 1,
          name: "Accesso amministrazione",
        }),
      ).toBeVisible()
      await audit.assertPage({ cookie: "none" })

      await page.getByLabel("Nome utente").fill(username)
      await page.getByLabel("Password").fill(password)
      const login = waitForResponse(page, "POST", "/admin/login", 303)
      await page.getByRole("button", { name: "Accedi" }).click()
      await login
      await page.waitForURL(`${adminOrigin}/admin`)

      const articleList = waitForResponse(
        page,
        "GET",
        "/api/admin/articles",
        200,
      )
      const openNavigation = page.getByRole("button", {
        name: "Apri navigazione",
      })
      await expect(openNavigation).toBeVisible()
      await openNavigation.click()
      const mobileNavigation = page.getByRole("navigation", {
        name: "Navigazione mobile",
      })
      const closeNavigation = mobileNavigation.getByRole("button", {
        name: "Chiudi navigazione",
      })
      const articlesLink = mobileNavigation.getByRole("link", {
        name: "Articoli",
      })
      await expect(closeNavigation).toBeFocused()
      await expect(articlesLink).toBeVisible()
      await articlesLink.click()
      await articleList
      await expect(
        page.getByRole("heading", { level: 1, name: "Articoli" }),
      ).toBeVisible()
      await auditAtBothViewports(page, () =>
        audit.assertPage({ authenticatedShell: true, cookie: "authenticated" }),
      )

      await page.getByRole("link", { name: "Nuovo articolo" }).click()
      await expect(
        page.getByRole("heading", { level: 1, name: "Nuovo articolo" }),
      ).toBeVisible()
    })

    let articleID = ""
    await test.step("create V1 with the curated cover and capture its URL identity", async () => {
      await page.setViewportSize(mobileViewport)
      await page.getByLabel("Titolo").fill(v1Title)
      await page.getByLabel("Slug").fill(slug)
      await page
        .getByLabel("Sommario")
        .fill(
          "Sommario sintetico sufficientemente esteso per il ciclo articolo E2E.",
        )
      await page.getByLabel("Area").selectOption("famiglia-e-persone")
      await page
        .getByLabel("Copertina")
        .selectOption({ label: notebookCoverAlt })

      const editor = page.getByRole("textbox", { name: "Contenuto articolo" })
      await expect(editor).toBeVisible()
      await editor.fill(v1Body)

      const created = waitForResponse(page, "POST", "/api/admin/articles", 201)
      const editorNavigation = page.waitForURL(
        (url) => createdArticleIDFromURL(url) !== "",
        { timeout: 10_000 },
      )
      const createButton = page.getByRole("button", { name: "Crea bozza" })
      await expect(createButton).toBeVisible()
      await createButton.click()
      await Promise.all([created, editorNavigation])
      articleID = createdArticleIDFromURL(page.url())
      expect(articleID).not.toBe("")
      await expect(page.getByRole("status")).toHaveText(/^Bozza creata$/)
      await expect(
        page.getByRole("button", { name: "Salva bozza" }),
      ).toBeEnabled()
      await expect(page.getByRole("button", { name: "Pubblica" })).toBeEnabled()
      await expect(page.getByLabel("Titolo")).toHaveValue(v1Title)
      await expect(page.getByLabel("Copertina")).toHaveValue("article-notebook")
      await expect(editor).toHaveText(v1Body)
      await auditAtBothViewports(page, () =>
        audit.assertPage({ authenticatedShell: true, cookie: "authenticated" }),
      )
    })

    await test.step("saved preview is exact and its viewport controls really resize", async () => {
      await page.setViewportSize(desktopViewport)
      await expect(
        page.getByRole("heading", {
          level: 2,
          name: "Anteprima ultima bozza salvata",
        }),
      ).toBeVisible()
      const frame = page.getByTitle("Anteprima articolo salvato")
      const preview = page.frameLocator(
        'iframe[title="Anteprima articolo salvato"]',
      )
      await expect(
        preview.getByRole("heading", { level: 1, name: v1Title }),
      ).toBeVisible()
      await expect(preview.getByText(v1Body)).toBeVisible()
      const previewCover = preview.locator("img").first()
      await expect(previewCover).toHaveAttribute("alt", notebookCoverAlt)
      await expect(previewCover).toHaveAttribute(
        "src",
        /article-notebook-landscape-[0-9a-f]{12}\.webp$/,
      )

      await page.getByRole("button", { name: "360", exact: true }).click()
      await expect(frame).toHaveClass(/preview-frame--mobile/)
      await expectFrameWidth(frame, 360)
      await assertFrameHasNoHorizontalOverflow(frame)

      await page.getByRole("button", { name: "768", exact: true }).click()
      await expect(frame).toHaveClass(/preview-frame--tablet/)
      await expectFrameWidth(frame, 768)
      await assertFrameHasNoHorizontalOverflow(frame)

      await page.getByRole("button", { name: "Desktop", exact: true }).click()
      await expect(frame).toHaveClass(/preview-frame--desktop/)
      const desktopWidth = (await frame.boundingBox())?.width ?? 0
      expect(desktopWidth).toBeGreaterThan(768)
      await assertFrameHasNoHorizontalOverflow(frame)

      await page.setViewportSize(constrainedAdminViewport)
      await page.getByRole("button", { name: "360", exact: true }).click()
      await expect(frame).toHaveClass(/preview-frame--mobile/)
      await assertAdminHasNoHorizontalOverflow(page)
      await assertFrameFitsAvailableWidth(frame)
      await assertFrameHasNoHorizontalOverflow(frame)

      await page.setViewportSize(desktopViewport)
      await page.getByRole("button", { name: "Desktop", exact: true }).click()
      await expect(frame).toHaveClass(/preview-frame--desktop/)
      await auditAtBothViewports(page, () =>
        audit.assertPage({ authenticatedShell: true, cookie: "authenticated" }),
      )
    })

    await test.step("V1 stays public while V2 is only a saved draft", async () => {
      await page.setViewportSize(desktopViewport)
      const publishedV1 = waitForResponse(
        page,
        "POST",
        `/api/admin/articles/${articleID}/publish`,
        200,
      )
      const publishButton = page.getByRole("button", {
        name: "Pubblica",
        exact: true,
      })
      await expect(publishButton).toBeVisible()
      await publishButton.click()
      await publishedV1
      await expect(page.getByRole("status")).toHaveText("Articolo pubblicato")

      await publicPage.goto(publicPath)
      await expect(
        publicPage.getByRole("heading", { level: 1, name: v1Title }),
      ).toBeVisible()
      await expect(publicPage.getByText(v1Body)).toBeVisible()

      const editor = page.getByRole("textbox", { name: "Contenuto articolo" })
      await page.getByLabel("Titolo").fill(v2Title)
      await editor.fill(v2Body)
      const savedV2 = waitForResponse(
        page,
        "PUT",
        `/api/admin/articles/${articleID}/draft`,
        200,
      )
      await page.getByRole("button", { name: "Salva bozza" }).click()
      await savedV2
      await expect(page.getByRole("status")).toHaveText("Bozza salvata")
      await expect(page.getByText("Modifiche non pubblicate")).toBeVisible()

      await publicPage.reload()
      await expect(
        publicPage.getByRole("heading", { level: 1, name: v1Title }),
      ).toBeVisible()
      await expect(publicPage.getByText(v1Body)).toBeVisible()
      await expect(
        publicPage.getByRole("heading", { level: 1, name: v2Title }),
      ).toHaveCount(0)
      await expect(publicPage.getByText(v2Body)).toHaveCount(0)
    })

    await test.step("republish, withdraw to designed 404, then republish V2", async () => {
      const republishedV2 = waitForResponse(
        page,
        "POST",
        `/api/admin/articles/${articleID}/publish`,
        200,
      )
      await page
        .getByRole("button", { name: "Ripubblica", exact: true })
        .click()
      await republishedV2
      await publicPage.reload()
      await expect(
        publicPage.getByRole("heading", { level: 1, name: v2Title }),
      ).toBeVisible()
      await expect(publicPage.getByText(v2Body)).toBeVisible()

      const withdrawn = waitForResponse(
        page,
        "POST",
        `/api/admin/articles/${articleID}/withdraw`,
        200,
      )
      await page.getByRole("button", { name: "Ritira", exact: true }).click()
      await withdrawn
      const missing = await publicPage.reload()
      expect(missing?.status()).toBe(404)
      await expect(
        publicPage.getByRole("heading", {
          level: 1,
          name: "La pagina non è disponibile",
        }),
      ).toBeVisible()

      const publishedAgain = waitForResponse(
        page,
        "POST",
        `/api/admin/articles/${articleID}/publish`,
        200,
      )
      await page
        .getByRole("button", { name: "Ripubblica", exact: true })
        .click()
      await publishedAgain
      await publicPage.reload()
      await expect(
        publicPage.getByRole("heading", { level: 1, name: v2Title }),
      ).toBeVisible()
      await expect(publicPage.getByText(v2Body)).toBeVisible()
      await auditAtBothViewports(publicPage, () => publicAudit.assertPage())
      expect(await publicContext.cookies()).toEqual([])
    })

    await test.step("a real stale ETag preserves page B local edits and locks mutations", async () => {
      const conflictPage = await context.newPage()
      const conflictAudit = installAdminAudit(conflictPage, [
        {
          method: "PUT",
          pathname: `/api/admin/articles/${articleID}/draft`,
          status: 409,
        },
      ])
      try {
        await conflictPage.setViewportSize(mobileViewport)
        await conflictPage.goto(`/admin/articoli/${articleID}`)
        await expect(conflictPage.getByLabel("Titolo")).toHaveValue(v2Title)
        const conflictEditor = conflictPage.getByRole("textbox", {
          name: "Contenuto articolo",
        })
        await expect(conflictEditor).toHaveText(v2Body)

        const primaryEditor = page.getByRole("textbox", {
          name: "Contenuto articolo",
        })
        await page.getByLabel("Titolo").fill(v3Title)
        await primaryEditor.fill(v3Body)
        const savedV3 = waitForResponse(
          page,
          "PUT",
          `/api/admin/articles/${articleID}/draft`,
          200,
        )
        await page.getByRole("button", { name: "Salva bozza" }).click()
        await savedV3
        await expect(page.getByRole("status")).toHaveText("Bozza salvata")

        await conflictPage.getByLabel("Titolo").fill(staleTitle)
        await conflictEditor.fill(staleBody)
        const staleSave = waitForResponse(
          conflictPage,
          "PUT",
          `/api/admin/articles/${articleID}/draft`,
          409,
        )
        await conflictPage.getByRole("button", { name: "Salva bozza" }).click()
        const conflictResponse = await staleSave
        const conflictPayload = (await conflictResponse.json()) as {
          error?: { code?: string }
        }
        expect(conflictPayload.error?.code).toBe("article_conflict")
        await expect(
          conflictPage.getByRole("button", {
            name: "Ricarica per riconciliare",
          }),
        ).toBeVisible()
        await expect(conflictPage.getByLabel("Titolo")).toHaveValue(staleTitle)
        await expect(conflictEditor).toHaveText(staleBody)
        await expect(
          conflictPage.getByRole("button", { name: "Salva bozza" }),
        ).toBeDisabled()
        await expect(
          conflictPage.getByRole("button", { name: "Ripubblica" }),
        ).toBeDisabled()
        await auditAtBothViewports(conflictPage, () =>
          conflictAudit.assertPage({
            authenticatedShell: true,
            cookie: "authenticated",
          }),
        )
        await conflictAudit.assertClean()
      } finally {
        await conflictPage.close({ runBeforeUnload: false })
      }
    })

    await test.step("real logout revokes the session and removes protected content", async () => {
      await page.setViewportSize(desktopViewport)
      const logout = waitForResponse(page, "DELETE", "/api/admin/session", 204)
      const loginNavigation = page.waitForURL(`${adminOrigin}/admin/login`, {
        waitUntil: "domcontentloaded",
      })
      await page.getByRole("button", { name: "Esci", exact: true }).click()
      await Promise.all([logout, loginNavigation])
      expect(await context.cookies(adminOrigin)).toEqual([])
      await expect(
        page.getByRole("heading", {
          level: 1,
          name: "Accesso amministrazione",
        }),
      ).toBeVisible()
      await page.goto(`/admin/articoli/${articleID}`)
      await expect(page).toHaveURL(`${adminOrigin}/admin/login`)
      await expect(
        page.getByRole("heading", { level: 1, name: "Modifica articolo" }),
      ).toHaveCount(0)
      await audit.assertPage({ cookie: "none" })
    })

    expect(mutations.filter((entry) => entry.action === "create")).toEqual([
      { action: "create", csrfPresent: true, ifMatchQuoted: false },
    ])
    expect(mutations.filter((entry) => entry.action === "draft")).toEqual([
      { action: "draft", csrfPresent: true, ifMatchQuoted: true },
      { action: "draft", csrfPresent: true, ifMatchQuoted: true },
      { action: "draft", csrfPresent: true, ifMatchQuoted: true },
    ])
    expect(mutations.filter((entry) => entry.action === "publish")).toEqual([
      { action: "publish", csrfPresent: true, ifMatchQuoted: true },
      { action: "publish", csrfPresent: true, ifMatchQuoted: true },
      { action: "publish", csrfPresent: true, ifMatchQuoted: true },
    ])
    expect(mutations.filter((entry) => entry.action === "withdraw")).toEqual([
      { action: "withdraw", csrfPresent: true, ifMatchQuoted: true },
    ])
    expect(mutations.filter((entry) => entry.action === "logout")).toEqual([
      { action: "logout", csrfPresent: true, ifMatchQuoted: false },
    ])
    await publicAudit.assertClean()
    await audit.assertClean()
  } finally {
    await publicContext.close()
  }
})

async function auditAtBothViewports(page: Page, audit: () => Promise<void>) {
  for (const viewport of [mobileViewport, desktopViewport]) {
    await page.setViewportSize(viewport)
    await audit()
  }
}

async function expectFrameWidth(
  frame: ReturnType<Page["getByTitle"]>,
  expected: number,
) {
  const width = (await frame.boundingBox())?.width ?? 0
  expect(Math.abs(width - expected)).toBeLessThanOrEqual(1)
}

async function assertFrameHasNoHorizontalOverflow(
  frame: ReturnType<Page["getByTitle"]>,
) {
  const overflow = await frame.evaluate((element) => {
    const iframe = element as HTMLIFrameElement
    const root = iframe.contentDocument?.documentElement
    if (!root) return null
    return root.scrollWidth - root.clientWidth
  })
  expect(overflow, "preview document horizontal overflow").toBe(0)
}

async function assertAdminHasNoHorizontalOverflow(page: Page) {
  const overflow = await page.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  )
  expect(overflow, "admin document horizontal overflow").toBe(0)
}

async function assertFrameFitsAvailableWidth(
  frame: ReturnType<Page["getByTitle"]>,
) {
  const geometry = await frame.evaluate((element) => {
    const frameRect = element.getBoundingClientRect()
    const containerRect = element.parentElement?.getBoundingClientRect()
    return {
      frameLeft: frameRect.left,
      frameRight: frameRect.right,
      containerLeft: containerRect?.left ?? Number.NaN,
      containerRight: containerRect?.right ?? Number.NaN,
      viewportWidth: document.documentElement.clientWidth,
    }
  })

  expect(
    geometry.frameLeft,
    "preview left edge within container",
  ).toBeGreaterThanOrEqual(geometry.containerLeft)
  expect(
    geometry.frameRight,
    "preview right edge within container",
  ).toBeLessThanOrEqual(geometry.containerRight)
  expect(
    geometry.frameLeft,
    "preview left edge within viewport",
  ).toBeGreaterThanOrEqual(0)
  expect(
    geometry.frameRight,
    "preview right edge within viewport",
  ).toBeLessThanOrEqual(geometry.viewportWidth)
}

function waitForResponse(
  page: Page,
  method: string,
  target: string,
  status: number,
) {
  const observed: string[] = []
  const collect = (response: Awaited<ReturnType<Page["waitForResponse"]>>) => {
    const url = new URL(response.url())
    if (url.origin !== adminOrigin) return
    observed.push(
      `${response.request().method()} ${url.pathname}${url.search} ${response.status()}`,
    )
  }
  page.on("response", collect)
  return page
    .waitForResponse(
      (response) => {
        const url = new URL(response.url())
        return (
          response.request().method() === method &&
          `${url.pathname}${url.search}` === target &&
          response.status() === status
        )
      },
      { timeout: 10_000 },
    )
    .catch(() => {
      throw new Error(
        `expected response ${method} ${target} ${status}; observed same-origin responses: ${observed.join(
          ", ",
        )}`,
      )
    })
    .finally(() => page.off("response", collect))
}

function createdArticleIDFromURL(value: string | URL) {
  const pathname = (value instanceof URL ? value : new URL(value)).pathname
  const match = pathname.match(/^\/admin\/articoli\/([^/]+)$/)
  if (!match) return ""
  const id = decodeURIComponent(match[1])
  return id === "nuovo" ? "" : id
}
