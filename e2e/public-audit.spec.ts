import { expect, test } from "@playwright/test"

import { installPublicAudit } from "./support/public-audit"

test.describe("public audit negative regressions", () => {
  test.skip(
    ({ browserName }) => browserName !== "chromium",
    "Chromium-only public audit suite",
  )

  test("fails a focus target whose center is obscured", async ({ page }) => {
    const audit = installPublicAudit(page)
    await page.setContent(`<!doctype html>
      <html lang="it">
        <head>
          <meta name="viewport" content="width=device-width, initial-scale=1">
          <title>Target parzialmente coperto</title>
          <style>
            body { margin: 0; background: white; color: black; }
            .fixture { position: relative; width: 12rem; height: 6rem; }
            .target { position: absolute; top: 0; left: 0; display: block; width: 7.5rem; height: 2.75rem; line-height: 2.75rem; }
            .cover { position: absolute; z-index: 2; top: 0; left: 3.25rem; width: 1rem; height: 2.75rem; background: black; }
          </style>
        </head>
        <body>
          <header>Testata</header>
          <main>
            <h1>Target parzialmente coperto</h1>
            <div class="fixture">
              <a class="target" href="#destinazione">Continua</a>
              <span class="cover" aria-hidden="true"></span>
            </div>
            <div id="destinazione">Destinazione</div>
          </main>
          <footer>Piè di pagina</footer>
        </body>
      </html>`)

    await expect(audit.assertPage()).rejects.toThrow(/reason=obscured/)
  })

  test("fails a small standalone anchor even when its display is inline", async ({
    page,
  }) => {
    const audit = installPublicAudit(page)
    await page.setContent(`<!doctype html>
      <html lang="it">
        <head>
          <meta name="viewport" content="width=device-width, initial-scale=1">
          <title>Link standalone piccolo</title>
          <style>body { background: white; color: black; }</style>
        </head>
        <body>
          <header>Testata</header>
          <main>
            <h1>Link standalone piccolo</h1>
            <a href="#destinazione" style="font-size: 10px">Continua</a>
            <div id="destinazione">Destinazione</div>
          </main>
          <footer>Piè di pagina</footer>
        </body>
      </html>`)

    await expect(audit.assertPage()).rejects.toThrow(
      /standalone targets smaller than 44x44 CSS px/,
    )
  })

  test("rejects an allowlisted status when the request has an unexpected query", async ({
    page,
  }) => {
    const audit = installPublicAudit(page, [
      { method: "POST", pathname: "/contatti", status: 422 },
    ])
    await page.goto("/contatti")
    await page.waitForTimeout(3_100)
    await page
      .locator("form[data-contact-form]")
      .evaluate((form) => form.setAttribute("action", "/contatti?unexpected=1"))
    await page.getByLabel("Nome e cognome").fill("A")
    await page.getByLabel("Email", { exact: true }).fill("non-valida")
    await page.getByLabel("Messaggio breve").fill("breve")
    const invalidResponse = page.waitForResponse(
      (response) =>
        response.status() === 422 &&
        new URL(response.url()).search === "?unexpected=1",
    )
    await page.getByRole("button", { name: "Invia la richiesta" }).click()
    await invalidResponse

    let failure = ""
    try {
      await audit.assertClean()
    } catch (error) {
      failure = String(error)
    }
    expect(failure).toContain("unexpected HTTP POST /contatti?unexpected=1 422")
    expect(failure).toContain(
      "expected HTTP POST /contatti 422 1 time(s), observed 0",
    )
  })
})
