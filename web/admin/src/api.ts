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

type RequestOptions = {
  method?: "POST" | "DELETE"
  ifMatch?: string
}

export type APIResult<T> = {
  data: T
  etag: string | null
}

export class APIError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = "APIError"
    this.status = status
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
    const response = await fetch(path, {
      method: options.method,
      credentials: "same-origin",
      headers,
    })
    if (response.status === 401 && !this.#transitionedToLogin) {
      this.#transitionedToLogin = true
      this.#onUnauthorized()
    }
    if (!response.ok) {
      let message = "Richiesta non riuscita"
      try {
        const payload = (await response.json()) as { error?: string }
        if (typeof payload.error === "string" && payload.error !== "") {
          message = payload.error
        }
      } catch {
        // The UI intentionally falls back to one generic message.
      }
      throw new APIError(response.status, message)
    }
    if (response.status === 204) {
      return { data: undefined as T, etag: response.headers.get("ETag") }
    }
    const data = (await response.json()) as T
    return { data, etag: response.headers.get("ETag") }
  }
}
