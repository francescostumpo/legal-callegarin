import AxeBuilder from "@axe-core/playwright"
import { expect, type Page } from "@playwright/test"

export const adminOrigin = "https://127.0.0.1:4173"
const adminCookieName = "__Host-callegarin_admin"

export type ExactAdminHTTPError = {
  method: string
  pathname: string
  search?: string
  status: 401 | 409 | 503
  count?: number
}

export type ExactAdminPostResponseAbort = {
  method: string
  pathname: string
  search?: string
  status: number
  errorText: string
  count?: number
}

type ObservedAdminHTTPError = {
  method: string
  target: string
  status: number
}

type ObservedAdminHTTPResponse = ObservedAdminHTTPError & {
  request: object
  sequence: number
}

type ObservedAdminRequestAbort = {
  request: object
  sequence: number
  method: string
  target: string
  errorText: string
}

type AdminPageAuditOptions = {
  authenticatedShell?: boolean
  cookie: "none" | "authenticated"
  focusWithin?: string
}

export function installAdminAudit(
  page: Page,
  allowedHTTPError: ExactAdminHTTPError[] = [],
  allowedPostResponseAbort: ExactAdminPostResponseAbort[] = [],
) {
  const issues: string[] = []
  const pending: Promise<void>[] = []
  const observedHTTPError: ObservedAdminHTTPError[] = []
  const observedHTTPResponse: ObservedAdminHTTPResponse[] = []
  const observedRequestAbort: ObservedAdminRequestAbort[] = []
  const resourceConsoleError: string[] = []
  let observationSequence = 0

  page.on("request", (request) => {
    const url = new URL(request.url())
    if (url.origin !== adminOrigin) {
      issues.push(
        `third-party request ${request.method()} ${url.origin}${url.pathname}`,
      )
    }
  })
  page.on("pageerror", (error) => issues.push(`pageerror: ${error.message}`))
  page.on("console", (message) => {
    if (message.type() !== "error") return
    if (chromiumResourceErrorStatus(message.text()) !== null) {
      resourceConsoleError.push(message.text())
      return
    }
    issues.push(`console error: ${message.text()}`)
  })
  page.on("requestfailed", (request) => {
    const url = new URL(request.url())
    if (url.origin === adminOrigin) {
      observedRequestAbort.push({
        request,
        sequence: ++observationSequence,
        method: request.method(),
        target: requestTarget(url),
        errorText: request.failure()?.errorText ?? "unknown",
      })
    }
  })
  page.on("response", (response) => {
    const request = response.request()
    const url = new URL(response.url())
    if (url.origin !== adminOrigin) return

    observedHTTPResponse.push({
      request,
      sequence: ++observationSequence,
      method: request.method(),
      target: requestTarget(url),
      status: response.status(),
    })

    if (response.status() >= 400) {
      observedHTTPError.push({
        method: request.method(),
        target: requestTarget(url),
        status: response.status(),
      })
    }
    pending.push(
      response.headerValue("set-cookie").then((header) => {
        if (header !== null && !isSecureAdminSetCookie(header)) {
          issues.push("admin response set an unexpected or insecure cookie")
        }
      }),
    )
  })

  return {
    async assertPage(options: AdminPageAuditOptions) {
      await page.waitForLoadState("domcontentloaded")
      await page.evaluate(
        () =>
          new Promise<void>((resolve) =>
            requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
          ),
      )
      await assertAxe(page)
      await assertSemanticStructure(page, options.authenticatedShell ?? false)
      await assertHorizontalBounds(page)
      if (options.authenticatedShell) {
        await assertDesktopAccountControlsDoNotOverlap(page)
      }
      await assertFocusAndTargetSize(page, options.focusWithin)
      await assertCookieState(page, options.cookie)
    },
    async assertClean() {
      await Promise.all(pending)
      issues.push(
        ...auditExpectedAdminHTTPErrors(
          allowedHTTPError,
          observedHTTPError,
          resourceConsoleError,
        ),
        ...auditExpectedAdminPostResponseAborts(
          allowedPostResponseAbort,
          observedHTTPResponse,
          observedRequestAbort,
        ),
      )
      expect(issues, "admin browser audit events").toEqual([])
    },
  }
}

