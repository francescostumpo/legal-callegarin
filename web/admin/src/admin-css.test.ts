import { readFileSync } from "node:fs"
import { expect, it } from "vitest"

const adminCSS = readFileSync("src/admin.css", "utf8")

it("keeps the wordmark touch target at least 44 pixels high", () => {
  expect(adminCSS).toMatch(/\.wordmark\s*\{[^}]*min-height:\s*44px;/s)
})

it("keeps the dashboard queue link at least 44 pixels high", () => {
  expect(adminCSS).toMatch(
    /\.notification-card a\s*\{[^}]*min-height:\s*44px;/s,
  )
})
