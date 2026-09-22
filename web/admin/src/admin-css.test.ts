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

it("uses an AA copper tone for article card metadata and title links", () => {
  expect(adminCSS).toMatch(
    /\.article-admin-card \.eyebrow,\s*\.article-admin-card h2 a\s*\{[^}]*color:\s*var\(--copper-dark\);/s,
  )

  const copperDark = adminCSS.match(/--copper-dark:\s*(#[0-9a-f]{6});/i)?.[1]
  expect(copperDark).toBeDefined()
  expect(contrastRatio("#ffffff", copperDark!)).toBeGreaterThanOrEqual(4.5)
})

it("keeps the new-article primary link at least 44 pixels high", () => {
  expect(adminCSS).toMatch(
    /\.article-list-actions \.primary\s*\{[^}]*display:\s*inline-flex;[^}]*min-height:\s*44px;[^}]*align-items:\s*center;/s,
  )
})

it("keeps article-card title links at least 44 pixels high", () => {
  expect(adminCSS).toMatch(
    /\.article-admin-card h2 a\s*\{[^}]*display:\s*inline-flex;[^}]*min-height:\s*44px;[^}]*align-items:\s*center;/s,
  )
})

it("allows article fields and controls to shrink inside the responsive grid", () => {
  expect(adminCSS).toMatch(/\.article-fields label\s*\{[^}]*min-width:\s*0;/s)
  expect(adminCSS).toMatch(
    /\.article-fields input,\s*\.article-fields textarea,\s*\.article-fields select\s*\{[^}]*width:\s*100%;[^}]*min-width:\s*0;[^}]*max-width:\s*100%;/s,
  )
})

it("keeps the article summary and preview link at least 44 pixels high", () => {
  expect(adminCSS).toMatch(
    /\.article-fields textarea\s*\{[^}]*min-height:\s*44px;[^}]*resize:\s*vertical;/s,
  )
  expect(adminCSS).toMatch(
    /\.preview-sizes a\s*\{[^}]*display:\s*inline-flex;[^}]*min-height:\s*44px;[^}]*align-items:\s*center;/s,
  )
})

it("bundles the ProseMirror base and gap-cursor styles without inline CSS", () => {
  for (const fragment of [
    ".tiptap-surface .ProseMirror {\n  min-height: 18rem;\n  position: relative;",
    "word-wrap: break-word;\n  white-space: pre-wrap;\n  white-space: break-spaces;",
    'font-variant-ligatures: none;\n  font-feature-settings: "liga" 0;',
    '.tiptap-surface .ProseMirror [contenteditable="false"] {',
    ".tiptap-surface .ProseMirror pre {",
    ".tiptap-surface img.ProseMirror-separator {",
    ".tiptap-surface .ProseMirror-gapcursor {",
    ".tiptap-surface .ProseMirror-gapcursor::after {",
    ".tiptap-surface .ProseMirror-hideselection *::selection {",
    ".tiptap-surface .ProseMirror-hideselection * {",
    ".tiptap-surface .ProseMirror-focused .ProseMirror-gapcursor {",
  ]) {
    expect(adminCSS).toContain(fragment)
  }
  expect(adminCSS).toMatch(
    /\.tiptap-surface\s+\.ProseMirror\s+\[contenteditable="false"\]\s+\[contenteditable="true"\]\s*\{/s,
  )
})

function contrastRatio(first: string, second: string) {
  const luminances = [first, second]
    .map((color) =>
      color
        .slice(1)
        .match(/../g)!
        .map((channel) => Number.parseInt(channel, 16) / 255)
        .map((channel) =>
          channel <= 0.04045
            ? channel / 12.92
            : ((channel + 0.055) / 1.055) ** 2.4,
        ),
    )
    .map(([red, green, blue]) => 0.2126 * red + 0.7152 * green + 0.0722 * blue)
    .sort((left, right) => right - left)
  return (luminances[0] + 0.05) / (luminances[1] + 0.05)
}