export function auditExpectedAdminPostResponseAborts(
  allowed: ExactAdminPostResponseAbort[],
  observedResponses: ObservedAdminHTTPResponse[],
  observedAborts: ObservedAdminRequestAbort[],
) {
  const issues: string[] = []
  const credited = allowed.map(() => 0)

  for (const abort of observedAborts) {
    const allowanceIndex = allowed.findIndex((entry, index) => {
      const target = `${entry.pathname}${entry.search ?? ""}`
      if (
        credited[index] >= (entry.count ?? 1) ||
        entry.method.toUpperCase() !== abort.method.toUpperCase() ||
        target !== abort.target ||
        entry.errorText !== abort.errorText
      ) {
        return false
      }
      return observedResponses.some(
        (response) =>
          response.request === abort.request &&
          response.sequence < abort.sequence &&
          response.method.toUpperCase() === entry.method.toUpperCase() &&
          response.target === target &&
          response.status === entry.status,
      )
    })
    if (allowanceIndex < 0) {
      issues.push(
        `same-origin request failed: ${abort.method} ${abort.target} (${abort.errorText})`,
      )
      continue
    }
    credited[allowanceIndex]++
  }

  return issues
}

export function auditExpectedAdminHTTPErrors(
  allowedHTTPError: ExactAdminHTTPError[],
  observedHTTPError: ObservedAdminHTTPError[],
  resourceConsoleError: string[],
) {
  const issues: string[] = []
  const allowed = new Map(
    allowedHTTPError.map((entry) => [
      errorKey(
        entry.method,
        `${entry.pathname}${entry.search ?? ""}`,
        entry.status,
      ),
      {
        expected: entry.count ?? 1,
        observed: 0,
        consoleObserved: 0,
        status: entry.status,
      },
    ]),
  )

  for (const event of observedHTTPError) {
    const key = errorKey(event.method, event.target, event.status)
    const allowance = allowed.get(key)
    if (allowance && allowance.observed < allowance.expected) {
      allowance.observed++
    } else {
      issues.push(`unexpected HTTP ${key}`)
    }
  }

  for (const message of resourceConsoleError) {
    const status = chromiumResourceErrorStatus(message)
    const allowance = [...allowed.values()].find(
      (candidate) =>
        candidate.status === status &&
        candidate.consoleObserved < candidate.observed,
    )
    if (allowance) {
      allowance.consoleObserved++
    } else {
      issues.push(`console error: ${message}`)
    }
  }

  for (const [key, allowance] of allowed) {
    if (allowance.observed !== allowance.expected) {
      issues.push(
        `expected HTTP ${key} ${allowance.expected} time(s), observed ${allowance.observed}`,
      )
    }
  }
  return issues
}

function chromiumResourceErrorStatus(message: string) {
  const match =
    /^Failed to load resource: the server responded with a status of (401|409|503) \((Unauthorized|Conflict|Service Unavailable)?\)$/.exec(
      message,
    )
  if (!match) return null
  const status = Number.parseInt(match[1], 10)
  const expectedReason =
    status === 401
      ? "Unauthorized"
      : status === 409
        ? "Conflict"
        : "Service Unavailable"
  return match[2] === undefined || match[2] === expectedReason ? status : null
}

function isSecureAdminSetCookie(header: string) {
  const parts = header.split(";").map((part) => part.trim())
  const cookiePair = parts.shift() ?? ""
  const separator = cookiePair.indexOf("=")
  if (separator < 1 || cookiePair.slice(0, separator) !== adminCookieName) {
    return false
  }
  const attributes = new Set(parts.map((part) => part.toLowerCase()))
  return (
    attributes.has("secure") &&
    attributes.has("httponly") &&
    attributes.has("samesite=strict") &&
    attributes.has("path=/")
  )
}

async function assertCookieState(
  page: Page,
  expected: AdminPageAuditOptions["cookie"],
) {
  const cookies = (await page.context().cookies(adminOrigin)).map((cookie) => ({
    name: cookie.name,
    httpOnly: cookie.httpOnly,
    secure: cookie.secure,
    sameSite: cookie.sameSite,
    path: cookie.path,
  }))
  if (expected === "none") {
    expect(cookies, "pre-authentication cookie metadata").toEqual([])
    return
  }
  expect(cookies, "authenticated cookie metadata").toEqual([
    {
      name: adminCookieName,
      httpOnly: true,
      secure: true,
      sameSite: "Strict",
      path: "/",
    },
  ])
}

