import AxeBuilder from "@axe-core/playwright"
import { expect, type Page } from "@playwright/test"

export const publicOrigin = "https://127.0.0.1:4173"

export type ExactHTTPError = {
  method: string
  pathname: string
  status: number
  count?: number
}

type PublicPageAuditOptions = {
  focusWithin?: string
}

export function installPublicAudit(
  page: Page,
  allowedHTTPError: ExactHTTPError[] = [],
) {
  const issues: string[] = []
  const pending: Promise<void>[] = []
  const allowed = new Map(
    allowedHTTPError.map((entry) => [
      errorKey(entry.method, entry.pathname, entry.status),
      { expected: entry.count ?? 1, observed: 0, consoleObserved: 0 },
    ]),
  )

  page.on("request", (request) => {
    const url = new URL(request.url())
    if (url.origin !== publicOrigin) {
      issues.push(
        `third-party request ${request.method()} ${url.origin}${url.pathname}`,
      )
    }
  })
  page.on("pageerror", (error) => issues.push(`pageerror: ${error.message}`))
  page.on("console", (message) => {
    if (
      message.type() === "error" &&
      !isAllowedNavigationConsoleError(message.text(), message.location().url)
    ) {
      issues.push(`console error: ${message.text()}`)
    }
  })
  page.on("requestfailed", (request) => {
    const url = new URL(request.url())
    if (url.origin === publicOrigin) {
      issues.push(
        `same-origin request failed: ${request.method()} ${url.pathname} (${request.failure()?.errorText ?? "unknown"})`,
      )
    }
  })
  page.on("response", (response) => {
    const request = response.request()
    const url = new URL(response.url())
    if (url.origin !== publicOrigin) return

    if (response.status() >= 400) {
      const key = errorKey(
        request.method(),
        requestTarget(url),
        response.status(),
      )
      const allowance = allowed.get(key)
      if (allowance && allowance.observed < allowance.expected) {
        allowance.observed++
      } else {
        issues.push(`unexpected HTTP ${key}`)
      }
    }
    pending.push(
      response.headerValue("set-cookie").then((cookie) => {
        if (cookie !== null) {
          issues.push(
            `public response set a cookie: ${request.method()} ${url.pathname}`,
          )
        }
      }),
    )
  })

  return {
    async assertPage(options: PublicPageAuditOptions = {}) {
      await page.waitForLoadState("networkidle")
      await assertAxe(page)
      await assertSemanticStructure(page)
      await assertHorizontalBounds(page)
      await assertFocusAndTargetSize(page, options.focusWithin)
    },
    async assertClean() {
      await Promise.all(pending)
      for (const [key, allowance] of allowed) {
        if (allowance.observed !== allowance.expected) {
          issues.push(
            `expected HTTP ${key} ${allowance.expected} time(s), observed ${allowance.observed}`,
          )
        }
      }
      expect(issues, "public browser audit events").toEqual([])
    },
  }

  function isAllowedNavigationConsoleError(message: string, location: string) {
    if (!location) return false
    const url = new URL(location)
    if (url.origin !== publicOrigin) return false

    for (const entry of allowedHTTPError) {
      if (
        requestTarget(url) !== entry.pathname ||
        message !==
          `Failed to load resource: the server responded with a status of ${entry.status} ()`
      ) {
        continue
      }
      const allowance = allowed.get(
        errorKey(entry.method, entry.pathname, entry.status),
      )
      if (allowance && allowance.consoleObserved < allowance.expected) {
        allowance.consoleObserved++
        return true
      }
    }
    return false
  }
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
    "WCAG A/AA violations",
  ).toEqual([])
}

async function assertSemanticStructure(page: Page) {
  await expect(page.locator("main:visible"), "one visible main").toHaveCount(1)
  await expect(page.getByRole("banner"), "one banner landmark").toHaveCount(1)
  await expect(
    page.getByRole("contentinfo"),
    "one footer landmark",
  ).toHaveCount(1)
  await expect(page.locator("h1"), "exactly one H1").toHaveCount(1)

  const levels = await page
    .locator("h1, h2, h3, h4, h5, h6")
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
  expect(jumps, "heading levels must not increase by more than one").toEqual([])
}

async function assertHorizontalBounds(page: Page) {
  const result = await page.evaluate(() => {
    const tolerance = 1
    const viewportWidth = document.documentElement.clientWidth
    const clipped: string[] = []
    for (const element of document.body.querySelectorAll<HTMLElement>("*")) {
      if (
        element.closest('[aria-hidden="true"], .form-honeypot') ||
        element.matches(".visually-hidden, .skip-link:not(:focus)")
      ) {
        continue
      }
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
      if (
        bounds.left < -tolerance ||
        bounds.right > viewportWidth + tolerance
      ) {
        clipped.push(
          `${element.tagName.toLowerCase()}${element.id ? `#${element.id}` : ""}${element.classList.length ? `.${[...element.classList].join(".")}` : ""}: ${bounds.left.toFixed(1)}..${bounds.right.toFixed(1)} / ${viewportWidth}`,
        )
      }
    }
    return {
      documentOverflow:
        document.documentElement.scrollWidth -
        document.documentElement.clientWidth,
      clipped,
    }
  })
  expect(
    result.documentOverflow,
    "document horizontal overflow",
  ).toBeLessThanOrEqual(0)
  expect(result.clipped, "visible elements clipped horizontally").toEqual([])
}

