import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import App from "./App"

type Contact = {
  id: string
  name: string
  email: string
  phone?: string
  message: string
  consentVersion: string
  privacyAcceptedAt: string
  state: "new" | "read" | "archived"
  createdAt: string
  updatedAt: string
  readAt?: string
  archivedAt?: string
  reviewDueAt: string
  deletionDueAt?: string
}

const now = "2026-09-11T12:00:00Z"
const expectedCoverIDs = [
  "hero-architecture",
  "approach-library",
  "family-objects",
  "succession-seal",
  "contracts-pen",
  "debt-ledger",
  "damages-road",
  "property-key",
  "criminal-threshold",
  "tax-ledger",
  "article-notebook",
  "contact-entrance",
]

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.replaceState({}, "", "/admin")
})

describe("admin shell", () => {
  it("bootstraps the session in memory and shows the dashboard badge only after opening it", async () => {
    window.history.replaceState({}, "", "/admin/contatti")
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input)
        if (path === "/api/admin/session") {
          return jsonResponse({
            username: "admin",
            csrfToken: "csrf-memory-only",
          })
        }
        if (
          path === "/api/admin/contacts/purge-due" &&
          init?.method === "POST"
        ) {
          return jsonResponse({ purged: 1 })
        }
        if (path.startsWith("/api/admin/contacts")) {
          return jsonResponse({ items: [], nextCursor: "" })
        }
        if (path === "/api/admin/dashboard") {
          return jsonResponse({
            new: 2,
            read: 1,
            archived: 0,
            deletionScheduled: 0,
            retentionReview: 1,
            purged: 0,
          })
        }
        throw new Error(`unexpected fetch ${path} ${init?.method ?? "GET"}`)
      },
    )
    vi.stubGlobal("fetch", fetchMock)

    render(<App />)

    expect(screen.getByText("Caricamento sessione…")).toBeInTheDocument()
    expect(
      await screen.findByRole("heading", { name: "Contatti" }),
    ).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith(
      "/api/admin/dashboard",
      expect.anything(),
    )
    expect(window.localStorage.length).toBe(0)
    expect(window.sessionStorage.length).toBe(0)
    const sessionCall = fetchMock.mock.calls.find(
      ([path]) => path === "/api/admin/session",
    )
    expect(sessionCall?.[1]).toMatchObject({ credentials: "same-origin" })

    await userEvent.click(screen.getByRole("link", { name: "Panoramica" }))
    expect(await screen.findByText("2 nuovi contatti")).toBeInTheDocument()
    expect(
      screen.getByText("Eliminazioni scadute completate: 1."),
    ).toBeInTheDocument()
    expect(screen.getByLabelText("2 nuovi contatti")).toBeInTheDocument()
    expect(
      fetchMock.mock.calls.filter(([path]) => path === "/api/admin/dashboard"),
    ).toHaveLength(1)
    const purgeCallIndex = fetchMock.mock.calls.findIndex(
      ([path]) => path === "/api/admin/contacts/purge-due",
    )
    const dashboardCallIndex = fetchMock.mock.calls.findIndex(
      ([path]) => path === "/api/admin/dashboard",
    )
    expect(purgeCallIndex).toBeGreaterThan(-1)
    expect(purgeCallIndex).toBeLessThan(dashboardCallIndex)
    expect(fetchMock.mock.calls[purgeCallIndex]?.[1]).toMatchObject({
      credentials: "same-origin",
      headers: expect.objectContaining({ "X-CSRF-Token": "csrf-memory-only" }),
      method: "POST",
    })
    await userEvent.click(screen.getByRole("link", { name: "Apri la coda" }))
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(([path]) =>
          String(path).includes("/api/admin/contacts?state=new"),
        ),
      ).toBe(true),
    )
  })

  it("performs one login transition after unauthorized responses", async () => {
    const redirect = vi.fn()
    const fetchMock = vi.fn(async () =>
      jsonResponse({ error: "authentication required" }, 401),
    )
    vi.stubGlobal("fetch", fetchMock)

    const { rerender } = render(<App onUnauthorized={redirect} />)
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Sessione scaduta",
    )
    rerender(<App onUnauthorized={redirect} />)
    await waitFor(() => expect(redirect).toHaveBeenCalledTimes(1))
  })

  it("shows useful loading, empty, error, bounded debounced search, and filters", async () => {
    window.history.replaceState({}, "", "/admin/contatti")
    let resolveList: ((response: Response) => void) | undefined
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path === "/api/admin/session") {
        return Promise.resolve(
          jsonResponse({ username: "admin", csrfToken: "csrf" }),
        )
      }
      if (path.startsWith("/api/admin/contacts")) {
        if (path.includes("q=errore")) {
          return Promise.resolve(
            jsonResponse({ error: "contacts temporarily unavailable" }, 503),
          )
        }
        if (
          fetchMock.mock.calls.filter(([called]) =>
            String(called).startsWith("/api/admin/contacts"),
          ).length === 1
        ) {
          return new Promise<Response>((resolve) => {
            resolveList = resolve
          })
        }
        return Promise.resolve(jsonResponse({ items: [], nextCursor: "" }))
      }
      throw new Error(`unexpected fetch ${path}`)
    })
    vi.stubGlobal("fetch", fetchMock)
    render(<App />)

    expect(
      await screen.findByText("Ricerca dei contatti in corso…"),
    ).toBeInTheDocument()
    await waitFor(() => expect(resolveList).toBeTypeOf("function"))
    resolveList!(jsonResponse({ items: [], nextCursor: "" }))
    expect(
      await screen.findByText(
        "Nessun contatto corrisponde ai filtri selezionati.",
      ),
    ).toBeInTheDocument()

    const search = screen.getByRole("searchbox", {
      name: "Cerca per nome o email",
    })
    expect(search).toHaveAttribute("maxLength", "120")
    fireEvent.change(search, { target: { value: "Maria" } })
    expect(
      fetchMock.mock.calls.some(([path]) => String(path).includes("q=Maria")),
    ).toBe(false)
    await waitFor(
      () =>
        expect(
          fetchMock.mock.calls.some(([path]) =>
            String(path).includes("q=Maria"),
          ),
        ).toBe(true),
      { timeout: 1000 },
    )
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "Stato" }),
      "archived",
    )
    await userEvent.click(
      screen.getByRole("checkbox", { name: "In eliminazione" }),
    )
    await waitFor(() => {
      const paths = fetchMock.mock.calls.map(([path]) => String(path))
      expect(
        paths.some(
          (path) =>
            path.includes("state=archived") &&
            path.includes("deletionScheduled=true"),
        ),
      ).toBe(true)
    })

    fireEvent.change(search, { target: { value: "errore" } })
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Impossibile caricare i contatti",
    )
  })

  it.each([
    [
      "data",
      () =>
        jsonResponse({
          items: [contactFixture({ id: "old", name: "Old Result" })],
          nextCursor: "",
        }),
    ],
    ["error", () => jsonResponse({ error: "old request failed" }, 503)],
  ])(
    "ignores out-of-order stale contact-list %s responses",
    async (_kind, oldResponseFactory) => {
      window.history.replaceState({}, "", "/admin/contatti")
      let resolveOld: ((response: Response) => void) | undefined
      let resolveCurrent: ((response: Response) => void) | undefined
      const fetchMock = vi.fn((input: RequestInfo | URL) => {
        const path = String(input)
        if (path === "/api/admin/session") {
          return Promise.resolve(
            jsonResponse({ username: "admin", csrfToken: "csrf" }),
          )
        }
        if (path.includes("q=vecchia")) {
          return new Promise<Response>((resolve) => {
            resolveOld = resolve
          })
        }
        if (path.includes("q=attuale")) {
          return new Promise<Response>((resolve) => {
            resolveCurrent = resolve
          })
        }
        if (path.startsWith("/api/admin/contacts")) {
          return Promise.resolve(jsonResponse({ items: [], nextCursor: "" }))
        }
        throw new Error(`unexpected fetch ${path}`)
      })
      vi.stubGlobal("fetch", fetchMock)
      render(<App />)

      expect(
        await screen.findByText(
          "Nessun contatto corrisponde ai filtri selezionati.",
        ),
      ).toBeInTheDocument()
      const search = screen.getByRole("searchbox", {
        name: "Cerca per nome o email",
      })
      fireEvent.change(search, { target: { value: "vecchia" } })
      await waitFor(() => expect(resolveOld).toBeTypeOf("function"))
      fireEvent.change(search, { target: { value: "attuale" } })
      await waitFor(() => expect(resolveCurrent).toBeTypeOf("function"))

      resolveCurrent!(
        jsonResponse({
          items: [contactFixture({ id: "current", name: "Current Result" })],
          nextCursor: "",
        }),
      )
      expect(await screen.findByText("Current Result")).toBeInTheDocument()

      const oldResponse = oldResponseFactory()
      resolveOld!(oldResponse)
      await waitFor(() => {
        expect(oldResponse.bodyUsed).toBe(true)
        expect(screen.getByText("Current Result")).toBeInTheDocument()
        expect(screen.queryByText("Old Result")).not.toBeInTheDocument()
        expect(screen.queryByRole("alert")).not.toBeInTheDocument()
        expect(
          screen.queryByText("Ricerca dei contatti in corso…"),
        ).not.toBeInTheDocument()
      })
    },
  )

  it("automatically advances a sparse initial page without showing a definitive empty state", async () => {
    window.history.replaceState({}, "", "/admin/contatti")
    let resolveMatch: ((response: Response) => void) | undefined
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path === "/api/admin/session") {
        return Promise.resolve(
          jsonResponse({ username: "admin", csrfToken: "csrf" }),
        )
      }
      if (path === "/api/admin/contacts") {
        return Promise.resolve(
          jsonResponse({ items: [], nextCursor: "sparse-1" }),
        )
      }
      if (path === "/api/admin/contacts?cursor=sparse-1") {
        return new Promise<Response>((resolve) => {
          resolveMatch = resolve
        })
      }
      throw new Error(`unexpected fetch ${path}`)
    })
    vi.stubGlobal("fetch", fetchMock)
    render(<App />)

    await waitFor(() => expect(resolveMatch).toBeTypeOf("function"))
    expect(screen.getByRole("status")).toHaveTextContent(
      "Ricerca dei contatti in corso…",
    )
    expect(
      screen.queryByText("Nessun contatto corrisponde ai filtri selezionati."),
    ).not.toBeInTheDocument()

    resolveMatch!(
      jsonResponse({
        items: [contactFixture({ id: "match", name: "Sparse Match" })],
        nextCursor: "",
      }),
    )
    expect(await screen.findByText("Sparse Match")).toBeInTheDocument()
    expect(
      screen.queryByText("Nessun contatto corrisponde ai filtri selezionati."),
    ).not.toBeInTheDocument()
  })

  it("advances through multiple sparse pages before showing the exhausted empty state", async () => {
    window.history.replaceState({}, "", "/admin/contatti")
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === "/api/admin/session") {
        return jsonResponse({ username: "admin", csrfToken: "csrf" })
      }
      if (path === "/api/admin/contacts") {
        return jsonResponse({ items: [], nextCursor: "empty-1" })
      }
      if (path === "/api/admin/contacts?cursor=empty-1") {
        return jsonResponse({ items: [], nextCursor: "empty-2" })
      }
      if (path === "/api/admin/contacts?cursor=empty-2") {
        return jsonResponse({ items: [], nextCursor: "" })
      }
      throw new Error(`unexpected fetch ${path}`)
    })
    vi.stubGlobal("fetch", fetchMock)
    render(<App />)

    expect(
      await screen.findByText(
        "Nessun contatto corrisponde ai filtri selezionati.",
      ),
    ).toBeInTheDocument()
    expect(
      fetchMock.mock.calls.filter(([path]) =>
        String(path).startsWith("/api/admin/contacts"),
      ),
    ).toHaveLength(3)
    expect(screen.queryByRole("status")).not.toBeInTheDocument()
  })

  it("stops a non-progressing sparse cursor with an honest error", async () => {
    window.history.replaceState({}, "", "/admin/contatti")
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === "/api/admin/session") {
        return jsonResponse({ username: "admin", csrfToken: "csrf" })
      }
      if (path === "/api/admin/contacts") {
        return jsonResponse({ items: [], nextCursor: "repeat" })
      }
      if (path === "/api/admin/contacts?cursor=repeat") {
        return jsonResponse({ items: [], nextCursor: "repeat" })
      }
      throw new Error(`unexpected fetch ${path}`)
    })
    vi.stubGlobal("fetch", fetchMock)
    render(<App />)

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Impossibile continuare: paginazione non valida",
    )
    expect(
      fetchMock.mock.calls.filter(([path]) =>
        String(path).startsWith("/api/admin/contacts"),
      ),
    ).toHaveLength(2)
    expect(
      screen.queryByText("Nessun contatto corrisponde ai filtri selezionati."),
    ).not.toBeInTheDocument()
  })

  it("aborts and ignores an old sparse chain after the query changes", async () => {
    window.history.replaceState({}, "", "/admin/contatti")
    let resolveOld: ((response: Response) => void) | undefined
    let oldSignal: AbortSignal | undefined
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path === "/api/admin/session") {
        return Promise.resolve(
          jsonResponse({ username: "admin", csrfToken: "csrf" }),
        )
      }
      if (path === "/api/admin/contacts") {
        return Promise.resolve(jsonResponse({ items: [], nextCursor: "" }))
      }
      if (path === "/api/admin/contacts?q=vecchia") {
        return Promise.resolve(jsonResponse({ items: [], nextCursor: "old-1" }))
      }
      if (path === "/api/admin/contacts?q=vecchia&cursor=old-1") {
        oldSignal = init?.signal ?? undefined
        return new Promise<Response>((resolve) => {
          resolveOld = resolve
        })
      }
      if (path === "/api/admin/contacts?q=attuale") {
        return Promise.resolve(
          jsonResponse({
            items: [contactFixture({ id: "current", name: "Current Sparse" })],
            nextCursor: "",
          }),
        )
      }
      throw new Error(`unexpected fetch ${path}`)
    })
    vi.stubGlobal("fetch", fetchMock)
    render(<App />)

    expect(
      await screen.findByText(
        "Nessun contatto corrisponde ai filtri selezionati.",
      ),
    ).toBeInTheDocument()
    const search = screen.getByRole("searchbox", {
      name: "Cerca per nome o email",
    })
    fireEvent.change(search, { target: { value: "vecchia" } })
    await waitFor(() => expect(resolveOld).toBeTypeOf("function"))
    fireEvent.change(search, { target: { value: "attuale" } })

    expect(await screen.findByText("Current Sparse")).toBeInTheDocument()
    expect(oldSignal?.aborted).toBe(true)
    resolveOld!(
      jsonResponse({
        items: [contactFixture({ id: "old", name: "Old Sparse" })],
        nextCursor: "",
      }),
    )
    await waitFor(() => {
      expect(screen.getByText("Current Sparse")).toBeInTheDocument()
      expect(screen.queryByText("Old Sparse")).not.toBeInTheDocument()
      expect(screen.queryByRole("alert")).not.toBeInTheDocument()
    })
  })

  it("skips sparse load-more pages and appends only unique contacts", async () => {
    window.history.replaceState({}, "", "/admin/contatti")
    const existing = contactFixture({ id: "existing", name: "Existing" })
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === "/api/admin/session") {
        return jsonResponse({ username: "admin", csrfToken: "csrf" })
      }
      if (path === "/api/admin/contacts") {
        return jsonResponse({ items: [existing], nextCursor: "more-1" })
      }
      if (path === "/api/admin/contacts?cursor=more-1") {
        return jsonResponse({ items: [], nextCursor: "more-2" })
      }
      if (path === "/api/admin/contacts?cursor=more-2") {
        return jsonResponse({
          items: [
            existing,
            contactFixture({ id: "additional", name: "Additional" }),
          ],
          nextCursor: "",
        })
      }
      throw new Error(`unexpected fetch ${path}`)
    })
    vi.stubGlobal("fetch", fetchMock)
    render(<App />)

    expect(await screen.findByText("Existing")).toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "Carica altri" }))
    expect(await screen.findByText("Additional")).toBeInTheDocument()
    expect(screen.getAllByText("Existing")).toHaveLength(1)
    expect(
      screen.queryByRole("button", { name: "Carica altri" }),
    ).not.toBeInTheDocument()
    expect(
      fetchMock.mock.calls.filter(([path]) =>
        String(path).startsWith("/api/admin/contacts"),
      ),
    ).toHaveLength(3)
  })

  it("clears old results and their cursor when a replacement query fails", async () => {
    window.history.replaceState({}, "", "/admin/contatti")
    const old = contactFixture({ id: "old", name: "Old Filter Result" })
    let resolveReplacement: ((response: Response) => void) | undefined
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path === "/api/admin/session") {
        return Promise.resolve(
          jsonResponse({ username: "admin", csrfToken: "csrf" }),
        )
      }
      if (path === "/api/admin/contacts") {
        return Promise.resolve(
          jsonResponse({ items: [old], nextCursor: "old-cursor" }),
        )
      }
      if (path === "/api/admin/contacts?q=nuova") {
        return new Promise<Response>((resolve) => {
          resolveReplacement = resolve
        })
      }
      throw new Error(`unexpected fetch ${path}`)
    })
    vi.stubGlobal("fetch", fetchMock)
    render(<App />)

    expect(await screen.findByText("Old Filter Result")).toBeInTheDocument()
    expect(
      screen.getByRole("button", { name: "Carica altri" }),
    ).toBeInTheDocument()
    fireEvent.change(
      screen.getByRole("searchbox", { name: "Cerca per nome o email" }),
      { target: { value: "nuova" } },
    )
    await waitFor(() => expect(resolveReplacement).toBeTypeOf("function"))

    expect(screen.queryByText("Old Filter Result")).not.toBeInTheDocument()
    expect(
      screen.queryByRole("button", { name: "Carica altri" }),
    ).not.toBeInTheDocument()
    expect(screen.getByRole("status")).toHaveTextContent(
      "Ricerca dei contatti in corso…",
    )

    resolveReplacement!(
      jsonResponse({ error: "contacts temporarily unavailable" }, 503),
    )
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Impossibile caricare i contatti",
    )
    expect(screen.queryByText("Old Filter Result")).not.toBeInTheDocument()
    expect(
      screen.queryByRole("button", { name: "Carica altri" }),
    ).not.toBeInTheDocument()
    expect(
      fetchMock.mock.calls.some(([path]) =>
        String(path).includes("cursor=old-cursor"),
      ),
    ).toBe(false)
  })

  it("preserves loaded contacts and a safe retry cursor after load-more failure", async () => {
    window.history.replaceState({}, "", "/admin/contatti")
    const existing = contactFixture({ id: "existing", name: "Retry Existing" })
    let loadMoreAttempts = 0
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === "/api/admin/session") {
        return jsonResponse({ username: "admin", csrfToken: "csrf" })
      }
      if (path === "/api/admin/contacts") {
        return jsonResponse({ items: [existing], nextCursor: "retry-cursor" })
      }
      if (path === "/api/admin/contacts?cursor=retry-cursor") {
        loadMoreAttempts++
        if (loadMoreAttempts === 1) {
          return jsonResponse({ error: "temporary storage failure" }, 503)
        }
        return jsonResponse({
          items: [contactFixture({ id: "retried", name: "Retry Result" })],
          nextCursor: "",
        })
      }
      throw new Error(`unexpected fetch ${path}`)
    })
    vi.stubGlobal("fetch", fetchMock)
    render(<App />)

    expect(await screen.findByText("Retry Existing")).toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "Carica altri" }))
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Impossibile caricare i contatti",
    )
    expect(screen.getByText("Retry Existing")).toBeInTheDocument()

    await userEvent.click(screen.getByRole("button", { name: "Carica altri" }))
    expect(await screen.findByText("Retry Result")).toBeInTheDocument()
    expect(screen.getByText("Retry Existing")).toBeInTheDocument()
    expect(screen.queryByRole("alert")).not.toBeInTheDocument()
    expect(loadMoreAttempts).toBe(2)
  })

  it("makes a cyclic load-more cursor terminal while preserving loaded contacts", async () => {
    window.history.replaceState({}, "", "/admin/contatti")
    const initial = contactFixture({ id: "initial", name: "Cycle Existing" })
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === "/api/admin/session") {
        return jsonResponse({ username: "admin", csrfToken: "csrf" })
      }
      if (path === "/api/admin/contacts") {
        return jsonResponse({ items: [initial], nextCursor: "c1" })
      }
      if (path === "/api/admin/contacts?cursor=c1") {
        return jsonResponse({ items: [], nextCursor: "c2" })
      }
      if (path === "/api/admin/contacts?cursor=c2") {
        return jsonResponse({ items: [], nextCursor: "c1" })
      }
      throw new Error(`unexpected fetch ${path}`)
    })
    vi.stubGlobal("fetch", fetchMock)
    const { rerender } = render(<App />)

    expect(await screen.findByText("Cycle Existing")).toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "Carica altri" }))
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Impossibile continuare: paginazione non valida",
    )
    expect(screen.getByText("Cycle Existing")).toBeInTheDocument()

    const terminalButton = screen.queryByRole("button", {
      name: "Carica altri",
    })
    if (terminalButton) fireEvent.click(terminalButton)
    rerender(<App />)

    expect(terminalButton).not.toBeInTheDocument()
    expect(
      fetchMock.mock.calls.filter(([path]) =>
        String(path).startsWith("/api/admin/contacts"),
      ),
    ).toHaveLength(3)
  })

  it("opens a new contact with an explicit read mutation and uses CSRF and If-Match", async () => {
    window.history.replaceState({}, "", "/admin/contatti/contact-1")
    const created = contactFixture({ state: "new" })
    const read = contactFixture({ state: "read", readAt: now })
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input)
        if (path === "/api/admin/session")
          return jsonResponse({ username: "admin", csrfToken: "csrf-token" })
        if (path === "/api/admin/contacts/contact-1" && !init?.method)
          return jsonResponse(created, 200, { ETag: '"etag-1"' })
        if (path.endsWith("/read") && init?.method === "POST")
          return jsonResponse(read, 200, { ETag: '"etag-2"' })
        throw new Error(`unexpected fetch ${path}`)
      },
    )
    vi.stubGlobal("fetch", fetchMock)

    render(<App />)

    expect(
      await screen.findByRole("heading", { name: "Mario Rossi" }),
    ).toBeInTheDocument()
    expect(screen.getByText("Letto")).toBeInTheDocument()
    const readCall = fetchMock.mock.calls.find(([path]) =>
      String(path).endsWith("/read"),
    )
    expect(readCall?.[1]).toMatchObject({
      method: "POST",
      credentials: "same-origin",
      headers: expect.objectContaining({
        "X-CSRF-Token": "csrf-token",
        "If-Match": '"etag-1"',
      }),
    })
  })

  it("archives, restores, confirms deletion, and cancels scheduled deletion", async () => {
    window.history.replaceState({}, "", "/admin/contatti/contact-1")
    let current = contactFixture({ state: "read", readAt: now })
    let etag = 1
    const actions: string[] = []
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input)
        if (path === "/api/admin/session")
          return jsonResponse({ username: "admin", csrfToken: "csrf" })
        if (path === "/api/admin/contacts/contact-1" && !init?.method)
          return jsonResponse(current, 200, { ETag: `"etag-${etag}"` })
        const action = path.split("/").at(-1) ?? ""
        actions.push(action)
        if (action === "archive")
          current = { ...current, state: "archived", archivedAt: now }
        if (action === "restore")
          current = { ...current, state: "read", archivedAt: undefined }
        if (action === "schedule-deletion")
          current = { ...current, deletionDueAt: "2026-10-11T12:00:00Z" }
        if (action === "cancel-deletion")
          current = { ...current, deletionDueAt: undefined }
        etag += 1
        return jsonResponse(current, 200, { ETag: `"etag-${etag}"` })
      },
    )
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    render(<App />)

    await user.click(await screen.findByRole("button", { name: "Archivia" }))
    expect(await screen.findByText("Archiviato")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Ripristina" }))
    expect(await screen.findByText("Letto")).toBeInTheDocument()

    const schedule = screen.getByRole("button", {
      name: "Programma eliminazione",
    })
    await user.click(schedule)
    let dialog = screen.getByRole("dialog", {
      name: "Conferma eliminazione programmata",
    })
    await user.click(within(dialog).getByRole("button", { name: "Annulla" }))
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
    expect(schedule).toHaveFocus()

    await user.click(schedule)
    dialog = screen.getByRole("dialog", {
      name: "Conferma eliminazione programmata",
    })
    await user.click(
      within(dialog).getByRole("button", {
        name: "Conferma, elimina tra 30 giorni",
      }),
    )
    expect(
      await screen.findByText(/Eliminazione programmata/),
    ).toBeInTheDocument()
    await user.click(
      screen.getByRole("button", { name: "Annulla eliminazione programmata" }),
    )
    await waitFor(() =>
      expect(
        screen.queryByText(/Recuperabile fino al/),
      ).not.toBeInTheDocument(),
    )
    expect(actions).toEqual([
      "archive",
      "restore",
      "schedule-deletion",
      "cancel-deletion",
    ])
  })

  it("offers a safe reload after a stale mutation conflict", async () => {
    window.history.replaceState({}, "", "/admin/contatti/contact-1")
    const contact = contactFixture({ state: "read", readAt: now })
    let gets = 0
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input)
        if (path === "/api/admin/session")
          return jsonResponse({ username: "admin", csrfToken: "csrf" })
        if (path === "/api/admin/contacts/contact-1" && !init?.method) {
          gets += 1
          return jsonResponse(contact, 200, { ETag: `"etag-${gets}"` })
        }
        return jsonResponse({ error: "contact changed; reload and retry" }, 409)
      },
    )
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    render(<App />)

    await user.click(await screen.findByRole("button", { name: "Archivia" }))
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Il contatto è stato modificato",
    )
    await user.click(screen.getByRole("button", { name: "Ricarica contatto" }))
    await waitFor(() => expect(gets).toBe(2))
  })

  it("closes the mobile sidebar and deletion dialog with Escape and restores focus", async () => {
    window.history.replaceState({}, "", "/admin/contatti/contact-1")
    const contact = contactFixture({ state: "read", readAt: now })
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) =>
        String(input) === "/api/admin/session"
          ? jsonResponse({ username: "admin", csrfToken: "csrf" })
          : jsonResponse(contact, 200, { ETag: '"etag-1"' }),
      ),
    )
    const user = userEvent.setup()
    render(<App />)

    const menu = await screen.findByRole("button", { name: "Apri navigazione" })
    await user.click(menu)
    expect(
      screen.getByRole("navigation", { name: "Navigazione mobile" }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole("button", { name: "Chiudi navigazione" }),
    ).toHaveFocus()
    await user.tab({ shift: true })
    expect(
      within(
        screen.getByRole("navigation", { name: "Navigazione mobile" }),
      ).getByRole("link", { name: "Contatti" }),
    ).toHaveFocus()
    await user.keyboard("{Escape}")
    expect(
      screen.queryByRole("navigation", { name: "Navigazione mobile" }),
    ).not.toBeInTheDocument()
    expect(menu).toHaveFocus()

    const schedule = screen.getByRole("button", {
      name: "Programma eliminazione",
    })
    await user.click(schedule)
    const dialog = screen.getByRole("dialog", {
      name: "Conferma eliminazione programmata",
    })
    expect(
      within(dialog).getByRole("button", { name: "Annulla" }),
    ).toHaveFocus()
    await user.tab({ shift: true })
    expect(
      within(dialog).getByRole("button", {
        name: "Conferma, elimina tra 30 giorni",
      }),
    ).toHaveFocus()
    await user.keyboard("{Escape}")
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
    expect(schedule).toHaveFocus()
  })
})