async function assertAxe(page: Page) {
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
    .analyze()
  expect(
    results.violations.map((violation) => ({
      id: violation.id,
      impact: violation.impact,
      targets: violation.nodes.map((node) => node.target.join(" ")),
    })),
    "admin WCAG A/AA violations",
  ).toEqual([])
}

async function assertSemanticStructure(
  page: Page,
  authenticatedShell: boolean,
) {
  await expect(page.locator("main:visible"), "one visible main").toHaveCount(1)
  await expect(
    page.locator("h1:visible"),
    "exactly one visible H1",
  ).toHaveCount(1)
  if (authenticatedShell) {
    await expect(page.getByRole("banner"), "one banner landmark").toHaveCount(1)
    expect(
      await page.locator("nav[aria-label]").count(),
      "an identified admin navigation landmark",
    ).toBeGreaterThanOrEqual(1)
    await expect(
      page.locator('a[href="#contenuto"]'),
      "one skip link to the workspace",
    ).toHaveCount(1)
  }

  const levels = await page
    .locator(
      "h1:visible, h2:visible, h3:visible, h4:visible, h5:visible, h6:visible",
    )
    .evaluateAll((headings) =>
      headings.map((heading) => Number(heading.tagName.slice(1))),
    )
  const jumps: string[] = []
  for (let index = 1; index < levels.length; index++) {
    if (levels[index] > levels[index - 1] + 1) {
      jumps.push(
        `H${levels[index - 1]} to H${levels[index]} at heading ${index + 1}`,
      )
    }
  }
  expect(jumps, "admin heading levels must not jump").toEqual([])
}

async function assertHorizontalBounds(page: Page) {
  const result = await page.evaluate(() => {
    const viewportWidth = document.documentElement.clientWidth
    const clipped: string[] = []
    for (const element of document.body.querySelectorAll<HTMLElement>("*")) {
      const style = getComputedStyle(element)
      const bounds = element.getBoundingClientRect()
      if (
        style.display === "none" ||
        style.visibility === "hidden" ||
        Number(style.opacity) === 0 ||
        bounds.width === 0 ||
        bounds.height === 0
      ) {
        continue
      }
      if (bounds.left < -1 || bounds.right > viewportWidth + 1) {
        clipped.push(
          `${element.tagName.toLowerCase()}${element.id ? `#${element.id}` : ""}${element.classList.length ? `.${[...element.classList].join(".")}` : ""}: ${bounds.left.toFixed(1)}..${bounds.right.toFixed(1)} / ${viewportWidth}`,
        )
      }
    }
    return {
      overflow:
        document.documentElement.scrollWidth -
        document.documentElement.clientWidth,
      clipped,
    }
  })
  expect(result.clipped, "admin elements clipped horizontally").toEqual([])
  expect(
    result.overflow,
    "admin document horizontal overflow",
  ).toBeLessThanOrEqual(0)
}

async function assertDesktopAccountControlsDoNotOverlap(page: Page) {
  if ((page.viewportSize()?.width ?? 0) < 896) return

  const [operator, logout] = await Promise.all([
    page.locator(".operator:visible").boundingBox(),
    page.locator(".logout:visible").boundingBox(),
  ])
  expect(operator, "visible desktop operator bounds").not.toBeNull()
  expect(logout, "visible desktop logout bounds").not.toBeNull()
  if (!operator || !logout) return

  const intersects =
    operator.x < logout.x + logout.width &&
    operator.x + operator.width > logout.x &&
    operator.y < logout.y + logout.height &&
    operator.y + operator.height > logout.y
  expect(intersects, "desktop operator and logout must not overlap").toBe(false)
}

