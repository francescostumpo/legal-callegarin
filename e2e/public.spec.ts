import { expect, test } from "@playwright/test"

import { installPublicAudit } from "./support/public-audit"

const articleTitle = "Affidamento condiviso: guida essenziale"
const articleSlug = "/sentenze-e-riflessioni/affidamento-condiviso-guida-e2e"
const draftSlug = "/sentenze-e-riflessioni/bozza-riservata-e2e"
const draftTitle = "Bozza riservata per il collaudo"

test.describe("public Chromium journeys", () => {
  test.skip(
    ({ browserName }) => browserName !== "chromium",
    "Chromium-only public suite",
  )

  test("mobile navigation restores focus and reaches practice areas", async ({
    context,
    page,
  }) => {
    await page.setViewportSize({ width: 360, height: 800 })
    const audit = installPublicAudit(page)
    await page.goto("/")

    const menu = page.locator("summary").first()
    await expect(menu).toContainText("Menu")
    await menu.click()
    const mobileNavigation = page.getByRole("navigation", {
      name: "Navigazione principale mobile",
    })
    const profile = mobileNavigation.getByRole("link", { name: "Profilo" })
    await expect(profile).toBeFocused()
    await page.keyboard.press("Escape")
    await expect(menu).toBeFocused()
    await expect(mobileNavigation).toBeHidden()

    await menu.click()
    await audit.assertPage({ focusWithin: "[data-navigation][open]" })
    await mobileNavigation
      .getByRole("link", { name: "Aree di attività", exact: true })
      .click()
    await expect(
      page.getByRole("heading", { level: 1, name: "Aree di attività" }),
    ).toBeVisible()
    await audit.assertPage()
    await audit.assertClean()
    expect(await context.cookies()).toEqual([])
  })

  test("practice journey exposes family assistance and its related article", async ({
    context,
    page,
  }) => {
    const audit = installPublicAudit(page)
    await page.goto("/")
    await page
      .getByRole("link", { name: "Aree di attività", exact: true })
      .first()
      .click()
    await expect(
      page.getByRole("heading", { level: 1, name: "Aree di attività" }),
    ).toBeVisible()
    await page
      .getByRole("link", { name: "Famiglia e persone", exact: true })
      .first()
      .click()
    await expect(
      page.getByRole("heading", { level: 1, name: "Famiglia e persone" }),
    ).toBeVisible()
    await expect(
      page.getByRole("heading", { level: 2, name: "Ambiti di assistenza" }),
    ).toBeVisible()
    await expect(page.getByRole("link", { name: articleTitle })).toBeVisible()
    await audit.assertPage()
    await audit.assertClean()
    expect(await context.cookies()).toEqual([])
  })

  test("article index publishes only the public snapshot and draft stays private", async ({
    context,
    page,
  }) => {
    const audit = installPublicAudit(page, [
      { method: "GET", pathname: draftSlug, status: 404 },
    ])
    await page.goto("/sentenze-e-riflessioni")
    await expect(
      page.getByRole("heading", { level: 1, name: "Sentenze e riflessioni" }),
    ).toBeVisible()
    await expect(page.getByText(draftTitle)).toHaveCount(0)
    await page.getByRole("link", { name: articleTitle }).first().click()
    await expect(
      page.getByRole("heading", { level: 1, name: articleTitle }),
    ).toBeVisible()
    await expect(
      page.getByText(
        "Questo contenuto sintetico verifica la pubblicazione dell'articolo.",
      ),
    ).toBeVisible()
    await expect(page.locator(".article-byline time")).toHaveCount(2)
    await expect(
      page.getByRole("link", { name: /Approfondisci: Famiglia e persone/ }),
    ).toBeVisible()
    await audit.assertPage()

    const draftResponse = await page.goto(draftSlug)
    expect(draftResponse?.status()).toBe(404)
    await expect(
      page.getByRole("heading", {
        level: 1,
        name: "La pagina non è disponibile",
      }),
    ).toBeVisible()
    await expect(page.getByText(draftTitle)).toHaveCount(0)
    await expect(page.getByText(/deve restare in stato di bozza/i)).toHaveCount(
      0,
    )
    await audit.assertPage()
    await audit.assertClean()
    expect(await context.cookies()).toEqual([])
  })

  test("enhanced contact form validates fields and follows one successful PRG", async ({
    context,
    page,
  }) => {
    test.setTimeout(90_000)
    const audit = installPublicAudit(page, [
      { method: "POST", pathname: "/contatti", status: 422 },
    ])
    let postCount = 0
    page.on("request", (request) => {
      if (
        request.method() === "POST" &&
        new URL(request.url()).pathname === "/contatti"
      ) {
        postCount++
      }
    })
    await page.goto("/contatti")
    await page.waitForTimeout(3_100)

    await page.getByLabel("Nome e cognome").fill("A")
    await page.getByLabel("Email", { exact: true }).fill("indirizzo-non-valido")
    await page.getByLabel("Messaggio breve").fill("troppo breve")
    const invalidResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" && response.status() === 422,
    )
    await page.getByRole("button", { name: "Invia la richiesta" }).click()
    expect((await invalidResponse).status()).toBe(422)

    const summary = page.locator("[data-contact-feedback]")
    await expect(summary).toHaveAttribute("role", "alert")
    await expect(summary.getByRole("link")).toHaveCount(4)
    for (const field of ["name", "email", "message", "privacy"]) {
      await expect(page.locator(`#${field}`)).toHaveAttribute(
        "aria-invalid",
        "true",
      )
      await expect(page.locator(`#contact-${field}-error`)).toBeVisible()
    }
    await expect(page.getByLabel("Nome e cognome")).toHaveValue("A")
    await expect(page.getByLabel("Email", { exact: true })).toHaveValue(
      "indirizzo-non-valido",
    )
    await expect(page.getByLabel("Messaggio breve")).toHaveValue("troppo breve")
    await audit.assertPage()

    await page.getByLabel("Nome e cognome").fill("Persona Browser E2E")
    await page
      .getByLabel("Email", { exact: true })
      .fill("browser-public@example.test")
    await page.getByLabel("Telefono (facoltativo)").fill("+39 000 0000099")
    await page
      .getByLabel("Messaggio breve")
      .fill("Richiesta interamente sintetica per il collaudo browser pubblico.")
    await page.getByLabel(/Confermo di aver letto/).check()
    const successURL = page.waitForURL(/\/contatti\?esito=/)
    await page.getByRole("button", { name: "Invia la richiesta" }).click()
    await successURL
    await expect(
      page.getByRole("heading", { name: "Richiesta ricevuta" }),
    ).toBeVisible()
    await expect(page.getByLabel("Nome e cognome")).toHaveValue("")
    await expect(page.getByLabel("Email", { exact: true })).toHaveValue("")
    await expect(page.getByLabel("Messaggio breve")).toHaveValue("")
    await expect(page.getByLabel(/Confermo di aver letto/)).not.toBeChecked()
    expect(postCount).toBe(2)
    await page.reload()
    expect(postCount).toBe(2)
    await audit.assertPage()
    await audit.assertClean()
    expect(await context.cookies()).toEqual([])
  })

  test("privacy page explains the cookie-free first-party site", async ({
    context,
    page,
  }) => {
    const audit = installPublicAudit(page)
    await page.goto("/contatti")
    await page
      .getByRole("link", { name: "informativa privacy" })
      .first()
      .click()
    await expect(
      page.getByRole("heading", { level: 1, name: "Privacy e cookie policy" }),
    ).toBeVisible()
    await expect(
      page.getByText(
        /Le pagine pubbliche non impostano cookie, non usano strumenti di analisi e non richiedono risorse da servizi terzi/,
      ),
    ).toBeVisible()
    await expect(page.getByRole("dialog")).toHaveCount(0)
    await expect(
      page.getByRole("button", { name: /accetta.*cookie/i }),
    ).toHaveCount(0)
    await audit.assertPage()
    await audit.assertClean()
    expect(await context.cookies()).toEqual([])
  })

  test("designed not-found page returns home without leaking errors", async ({
    context,
    page,
  }) => {
    const audit = installPublicAudit(page, [
      { method: "GET", pathname: "/pagina-inesistente-e2e", status: 404 },
    ])
    const response = await page.goto("/pagina-inesistente-e2e")
    expect(response?.status()).toBe(404)
    await expect(
      page.getByRole("heading", {
        level: 1,
        name: "La pagina non è disponibile",
      }),
    ).toBeVisible()
    await audit.assertPage()
    await page.getByRole("link", { name: "Torna alla pagina iniziale" }).click()
    await expect(
      page.getByRole("heading", {
        level: 1,
        name: "Ascolto, visione e rigore per ciò che conta davvero.",
      }),
    ).toBeVisible()
    await audit.assertClean()
    expect(await context.cookies()).toEqual([])
  })

  test("canonical public HTTP sweep is cookie-free", async ({
    context,
    request,
  }) => {
    const routes = [
      "/",
      "/profilo",
      "/approccio",
      "/aree-di-attivita",
      "/aree-di-attivita/famiglia-e-persone",
      "/aree-di-attivita/successioni-e-donazioni",
      "/aree-di-attivita/obbligazioni-e-contratti",
      "/aree-di-attivita/recupero-crediti",
      "/aree-di-attivita/risarcimento-danni",
      "/aree-di-attivita/diritti-reali",
      "/aree-di-attivita/diritto-penale",
      "/aree-di-attivita/diritto-tributario",
      "/sentenze-e-riflessioni",
      articleSlug,
      "/contatti",
      "/privacy-cookie-policy",
      "/robots.txt",
      "/sitemap.xml",
    ]
    for (const route of routes) {
      const response = await request.get(route)
      expect(response.status(), route).toBe(200)
      expect(response.headers()["set-cookie"], route).toBeUndefined()
    }
    expect(await context.cookies()).toEqual([])
  })

  test("five exact viewports pass the representative public audit matrix", async ({
    context,
    page,
  }) => {
    test.setTimeout(240_000)
    const audit = installPublicAudit(page)
    const viewports = [
      { width: 360, height: 800 },
      { width: 390, height: 844 },
      { width: 768, height: 1024 },
      { width: 1440, height: 900 },
      { width: 1920, height: 1080 },
    ]
    const routes = ["/", "/aree-di-attivita", articleSlug, "/contatti"]

    for (const viewport of viewports) {
      await test.step(`${viewport.width}x${viewport.height}`, async () => {
        await page.setViewportSize(viewport)
        for (const route of routes) {
          await page.goto(route)
          await audit.assertPage()
        }
        if (viewport.width <= 768) {
          await page.goto("/")
          await page.locator("summary").first().click()
          await expect(
            page.getByRole("navigation", {
              name: "Navigazione principale mobile",
            }),
          ).toBeVisible()
          await audit.assertPage({ focusWithin: "[data-navigation][open]" })
        } else {
          await page.goto("/")
          await expect(
            page.getByRole("navigation", {
              name: "Navigazione principale",
              exact: true,
            }),
          ).toBeVisible()
          await expect(page.locator("summary").first()).toBeHidden()
        }
      })
    }
    await audit.assertClean()
    expect(await context.cookies()).toEqual([])
  })
})
