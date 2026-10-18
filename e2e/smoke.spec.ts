import { expect, test } from "@playwright/test"

const publishedArticleTitle = "Affidamento condiviso: guida essenziale"
const e2eOrigin = "https://127.0.0.1:4173"

// Playwright traces serialize form bodies; authentication secrets must never
// be retained as test artifacts.
test.use({ trace: "off" })

test("@smoke public site and authenticated console are available", async ({
  context,
  page,
  request,
}) => {
  const readiness = await request.get("/health/ready")
  expect(readiness.ok()).toBeTruthy()
  expect(await readiness.text()).toBe("ok\n")

  await page.goto("/")
  await expect(
    page.getByRole("heading", {
      level: 1,
      name: "Assistenza legale chiara e rigorosa, vicina alle persone e alle loro esigenze.",
    }),
  ).toBeVisible()
  await expect(
    page.getByRole("link", { name: publishedArticleTitle }),
  ).toBeVisible()

  await page.goto("/sentenze-e-riflessioni/affidamento-condiviso-guida-e2e")
  await expect(
    page.getByRole("heading", { level: 1, name: publishedArticleTitle }),
  ).toBeVisible()

  await page.goto("/admin/login")
  await expect(
    page.getByRole("heading", { level: 1, name: "Accesso amministrazione" }),
  ).toBeVisible()
  const username = process.env.E2E_ADMIN_USERNAME
  const password = process.env.E2E_ADMIN_PASSWORD
  expect(username, "launcher-provided E2E username").toBeTruthy()
  expect(password, "launcher-provided E2E password").toBeTruthy()
  await page.getByLabel("Nome utente").fill(username!)
  await page.getByLabel("Password").fill(password!)
  let loginOrigin = "missing"
  page.on("request", (browserRequest) => {
    if (
      browserRequest.method() === "POST" &&
      browserRequest.url() === `${e2eOrigin}/admin/login`
    ) {
      loginOrigin = browserRequest.headers().origin ?? "missing"
    }
  })
  const loginResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      response.url() === `${e2eOrigin}/admin/login`,
  )
  await page.getByRole("button", { name: "Accedi" }).click()
  expect((await loginResponse).status()).toBe(303)
  expect(loginOrigin).toBe(e2eOrigin)

  await expect(
    page.getByRole("heading", { level: 1, name: "Panoramica" }),
  ).toBeVisible()
  const sessionCookie = (await context.cookies()).find(
    (cookie) => cookie.name === "__Host-callegarin_admin",
  )
  expect(sessionCookie).toMatchObject({
    secure: true,
    httpOnly: true,
    sameSite: "Strict",
    path: "/",
  })
})