async function assertFocusAndTargetSize(page: Page, focusWithin?: string) {
  const result = await page.evaluate(async (scopeSelector) => {
    const selector =
      'a[href], button:not([disabled]), input:not([type="hidden"]):not([disabled]), textarea:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'
    const focusFailures: string[] = []
    const sizeFailures: string[] = []
    const scope = scopeSelector
      ? document.querySelector<HTMLElement>(scopeSelector)
      : document
    if (!scope) {
      return {
        focusFailures: [`focus scope not found: ${scopeSelector}`],
        sizeFailures,
      }
    }

    for (const element of scope.querySelectorAll<HTMLElement>(selector)) {
      if (
        !element.checkVisibility({
          checkOpacity: true,
          checkVisibilityCSS: true,
        })
      ) {
        continue
      }
      element.scrollIntoView({ block: "center", inline: "center" })
      await nextFrame()
      const beforeFocus = element.getBoundingClientRect()
      if (beforeFocus.width === 0 || beforeFocus.height === 0) continue
      element.focus({ preventScroll: true })
      await nextFrame()
      const bounds = element.getBoundingClientRect()
      const inside =
        bounds.left >= -1 &&
        bounds.right <= innerWidth + 1 &&
        bounds.top >= -1 &&
        bounds.bottom <= innerHeight + 1
      const center = inside
        ? document.elementFromPoint(
            bounds.left + bounds.width / 2,
            bounds.top + bounds.height / 2,
          )
        : null
      const obstruction =
        inside && center !== element && !element.contains(center)
          ? center instanceof HTMLElement
            ? describe(center)
            : String(center)
          : ""
      const active = document.activeElement === element
      if (!active || !inside || obstruction) {
        focusFailures.push(
          `${describe(element)} bounds=${formatBounds(bounds)} viewport=${innerWidth}x${innerHeight} reason=${[
            !active ? "focus" : "",
            !inside ? "bounds" : "",
            obstruction ? `obscured by ${obstruction}` : "",
          ]
            .filter(Boolean)
            .join("+")}`,
        )
      }

      const inlineProse =
        element instanceof HTMLAnchorElement &&
        getComputedStyle(element).display === "inline" &&
        element.closest("p, label, dd") !== null
      if (inlineProse) continue

      let targetBounds = bounds
      if (
        element instanceof HTMLInputElement &&
        ["checkbox", "radio"].includes(element.type)
      ) {
        const label =
          element.closest("label") ??
          (element.id
            ? document.querySelector<HTMLLabelElement>(
                `label[for="${CSS.escape(element.id)}"]`,
              )
            : null)
        if (label) targetBounds = union(bounds, label.getBoundingClientRect())
      }
      if (targetBounds.width < 44 || targetBounds.height < 44) {
        sizeFailures.push(
          `${describe(element)}: ${targetBounds.width.toFixed(1)}x${targetBounds.height.toFixed(1)}`,
        )
      }
    }
    return { focusFailures, sizeFailures }

    function nextFrame() {
      return new Promise<void>((resolve) =>
        requestAnimationFrame(() => resolve()),
      )
    }
    function describe(element: HTMLElement) {
      const text =
        element.getAttribute("aria-label") || element.textContent?.trim() || ""
      return `${element.tagName.toLowerCase()}${element.id ? `#${element.id}` : ""}${element.classList.length ? `.${[...element.classList].join(".")}` : ""}${text ? ` “${text.slice(0, 45)}”` : ""}`
    }
    function formatBounds(bounds: DOMRect) {
      return `${bounds.left.toFixed(1)},${bounds.top.toFixed(1)} ${bounds.width.toFixed(1)}x${bounds.height.toFixed(1)}`
    }
    function union(first: DOMRect, second: DOMRect) {
      const left = Math.min(first.left, second.left)
      const top = Math.min(first.top, second.top)
      const right = Math.max(first.right, second.right)
      const bottom = Math.max(first.bottom, second.bottom)
      return new DOMRect(left, top, right - left, bottom - top)
    }
  }, focusWithin)

  expect(result.focusFailures, "admin focus targets").toEqual([])
  expect(
    result.sizeFailures,
    "admin standalone targets smaller than 44x44",
  ).toEqual([])
}

function requestTarget(url: URL) {
  return `${url.pathname}${url.search}`
}

function errorKey(method: string, target: string, status: number) {
  return `${method.toUpperCase()} ${target} ${status}`
}