describe("article console", () => {
  it("blocks dirty link navigation with an accessible choice and beforeunload", async () => {
    window.history.replaceState({}, "", "/admin/articoli/nuovo")
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === "/api/admin/session")
        return jsonResponse({ username: "admin", csrfToken: "csrf" })
      if (path === "/api/admin/covers") return jsonResponse([])
      if (path === "/api/admin/contacts")
        return jsonResponse({ items: [], nextCursor: "" })
      throw new Error(`unexpected ${path}`)
    })
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    render(<App />)

    await screen.findByRole("heading", { name: "Nuovo articolo" })
    fireEvent.change(screen.getByLabelText("Titolo"), {
      target: { value: "Modifica locale" },
    })
    const unload = new Event("beforeunload", { cancelable: true })
    window.dispatchEvent(unload)
    expect(unload.defaultPrevented).toBe(true)

    const contacts = screen.getByRole("link", { name: "Contatti" })
    await user.click(contacts)
    let dialog = screen.getByRole("dialog", {
      name: "Modifiche non salvate",
    })
    expect(within(dialog).getByRole("button", { name: "Resta" })).toHaveFocus()
    await user.tab({ shift: true })
    expect(within(dialog).getByRole("button", { name: "Esci" })).toHaveFocus()
    await user.keyboard("{Escape}")
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
    expect(contacts).toHaveFocus()
    expect(
      screen.getByRole("heading", { name: "Nuovo articolo" }),
    ).toBeInTheDocument()

    await user.click(contacts)
    dialog = screen.getByRole("dialog", { name: "Modifiche non salvate" })
    await user.click(within(dialog).getByRole("button", { name: "Resta" }))
    expect(contacts).toHaveFocus()
    await user.click(contacts)
    dialog = screen.getByRole("dialog", { name: "Modifiche non salvate" })
    await user.click(within(dialog).getByRole("button", { name: "Esci" }))
    expect(
      await screen.findByRole("heading", { name: "Contatti" }),
    ).toBeInTheDocument()
  })

  it("blocks dirty browser-history navigation", async () => {
    window.history.replaceState({}, "", "/admin/contatti")
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input)
        if (path === "/api/admin/session")
          return jsonResponse({ username: "admin", csrfToken: "csrf" })
        if (path === "/api/admin/contacts" || path === "/api/admin/articles")
          return jsonResponse({ items: [], nextCursor: "" })
        if (path === "/api/admin/covers") return jsonResponse([])
        throw new Error(`unexpected ${path}`)
      }),
    )
    const user = userEvent.setup()
    render(<App />)
    await user.click(await screen.findByRole("link", { name: "Articoli" }))
    await user.click(screen.getByRole("link", { name: "Nuovo articolo" }))
    await screen.findByRole("heading", { name: "Nuovo articolo" })
    fireEvent.change(screen.getByLabelText("Titolo"), {
      target: { value: "Modifica locale" },
    })

    window.history.back()
    const dialog = await screen.findByRole("dialog", {
      name: "Modifiche non salvate",
    })
    expect(
      screen.getByRole("heading", { name: "Nuovo articolo" }),
    ).toBeInTheDocument()
    await user.click(within(dialog).getByRole("button", { name: "Esci" }))
    expect(
      await screen.findByRole("heading", { name: "Articoli" }),
    ).toBeInTheDocument()
  })

  it("blocks dirty forward-history navigation", async () => {
    window.history.replaceState({}, "", "/admin/articoli/nuovo")
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input)
        if (path === "/api/admin/session")
          return jsonResponse({ username: "admin", csrfToken: "csrf" })
        if (path === "/api/admin/covers") return jsonResponse([])
        if (path === "/api/admin/contacts")
          return jsonResponse({ items: [], nextCursor: "" })
        throw new Error(`unexpected ${path}`)
      }),
    )
    const user = userEvent.setup()
    render(<App />)
    await screen.findByRole("heading", { name: "Nuovo articolo" })
    await user.click(screen.getByRole("link", { name: "Contatti" }))
    await screen.findByRole("heading", { name: "Contatti" })
    window.history.back()
    await screen.findByRole("heading", { name: "Nuovo articolo" })
    fireEvent.change(screen.getByLabelText("Titolo"), {
      target: { value: "Modifica locale" },
    })

    window.history.forward()
    const dialog = await screen.findByRole("dialog", {
      name: "Modifiche non salvate",
    })
    await user.click(within(dialog).getByRole("button", { name: "Resta" }))
    expect(
      screen.getByRole("heading", { name: "Nuovo articolo" }),
    ).toBeInTheDocument()
  })

  it("keeps a new draft local until explicit create and exposes only the restricted editor", async () => {
    window.history.replaceState({}, "", "/admin/articoli/nuovo")
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input)
        if (path === "/api/admin/session")
          return jsonResponse({ username: "admin", csrfToken: "csrf" })
        if (path === "/api/admin/covers")
          return jsonResponse(
            expectedCoverIDs.map((id) => ({
              id,
              alt: id,
              cardAvif: `/${id}-card.avif`,
              cardWebp: `/${id}-card.webp`,
              landscapeAvif: `/${id}-wide.avif`,
              landscapeWebp: `/${id}-wide.webp`,
            })),
          )
        if (path === "/api/admin/articles" && init?.method === "POST")
          return jsonResponse(
            {
              id: "article-1",
              slug: "prova",
              title: "Titolo valido",
              summary: "Sommario sufficientemente lungo",
              area: "diritti-reali",
              coverId: "",
              status: "draft",
              createdAt: now,
              updatedAt: now,
              body: {
                schemaVersion: 1,
                document: { type: "doc", content: [{ type: "paragraph" }] },
              },
            },
            201,
            { ETag: '"MQ"' },
          )
        if (path === "/api/admin/articles/article-1")
          return jsonResponse(
            {
              id: "article-1",
              slug: "prova",
              title: "Titolo valido",
              summary: "Sommario sufficientemente lungo",
              area: "diritti-reali",
              coverId: "",
              status: "draft",
              createdAt: now,
              updatedAt: now,
              body: {
                schemaVersion: 1,
                document: { type: "doc", content: [{ type: "paragraph" }] },
              },
            },
            200,
            { ETag: '"MQ"' },
          )
        throw new Error(`unexpected fetch ${path}`)
      },
    )
    vi.stubGlobal("fetch", fetchMock)
    render(<App />)
    expect(
      await screen.findByRole("heading", { name: "Nuovo articolo" }),
    ).toBeInTheDocument()
    for (const control of [
      "Grassetto",
      "Corsivo",
      "Titolo 2",
      "Titolo 3",
      "Elenco",
      "Numerato",
      "Citazione",
      "Collegamento",
    ])
      expect(screen.getByRole("button", { name: control })).toBeInTheDocument()
    expect(screen.queryByText(/HTML/i)).not.toBeInTheDocument()
    expect(document.querySelector('input[type="file"]')).toBeNull()
    expect(
      within(screen.getByLabelText("Copertina"))
        .getAllByRole("option")
        .map((option) => (option as HTMLOptionElement).value),
    ).toEqual(["", ...expectedCoverIDs])
    expect(
      fetchMock.mock.calls.some(
        ([path, init]) =>
          path === "/api/admin/articles" && init?.method === "POST",
      ),
    ).toBe(false)
    fireEvent.change(screen.getByLabelText("Titolo"), {
      target: { value: "Titolo valido" },
    })
    const editor = document.querySelector(".ProseMirror")
    if (!(editor instanceof HTMLElement)) throw new Error("missing editor")
    fireEvent.paste(editor, {
      clipboardData: {
        types: ["text/html", "text/plain"],
        getData: (type: string) =>
          type === "text/html"
            ? '<p>Testo ammesso</p><img src="https://evil.test/a.png"><pre><code>codice vietato</code></pre>'
            : "Testo ammesso codice vietato",
      },
    })
    expect(editor).toHaveTextContent("Testo ammesso")
    expect(screen.getByText("Modifiche non salvate")).toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "Crea bozza" }))
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          ([path, init]) =>
            path === "/api/admin/articles" && init?.method === "POST",
        ),
      ).toBe(true),
    )
    const create = fetchMock.mock.calls.find(
      ([path, init]) =>
        path === "/api/admin/articles" && init?.method === "POST",
    )
    expect(String(create?.[1]?.body)).not.toContain('"html"')
    const submitted = JSON.parse(String(create?.[1]?.body))
    const submittedTypes = JSON.stringify(submitted.body.document)
    expect(submittedTypes).toContain("Testo ammesso")
    expect(submittedTypes).not.toContain('"type":"image"')
    expect(submittedTypes).not.toContain('"type":"code"')
    expect(submittedTypes).not.toContain('"type":"codeBlock"')
    expect(
      await screen.findByRole("heading", { name: "Modifica articolo" }),
    ).toBeInTheDocument()
    expect(window.location.pathname).toBe("/admin/articoli/article-1")
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
    expect(window.localStorage.length).toBe(0)
    expect(window.sessionStorage.length).toBe(0)
  })

  it("shows list loading, error, retry, and honest empty states", async () => {
    window.history.replaceState({}, "", "/admin/articoli")
    let attempts = 0
    const pending = deferred<Response>()
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === "/api/admin/session")
        return jsonResponse({ username: "admin", csrfToken: "csrf" })
      if (path === "/api/admin/covers") return jsonResponse([])
      if (path === "/api/admin/articles") {
        attempts++
        if (attempts === 1) return pending.promise
        return jsonResponse({ items: [], nextCursor: "" })
      }
      throw new Error(`unexpected ${path}`)
    })
    vi.stubGlobal("fetch", fetchMock)
    render(<App />)
    expect(await screen.findByText("Caricamento articoli…")).toBeInTheDocument()
    pending.reject(new Error("offline"))
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Impossibile caricare gli articoli",
    )
    await userEvent.click(screen.getByRole("button", { name: "Riprova" }))
    expect(await screen.findByText("Nessun articolo")).toBeInTheDocument()
  })

  it("auto-chains sparse pages and deduplicates explicit load-more results", async () => {
    window.history.replaceState({}, "", "/admin/articoli")
    const calls: string[] = []
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === "/api/admin/session")
        return jsonResponse({ username: "admin", csrfToken: "csrf" })
      calls.push(path)
      if (path === "/api/admin/articles")
        return jsonResponse({ items: [], nextCursor: "sparse" })
      if (path === "/api/admin/articles?cursor=sparse")
        return jsonResponse({
          items: [articleSummary("a", "Articolo A")],
          nextCursor: "more",
        })
      if (path === "/api/admin/articles?cursor=more")
        return jsonResponse({
          items: [
            articleSummary("a", "Articolo A"),
            articleSummary("b", "Articolo B"),
          ],
          nextCursor: "",
        })
      throw new Error(`unexpected ${path}`)
    })
    vi.stubGlobal("fetch", fetchMock)
    render(<App />)
    expect(
      await screen.findByRole("heading", { name: "Articolo A" }),
    ).toBeInTheDocument()
    expect(screen.queryByText("Nessun articolo")).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "Carica altri" }))
    expect(
      await screen.findByRole("heading", { name: "Articolo B" }),
    ).toBeInTheDocument()
    expect(screen.getAllByRole("heading", { name: "Articolo A" })).toHaveLength(
      1,
    )
    expect(calls).toEqual([
      "/api/admin/articles",
      "/api/admin/articles?cursor=sparse",
      "/api/admin/articles?cursor=more",
    ])
  })

  it("aborts stale article-list requests when the status filter changes", async () => {
    window.history.replaceState({}, "", "/admin/articoli")
    const all = deferred<Response>()
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input)
        if (path === "/api/admin/session")
          return jsonResponse({ username: "admin", csrfToken: "csrf" })
        if (path === "/api/admin/articles") return all.promise
        if (path === "/api/admin/articles?status=draft")
          return jsonResponse({
            items: [articleSummary("draft", "Solo bozza")],
            nextCursor: "",
          })
        throw new Error(`unexpected ${path}`)
      },
    )
    vi.stubGlobal("fetch", fetchMock)
    render(<App />)
    await screen.findByText("Caricamento articoli…")
    await userEvent.selectOptions(screen.getByLabelText("Stato"), "draft")
    expect(
      await screen.findByRole("heading", { name: "Solo bozza" }),
    ).toBeInTheDocument()
    all.resolve(
      jsonResponse({
        items: [articleSummary("stale", "Risposta vecchia")],
        nextCursor: "",
      }),
    )
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(screen.queryByText("Risposta vecchia")).not.toBeInTheDocument()
  })

  it("stops sparse-page cursor cycles with a retryable error", async () => {
    window.history.replaceState({}, "", "/admin/articoli")
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === "/api/admin/session")
        return jsonResponse({ username: "admin", csrfToken: "csrf" })
      if (path === "/api/admin/articles")
        return jsonResponse({ items: [], nextCursor: "loop" })
      if (path === "/api/admin/articles?cursor=loop")
        return jsonResponse({
          items: [articleSummary("repeated", "Risultato ciclico")],
          nextCursor: "loop",
        })
      throw new Error(`unexpected ${path}`)
    })
    vi.stubGlobal("fetch", fetchMock)
    render(<App />)

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Impossibile caricare gli articoli",
    )
    expect(screen.getByRole("button", { name: "Riprova" })).toBeInTheDocument()
    expect(
      fetchMock.mock.calls.filter(([path]) =>
        String(path).startsWith("/api/admin/articles"),
      ),
    ).toHaveLength(2)
  })

  it("serializes duplicate and cross-action mutations before the first await", async () => {
    window.history.replaceState({}, "", "/admin/articoli/article-1")
    const saveRequest = deferred<Response>()
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input)
        if (path === "/api/admin/session")
          return jsonResponse({ username: "admin", csrfToken: "csrf" })
        if (path === "/api/admin/covers") return jsonResponse([])
        if (path === "/api/admin/articles/article-1" && !init?.method)
          return jsonResponse(articleDetail(), 200, { ETag: '"etag-1"' })
        if (path.endsWith("/draft") && init?.method === "PUT")
          return saveRequest.promise
        if (path.endsWith("/publish") && init?.method === "POST")
          return jsonResponse(articleDetail({ status: "published" }), 200, {
            ETag: '"etag-3"',
          })
        throw new Error(`unexpected ${path}`)
      },
    )
    vi.stubGlobal("fetch", fetchMock)
    render(<App />)

    await screen.findByDisplayValue("Titolo articolo")
    const save = screen.getByRole("button", { name: "Salva bozza" })
    const publish = screen.getByRole("button", { name: "Pubblica" })
    fireEvent.click(save)
    fireEvent.click(save)
    fireEvent.click(publish)

    expect(await screen.findByRole("status")).toHaveTextContent(
      "Salvataggio in corso",
    )
    expect(save).toBeDisabled()
    expect(publish).toBeDisabled()
    expect(screen.getByLabelText("Titolo")).toBeDisabled()
    expect(screen.getByLabelText("Copertina")).toBeDisabled()
    expect(screen.getByRole("button", { name: "Grassetto" })).toBeDisabled()
    expect(document.querySelector(".ProseMirror")).toHaveAttribute(
      "contenteditable",
      "false",
    )
    expect(screen.getByRole("button", { name: "360" })).toBeEnabled()
    expect(
      fetchMock.mock.calls.filter(
        ([path, init]) =>
          String(path).endsWith("/draft") && init?.method === "PUT",
      ),
    ).toHaveLength(1)
    expect(
      fetchMock.mock.calls.filter(
        ([path, init]) =>
          String(path).endsWith("/publish") && init?.method === "POST",
      ),
    ).toHaveLength(0)

    saveRequest.resolve(
      jsonResponse(articleDetail(), 200, { ETag: '"etag-2"' }),
    )
    expect(await screen.findByRole("status")).toHaveTextContent("Bozza salvata")
    expect(save).toBeEnabled()
    expect(publish).toBeEnabled()
    expect(screen.getByLabelText("Titolo")).toBeEnabled()
    await waitFor(() =>
      expect(document.querySelector(".ProseMirror")).toHaveAttribute(
        "contenteditable",
        "true",
      ),
    )
  })

  for (const failure of [
    {
      name: "409 conflicts",
      status: 409,
      code: "article_conflict",
      message: "Conflitto con una versione più recente",
      locks: true,
    },
    {
      name: "unknown commits",
      status: 503,
      code: "commit_unknown",
      message: "Esito del salvataggio incerto",
      locks: true,
    },
    {
      name: "ordinary transient failures",
      status: 503,
      code: "articles_unavailable",
      message: "temporaneamente non disponibile",
      locks: false,
    },
  ]) {
    it(`preserves the local draft after ${failure.name}`, async () => {
      window.history.replaceState({}, "", "/admin/articoli/article-1")
      let saveAttempts = 0
      const fetchMock = vi.fn(
        async (input: RequestInfo | URL, init?: RequestInit) => {
          const path = String(input)
          if (path === "/api/admin/session")
            return jsonResponse({ username: "admin", csrfToken: "csrf" })
          if (path === "/api/admin/covers") return jsonResponse([])
          if (path === "/api/admin/articles/article-1" && !init?.method)
            return jsonResponse(
              articleDetail({
                body: {
                  schemaVersion: 1,
                  document: {
                    type: "doc",
                    content: [
                      {
                        type: "paragraph",
                        content: [{ type: "text", text: "Corpo server" }],
                      },
                    ],
                  },
                },
              }),
              200,
              { ETag: '"etag-1"' },
            )
          if (path.endsWith("/draft") && init?.method === "PUT") {
            saveAttempts++
            if (saveAttempts === 1)
              return jsonResponse(
                {
                  error: "temporaneamente non disponibile",
                  code: failure.code,
                },
                failure.status,
              )
            return jsonResponse(articleDetail(), 200, { ETag: '"etag-2"' })
          }
          throw new Error(`unexpected ${path}`)
        },
      )
      vi.stubGlobal("fetch", fetchMock)
      const user = userEvent.setup()
      render(<App />)

      await screen.findByDisplayValue("Titolo articolo")
      await user.clear(screen.getByLabelText("Titolo"))
      await user.type(screen.getByLabelText("Titolo"), "Titolo locale")
      const bodyEditor = document.querySelector(".ProseMirror")
      if (!(bodyEditor instanceof HTMLElement))
        throw new Error("missing article body editor")
      fireEvent.paste(bodyEditor, {
        clipboardData: {
          types: ["text/plain"],
          getData: (type: string) =>
            type === "text/plain" ? "Corpo modificato localmente" : "",
        },
      })
      expect(bodyEditor).toHaveTextContent("Corpo modificato localmente")
      await user.click(screen.getByRole("button", { name: "Salva bozza" }))
      expect(await screen.findByRole("status")).toHaveTextContent(
        failure.message,
      )
      expect(screen.getByDisplayValue("Titolo locale")).toBeInTheDocument()
      expect(bodyEditor).toHaveTextContent("Corpo modificato localmente")
      expect(screen.getByText("Modifiche non salvate")).toBeInTheDocument()

      const save = screen.getByRole("button", { name: "Salva bozza" })
      if (failure.locks) {
        expect(save).toBeDisabled()
        expect(
          screen.getByRole("button", { name: "Ricarica per riconciliare" }),
        ).toBeInTheDocument()
      } else {
        await waitFor(() => expect(save).toBeEnabled())
        expect(
          screen.queryByRole("button", { name: "Ricarica per riconciliare" }),
        ).not.toBeInTheDocument()
        await user.click(save)
        expect(await screen.findByRole("status")).toHaveTextContent(
          "Bozza salvata",
        )
        expect(saveAttempts).toBe(2)
      }
      const saveCalls = fetchMock.mock.calls.filter(
        ([path, init]) =>
          String(path).endsWith("/draft") && init?.method === "PUT",
      )
      expect(new Headers(saveCalls[0]?.[1]?.headers).get("If-Match")).toBe(
        '"etag-1"',
      )
      expect(String(saveCalls[0]?.[1]?.body)).toContain(
        "Corpo modificato localmente",
      )
      if (!failure.locks)
        expect(new Headers(saveCalls[1]?.[1]?.headers).get("If-Match")).toBe(
          '"etag-1"',
        )
      if (!failure.locks)
        expect(String(saveCalls[1]?.[1]?.body)).toContain(
          "Corpo modificato localmente",
        )
    })
  }

  it("publishes, withdraws, and republishes with the latest ETag", async () => {
    window.history.replaceState({}, "", "/admin/articoli/article-1")
    const mutationETags: string[] = []
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input)
        if (path === "/api/admin/session")
          return jsonResponse({ username: "admin", csrfToken: "csrf" })
        if (path === "/api/admin/covers") return jsonResponse([])
        if (path === "/api/admin/articles/article-1" && !init?.method)
          return jsonResponse(articleDetail(), 200, { ETag: '"etag-1"' })
        if (init?.method === "POST") {
          mutationETags.push(new Headers(init.headers).get("If-Match") ?? "")
          const published = path.endsWith("/publish")
          const attempt = mutationETags.length
          return jsonResponse(
            articleDetail({
              status: published ? "published" : "withdrawn",
              firstPublishedAt: now,
              lastPublishedAt: now,
              published: {
                title: "Titolo articolo",
                summary: "Sommario sufficientemente lungo",
                publishedAt: now,
              },
            }),
            200,
            { ETag: `"etag-${attempt + 1}"` },
          )
        }
        throw new Error(`unexpected ${path}`)
      },
    )
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    render(<App />)

    await screen.findByDisplayValue("Titolo articolo")
    await user.click(screen.getByRole("button", { name: "Pubblica" }))
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Ripubblica" })).toBeEnabled(),
    )
    await user.click(screen.getByRole("button", { name: "Ritira" }))
    expect(await screen.findByRole("status")).toHaveTextContent(
      "Articolo ritirato",
    )
    expect(
      screen.queryByRole("button", { name: "Ritira" }),
    ).not.toBeInTheDocument()
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Ripubblica" })).toBeEnabled(),
    )
    await user.click(screen.getByRole("button", { name: "Ripubblica" }))
    expect(await screen.findByRole("button", { name: "Ritira" })).toBeEnabled()
    expect(mutationETags).toEqual(['"etag-1"', '"etag-2"', '"etag-3"'])
  })

  it("locks uncertain transitions for reconciliation while preserving local state", async () => {
    window.history.replaceState({}, "", "/admin/articoli/article-1")
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input)
        if (path === "/api/admin/session")
          return jsonResponse({ username: "admin", csrfToken: "csrf" })
        if (path === "/api/admin/covers") return jsonResponse([])
        if (path === "/api/admin/articles/article-1" && !init?.method)
          return jsonResponse(articleDetail(), 200, { ETag: '"etag-1"' })
        if (
          path === "/api/admin/articles/article-1/publish" &&
          init?.method === "POST"
        )
          return jsonResponse(
            { error: "outcome unknown", code: "commit_unknown" },
            503,
          )
        throw new Error(`unexpected ${path}`)
      },
    )
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    render(<App />)

    expect(
      await screen.findByDisplayValue("Titolo articolo"),
    ).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Pubblica" }))
    expect(await screen.findByRole("status")).toHaveTextContent(
      "Esito dell’operazione incerto",
    )
    expect(screen.getByDisplayValue("Titolo articolo")).toBeInTheDocument()
    expect(
      screen.getByRole("button", { name: "Ricarica per riconciliare" }),
    ).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Salva bozza" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "Pubblica" })).toBeDisabled()
  })

  it("keeps ordinary transition failures retryable", async () => {
    window.history.replaceState({}, "", "/admin/articoli/article-1")
    let attempts = 0
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input)
        if (path === "/api/admin/session")
          return jsonResponse({ username: "admin", csrfToken: "csrf" })
        if (path === "/api/admin/covers") return jsonResponse([])
        if (path === "/api/admin/articles/article-1" && !init?.method)
          return jsonResponse(articleDetail(), 200, { ETag: '"etag-1"' })
        if (path.endsWith("/publish") && init?.method === "POST") {
          attempts++
          if (attempts === 1)
            return jsonResponse(
              {
                error: "temporaneamente non disponibile",
                code: "articles_unavailable",
              },
              503,
            )
          return jsonResponse(
            {
              ...articleSummary("article-1", "Titolo articolo"),
              status: "published",
            },
            200,
            { ETag: '"etag-2"' },
          )
        }
        throw new Error(`unexpected ${path}`)
      },
    )
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    render(<App />)

    await screen.findByDisplayValue("Titolo articolo")
    const publish = screen.getByRole("button", { name: "Pubblica" })
    await user.click(publish)
    expect(await screen.findByRole("status")).toHaveTextContent(
      "temporaneamente non disponibile",
    )
    await waitFor(() => expect(publish).toBeEnabled())
    expect(
      screen.queryByRole("button", { name: "Ricarica per riconciliare" }),
    ).not.toBeInTheDocument()
    await user.click(publish)
    expect(await screen.findByRole("status")).toHaveTextContent(
      "Articolo pubblicato",
    )
    expect(attempts).toBe(2)
  })

  it("refreshes the saved preview only after a successful save and supports widths", async () => {
    window.history.replaceState({}, "", "/admin/articoli/article-1")
    let saves = 0
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input)
        if (path === "/api/admin/session")
          return jsonResponse({ username: "admin", csrfToken: "csrf" })
        if (path === "/api/admin/covers") return jsonResponse([])
        if (path === "/api/admin/articles/article-1" && !init?.method)
          return jsonResponse(articleDetail(), 200, { ETag: '"etag-1"' })
        if (path.endsWith("/draft") && init?.method === "PUT") {
          saves++
          if (saves === 2)
            return jsonResponse(
              { error: "temporaneamente non disponibile" },
              503,
            )
          return jsonResponse(
            articleSummary("article-1", "Titolo salvato"),
            200,
            {
              ETag: '"etag-2"',
            },
          )
        }
        throw new Error(`unexpected ${path}`)
      },
    )
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    render(<App />)

    await screen.findByDisplayValue("Titolo articolo")
    const frame = screen.getByTitle("Anteprima articolo salvato")
    const initialSource = frame.getAttribute("src")
    await user.click(screen.getByRole("button", { name: "360" }))
    expect(frame).toHaveStyle({ width: "360px" })
    await user.click(screen.getByRole("button", { name: "768" }))
    expect(frame).toHaveStyle({ width: "768px" })
    await user.click(screen.getByRole("button", { name: "Desktop" }))
    expect(frame).toHaveStyle({ width: "100%" })

    await user.clear(screen.getByLabelText("Titolo"))
    await user.type(screen.getByLabelText("Titolo"), "Titolo salvato")
    await user.click(screen.getByRole("button", { name: "Salva bozza" }))
    expect(await screen.findByRole("status")).toHaveTextContent("Bozza salvata")
    const savedSource = frame.getAttribute("src")
    expect(savedSource).not.toBe(initialSource)

    await user.type(screen.getByLabelText("Titolo"), " locale")
    await user.click(screen.getByRole("button", { name: "Salva bozza" }))
    expect(await screen.findByRole("status")).toHaveTextContent(
      "temporaneamente non disponibile",
    )
    expect(frame.getAttribute("src")).toBe(savedSource)
  })

  it("shows the published snapshot lifecycle and unpublished-change fact", async () => {
    window.history.replaceState({}, "", "/admin/articoli/article-1")
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input)
        if (path === "/api/admin/session")
          return jsonResponse({ username: "admin", csrfToken: "csrf" })
        if (path === "/api/admin/covers") return jsonResponse([])
        return jsonResponse(
          articleDetail({
            status: "withdrawn",
            firstPublishedAt: "2026-09-01T12:00:00Z",
            lastPublishedAt: "2026-09-02T12:00:00Z",
            hasUnpublishedChanges: true,
            published: {
              title: "Titolo pubblicato",
              summary: "Sommario pubblicato sufficientemente lungo",
              publishedAt: "2026-09-02T12:00:00Z",
            },
          }),
          200,
          { ETag: '"etag-1"' },
        )
      }),
    )
    render(<App />)

    expect(
      await screen.findByText("Modifiche non pubblicate"),
    ).toBeInTheDocument()
    expect(screen.getByText(/Prima pubblicazione/)).toBeInTheDocument()
    expect(screen.getByText(/Titolo pubblicato/)).toBeInTheDocument()
    expect(
      screen.getByRole("button", { name: "Ripubblica" }),
    ).toBeInTheDocument()
  })
})

function contactFixture(overrides: Partial<Contact> = {}): Contact {
  return {
    id: "contact-1",
    name: "Mario Rossi",
    email: "mario@example.test",
    phone: "+39 000 000000",
    message: "Messaggio sufficientemente lungo",
    consentVersion: "privacy-v1",
    privacyAcceptedAt: now,
    state: "new",
    createdAt: now,
    updatedAt: now,
    reviewDueAt: "2028-09-11T12:00:00Z",
    ...overrides,
  }
}

function articleSummary(id: string, title: string) {
  return {
    id,
    slug: id,
    title,
    summary: "Sommario sufficientemente lungo",
    area: "diritti-reali",
    coverId: "",
    status: "draft",
    createdAt: now,
    updatedAt: now,
  }
}

function articleDetail(overrides: Record<string, unknown> = {}) {
  return {
    ...articleSummary("article-1", "Titolo articolo"),
    body: {
      schemaVersion: 1,
      document: { type: "doc", content: [{ type: "paragraph" }] },
    },
    hasUnpublishedChanges: false,
    ...overrides,
  }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}

function jsonResponse(
  body: unknown,
  status = 200,
  headers: HeadersInit = {},
): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", ...headers },
  })
}
