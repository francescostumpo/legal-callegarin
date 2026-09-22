import { afterEach, describe, expect, it, vi } from "vitest"

import { AdminClient, APIError } from "./api"

afterEach(() => vi.unstubAllGlobals())

describe("AdminClient", () => {
  it("accepts an empty successful response", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(null, { status: 204 })),
    )
    const client = new AdminClient(vi.fn())

    await expect(
      client.fetchJSON<void>("/api/admin/session", { method: "DELETE" }),
    ).resolves.toEqual({ data: undefined, etag: null })
  })

  it("uses same-origin credentials and transitions to login only once", async () => {
    const onUnauthorized = vi.fn()
    const fetchMock = vi.fn(
      async () =>
        new Response(JSON.stringify({ error: "non autorizzato" }), {
          status: 401,
          headers: { "Content-Type": "application/json" },
        }),
    )
    vi.stubGlobal("fetch", fetchMock)
    const client = new AdminClient(onUnauthorized)
    client.setCSRFToken("csrf-in-memory")

    const first = client.fetchJSON("/api/admin/contacts/id/read", {
      method: "POST",
      ifMatch: '"etag"',
    })
    const second = client.fetchJSON("/api/admin/contacts/id/archive", {
      method: "POST",
      ifMatch: '"etag"',
    })

    await expect(first).rejects.toEqual(
      expect.objectContaining<Partial<APIError>>({ status: 401 }),
    )
    await expect(second).rejects.toEqual(
      expect.objectContaining<Partial<APIError>>({ status: 401 }),
    )
    expect(onUnauthorized).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/admin/contacts/id/read",
      expect.objectContaining({
        credentials: "same-origin",
        headers: expect.objectContaining({
          "If-Match": '"etag"',
          "X-CSRF-Token": "csrf-in-memory",
        }),
      }),
    )
  })

  it("parses the nested safe error contract including request reference and fields", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              error: {
                code: "article_validation",
                message: "Controlla i dati inseriti.",
                requestId: "request-safe-1",
                fields: { title: "Inserisci un titolo." },
              },
            }),
            { status: 422, headers: { "Content-Type": "application/json" } },
          ),
      ),
    )

    const client = new AdminClient(vi.fn())
    await expect(
      client.fetchJSON("/api/admin/articles/id/draft"),
    ).rejects.toEqual(
      expect.objectContaining<Partial<APIError>>({
        status: 422,
        code: "article_validation",
        message: "Controlla i dati inseriti.",
        requestId: "request-safe-1",
        fields: { title: "Inserisci un titolo." },
      }),
    )
  })
})
