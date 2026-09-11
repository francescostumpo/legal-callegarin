export type SessionDTO = {
  username: string
  csrfToken: string
}

export type DashboardDTO = {
  new: number
  read: number
  archived: number
  deletionScheduled: number
  retentionReview: number
  purged: number
}

export type PurgeDTO = {
  purged: number
}

export type ContactState = "new" | "read" | "archived"

export type ContactSummaryDTO = {
  id: string
  name: string
  email: string
  state: ContactState
  createdAt: string
  updatedAt: string
  reviewDueAt: string
  deletionDueAt?: string
}

export type ContactDetailDTO = ContactSummaryDTO & {
  phone?: string
  message: string
  consentVersion: string
  privacyAcceptedAt: string
  readAt?: string
  archivedAt?: string
}

export type ContactPageDTO = {
  items: ContactSummaryDTO[]
  nextCursor: string
}

export type ArticleStatus = "draft" | "published" | "withdrawn"
export type ArticleDocument = { type: "doc"; content?: unknown[] }
export type ArticleSummaryDTO = {
  id: string
  slug: string
  title: string
  summary: string
  area: string
  coverId: string
  status: ArticleStatus
  createdAt: string
  updatedAt: string
}
export type PublishedArticleSnapshotDTO = {
  slug: string
  title: string
  summary: string
  area: string
  coverId: string
}
export type ArticleLifecycleDTO = {
  published?: PublishedArticleSnapshotDTO
  firstPublishedAt?: string
  lastPublishedAt?: string
  hasUnpublishedChanges: boolean
}
export type ArticleMutationDTO = ArticleSummaryDTO & ArticleLifecycleDTO
export type ArticleDetailDTO = ArticleMutationDTO & {
  body: { schemaVersion: 1; document: ArticleDocument }
}
export type ArticlePageDTO = { items: ArticleSummaryDTO[]; nextCursor: string }
export type CoverDTO = {
  id: string
  alt: string
  cardAvif: string
  cardWebp: string
  landscapeAvif: string
  landscapeWebp: string
}

type RequestOptions = {
  method?: "POST" | "PUT" | "DELETE"
  ifMatch?: string
  signal?: AbortSignal
  body?: unknown
}

export type APIResult<T> = {
  data: T
  etag: string | null
}

export class APIError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, message: string, code = "") {
    super(message)
    this.name = "APIError"
    this.status = status
    this.code = code
  }
}

export class AdminClient {
  #csrfToken = ""
  #transitionedToLogin = false
  readonly #onUnauthorized: () => void

  constructor(onUnauthorized: () => void) {
    this.#onUnauthorized = onUnauthorized
  }

  setCSRFToken(token: string): void {
    this.#csrfToken = token
  }

  async fetchJSON<T>(
    path: string,
    options: RequestOptions = {},
  ): Promise<APIResult<T>> {
    const headers: Record<string, string> = { Accept: "application/json" }
    if (options.method !== undefined) {
      headers["X-CSRF-Token"] = this.#csrfToken
    }
    if (options.ifMatch !== undefined) {
      headers["If-Match"] = options.ifMatch
    }
    if (options.body !== undefined) headers["Content-Type"] = "application/json"
    const response = await fetch(path, {
      method: options.method,
      credentials: "same-origin",
      headers,
      body:
        options.body === undefined ? undefined : JSON.stringify(options.body),
      signal: options.signal,
    })
    if (response.status === 401 && !this.#transitionedToLogin) {
      this.#transitionedToLogin = true
      this.#onUnauthorized()
    }
    if (!response.ok) {
      let message = "Richiesta non riuscita"
      let code = ""
      try {
        const payload = (await response.json()) as {
          error?: string
          code?: string
        }
        if (typeof payload.error === "string" && payload.error !== "") {
          message = payload.error
        }
        code = payload.code ?? ""
      } catch {
        // The UI intentionally falls back to one generic message.
      }
      throw new APIError(response.status, message, code)
    }
    if (response.status === 204) {
      return { data: undefined as T, etag: response.headers.get("ETag") }
    }
    const data = (await response.json()) as T
    return { data, etag: response.headers.get("ETag") }
  }
}