async function assertFocusAndTargetSize(page: Page, focusWithin?: string) {
  const result = await page.evaluate(async (scopeSelector) => {
    const selector =
      'a[href], button:not([disabled]), summary, input:not([type="hidden"]):not([disabled]), textarea:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'
    const focusFailures: string[] = []
    const sizeFailures: string[] = []
    const describe = (element: HTMLElement) => {
      const text =
        element.textContent?.trim() ||
        element.getAttribute("aria-label") ||
        element.querySelector("img")?.alt ||
        ""
      return `${element.tagName.toLowerCase()}${element.id ? `#${element.id}` : ""}${element.classList.length ? `.${[...element.classList].join(".")}` : ""}${text ? ` “${text.slice(0, 45)}”` : ""}`
    }
    const formatBounds = (bounds: DOMRect) =>
      `${bounds.left.toFixed(1)},${bounds.top.toFixed(1)} ${bounds.width.toFixed(1)}x${bounds.height.toFixed(1)}`
    const boundsInsideViewport = (bounds: DOMRect) =>
      bounds.left >= -1 &&
      bounds.right <= innerWidth + 1 &&
      bounds.top >= -1 &&
      bounds.bottom <= innerHeight + 1

    const previousScrollBehavior = document.documentElement.style.scrollBehavior
    document.documentElement.style.scrollBehavior = "auto"

    try {
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
          element.closest('[aria-hidden="true"], .form-honeypot') ||
          !element.checkVisibility({
            checkOpacity: true,
            checkVisibilityCSS: true,
          })
        ) {
          continue
        }
        element.scrollIntoView({ block: "center", inline: "center" })
        await new Promise((resolve) =>
          requestAnimationFrame(() => resolve(null)),
        )
        const beforeFocus = element.getBoundingClientRect()
        if (beforeFocus.width === 0 || beforeFocus.height === 0) continue
        element.focus({ preventScroll: true })
        await new Promise((resolve) =>
          requestAnimationFrame(() => resolve(null)),
        )
        const focusedBounds = element.getBoundingClientRect()
        const activeMatches = document.activeElement === element
        const insideViewport = boundsInsideViewport(focusedBounds)
        const center = document.elementFromPoint(
          Math.min(
            innerWidth - 1,
            Math.max(0, focusedBounds.left + focusedBounds.width / 2),
          ),
          Math.min(
            innerHeight - 1,
            Math.max(0, focusedBounds.top + focusedBounds.height / 2),
          ),
        )
        const obstruction =
          insideViewport && center !== element && !element.contains(center)
            ? center instanceof HTMLElement
              ? describe(center)
              : String(center)
            : ""
        if (!activeMatches || !insideViewport || obstruction) {
          const active =
            document.activeElement instanceof HTMLElement
              ? describe(document.activeElement)
              : String(document.activeElement)
          const href =
            element instanceof HTMLAnchorElement
              ? ` href=${JSON.stringify(element.getAttribute("href"))}`
              : ""
          focusFailures.push(
            `${describe(element)}${href} bounds=${formatBounds(focusedBounds)} viewport=${innerWidth}x${innerHeight} active=${active} reason=${[
              !activeMatches ? "focus" : "",
              !insideViewport ? "bounds" : "",
              obstruction ? `obscured by ${obstruction}` : "",
            ]
              .filter(Boolean)
              .join("+")}`,
          )
        }

        const isInlineProseAnchor =
          element instanceof HTMLAnchorElement &&
          (element.closest("label") !== null ||
            (getComputedStyle(element).display === "inline" &&
              element.closest("p") !== null))
        if (isInlineProseAnchor) {
          continue
        }
        let targetBounds = focusedBounds
        if (
          element instanceof HTMLInputElement &&
          ["checkbox", "radio"].includes(element.type)
        ) {
          const label = element.id
            ? document.querySelector<HTMLLabelElement>(
                `label[for="${CSS.escape(element.id)}"]`,
              )
            : null
          if (label) {
            const labelBounds = label.getBoundingClientRect()
            const left = Math.min(focusedBounds.left, labelBounds.left)
            const top = Math.min(focusedBounds.top, labelBounds.top)
            const right = Math.max(focusedBounds.right, labelBounds.right)
            const bottom = Math.max(focusedBounds.bottom, labelBounds.bottom)
            targetBounds = new DOMRect(left, top, right - left, bottom - top)
          }
        }
        if (targetBounds.width < 44 || targetBounds.height < 44) {
          sizeFailures.push(
            `${describe(element)}: ${targetBounds.width.toFixed(1)}x${targetBounds.height.toFixed(1)}`,
          )
        }
      }
    } finally {
      document.documentElement.style.scrollBehavior = previousScrollBehavior
    }
    return { focusFailures, sizeFailures }
  }, focusWithin)

  expect(
    result.focusFailures,
    "focus targets clipped outside viewport",
  ).toEqual([])
  expect(
    result.sizeFailures,
    "standalone targets smaller than 44x44 CSS px",
  ).toEqual([])
}

function errorKey(method: string, pathname: string, status: number) {
  return `${method.toUpperCase()} ${pathname} ${status}`
}

function requestTarget(url: URL) {
  return `${url.pathname}${url.search}`
}
