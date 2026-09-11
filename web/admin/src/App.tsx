import {
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react"
import {
  createBrowserRouter,
  Link,
  Navigate,
  NavLink,
  Outlet,
  RouterProvider,
  useBlocker,
  useParams,
  useSearchParams,
  useNavigate,
} from "react-router-dom"
import { EditorContent, useEditor } from "@tiptap/react"
import StarterKit from "@tiptap/starter-kit"
import TiptapLink from "@tiptap/extension-link"

import {
  AdminClient,
  APIError,
  type ContactDetailDTO,
  type ContactPageDTO,
  type ContactState,
  type ContactSummaryDTO,
  type DashboardDTO,
  type PurgeDTO,
  type SessionDTO,
  type ArticleDetailDTO,
  type ArticleLifecycleDTO,
  type ArticleMutationDTO,
  type ArticlePageDTO,
  type ArticleStatus,
  type CoverDTO,
} from "./api"
import "./admin.css"

type AppProps = {
  onUnauthorized?: () => void
}

type SessionState =
  | { status: "loading" }
  | { status: "error"; message: string }
  | { status: "ready"; session: SessionDTO }

export default function App({ onUnauthorized = redirectToLogin }: AppProps) {
  const client = useMemo(
    () => new AdminClient(onUnauthorized),
    [onUnauthorized],
  )
  const [state, setState] = useState<SessionState>({ status: "loading" })

  useEffect(() => {
    let active = true
    void client
      .fetchJSON<SessionDTO>("/api/admin/session")
      .then(({ data }) => {
        if (!active) return
        client.setCSRFToken(data.csrfToken)
        setState({ status: "ready", session: data })
      })
      .catch((error: unknown) => {
        if (!active) return
        setState({
          status: "error",
          message:
            error instanceof APIError && error.status === 401
              ? "Sessione scaduta"
              : "Impossibile verificare la sessione",
        })
      })
    return () => {
      active = false
    }
  }, [client])

  if (state.status === "loading") {
    return <StatusPage>Caricamento sessione…</StatusPage>
  }
  if (state.status === "error") {
    return (
      <StatusPage>
        <span role="alert">{state.message}</span>
      </StatusPage>
    )
  }
  return <ReadyApp client={client} username={state.session.username} />
}

function ReadyApp({
  client,
  username,
}: {
  client: AdminClient
  username: string
}) {
  const router = useMemo(
    () =>
      createBrowserRouter(
        [
          {
            path: "/",
            element: <AdminShell client={client} username={username} />,
            children: [
              { index: true, element: <Dashboard client={client} /> },
              {
                path: "contatti",
                element: <ContactList client={client} />,
              },
              {
                path: "contatti/:id",
                element: <ContactDetail client={client} />,
              },
              {
                path: "articoli",
                element: <ArticleList client={client} />,
              },
              {
                path: "articoli/nuovo",
                element: <ArticleEditor client={client} />,
              },
              {
                path: "articoli/:id",
                element: <ArticleEditor client={client} />,
              },
              { path: "*", element: <Navigate replace to="/" /> },
            ],
          },
        ],
        { basename: "/admin" },
      ),
    [client, username],
  )
  return <RouterProvider router={router} />
}

function StatusPage({ children }: { children: ReactNode }) {
  return (
    <main className="status-page">
      <p>{children}</p>
    </main>
  )
}

function AdminShell({
  client,
  username,
}: {
  client: AdminClient
  username: string
}) {
  const [mobileOpen, setMobileOpen] = useState(false)
  const menuButton = useRef<HTMLButtonElement>(null)
  const closeButton = useRef<HTMLButtonElement>(null)
  const mobilePanel = useRef<HTMLElement>(null)

  const closeMobile = useCallback(() => {
    setMobileOpen(false)
    menuButton.current?.focus()
  }, [])

  useEffect(() => {
    if (!mobileOpen) return
    closeButton.current?.focus()
  }, [closeMobile, mobileOpen])

  const trapMobileFocus = (event: ReactKeyboardEvent<HTMLElement>) => {
    if (event.key === "Escape") {
      event.preventDefault()
      closeMobile()
      return
    }
    if (event.key !== "Tab") return
    const controls = mobilePanel.current?.querySelectorAll<HTMLElement>(
      "button:not(:disabled), a[href]",
    )
    if (!controls || controls.length === 0) return
    const first = controls[0]
    const last = controls[controls.length - 1]
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault()
      last.focus()
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault()
      first.focus()
    }
  }

  const logout = async () => {
    try {
      await client.fetchJSON<never>("/api/admin/session", { method: "DELETE" })
    } finally {
      redirectToLogin()
    }
  }

  return (
    <div className="admin-shell">
      <header className="topbar">
        <Link className="wordmark" to="/">
          Callegarin <span>amministrazione</span>
        </Link>
        <button
          aria-expanded={mobileOpen}
          aria-label="Apri navigazione"
          className="menu-button"
          onClick={() => setMobileOpen(true)}
          ref={menuButton}
          type="button"
        >
          Menu
        </button>
        <span className="operator">{username}</span>
      </header>
      <aside className="desktop-sidebar">
        <Navigation client={client} onNavigate={() => undefined} />
      </aside>
      {mobileOpen ? (
        <div className="mobile-overlay" role="presentation">
          <nav
            aria-label="Navigazione mobile"
            className="mobile-panel"
            onKeyDown={trapMobileFocus}
            ref={mobilePanel}
          >
            <button
              aria-label="Chiudi navigazione"
              className="close-button"
              onClick={closeMobile}
              ref={closeButton}
              type="button"
            >
              Chiudi
            </button>
            <Navigation client={client} onNavigate={closeMobile} />
          </nav>
        </div>
      ) : null}
      <main className="workspace" id="contenuto">
        <Outlet />
      </main>
      <button className="logout" onClick={() => void logout()} type="button">
        Esci
      </button>
    </div>
  )
}

function Navigation({
  onNavigate,
}: {
  client: AdminClient
  onNavigate: () => void
}) {
  return (
    <div className="navigation-links">
      <NavLink end onClick={onNavigate} to="/">
        Panoramica
      </NavLink>
      <NavLink onClick={onNavigate} to="/articoli">
        Articoli
      </NavLink>
      <NavLink onClick={onNavigate} to="/contatti">
        Contatti
      </NavLink>
    </div>
  )
}

function Dashboard({ client }: { client: AdminClient }) {
  const [state, setState] = useState<{
    loading: boolean
    data?: DashboardDTO
    error?: string
  }>({ loading: true })

  useEffect(() => {
    let active = true
    const load = async () => {
      try {
        const purge = await client.fetchJSON<PurgeDTO>(
          "/api/admin/contacts/purge-due",
          { method: "POST" },
        )
        const dashboard = await client.fetchJSON<DashboardDTO>(
          "/api/admin/dashboard",
        )
        if (active) {
          setState({
            loading: false,
            data: { ...dashboard.data, purged: purge.data.purged },
          })
        }
      } catch {
        if (active) {
          setState({
            loading: false,
            error: "Impossibile caricare la panoramica",
          })
        }
      }
    }
    void load()
    return () => {
      active = false
    }
  }, [client])

  return (
    <section>
      <PageHeading eyebrow="Console riservata" title="Panoramica" />
      {state.loading ? <p>Caricamento panoramica…</p> : null}
      {state.error ? <p role="alert">{state.error}</p> : null}
      {state.data ? (
        <>
          {state.data.new > 0 ? (
            <div className="notification-card">
              <span
                aria-label={`${state.data.new} nuovi contatti`}
                className="badge"
              >
                {state.data.new}
              </span>
              <p>{state.data.new} nuovi contatti</p>
              <Link to="/contatti?state=new">Apri la coda</Link>
            </div>
          ) : (
            <p className="empty-card">Nessun nuovo contatto.</p>
          )}
          <div className="metrics-grid">
            <Metric label="Letti" value={state.data.read} />
            <Metric label="Archiviati" value={state.data.archived} />
            <Metric
              label="In eliminazione"
              value={state.data.deletionScheduled}
            />
            <Metric label="Da rivedere" value={state.data.retentionReview} />
          </div>
          {state.data.purged > 0 ? (
            <p className="quiet-note">
              Eliminazioni scadute completate: {state.data.purged}.
            </p>
          ) : null}
        </>
      ) : null}
    </section>
  )
}

function Metric({ label, value }: { label: string; value: number }) {
  return (
    <article className="metric">
      <strong>{value}</strong>
      <span>{label}</span>
    </article>
  )
}

type ListState = {
  loading: boolean
  items: ContactSummaryDTO[]
  nextCursor: string
  error?: string
}

function appendUniqueContacts(
  current: ContactSummaryDTO[],
  incoming: ContactSummaryDTO[],
): ContactSummaryDTO[] {
  const ids = new Set(current.map((contact) => contact.id))
  return [
    ...current,
    ...incoming.filter((contact) => {
      if (ids.has(contact.id)) return false
      ids.add(contact.id)
      return true
    }),
  ]
}

function ContactList({ client }: { client: AdminClient }) {
  const [searchParams] = useSearchParams()
  const [query, setQuery] = useState("")
  const [debouncedQuery, setDebouncedQuery] = useState("")
  const [stateFilter, setStateFilter] = useState<ContactState | "">(() => {
    const state = searchParams.get("state")
    return state === "new" || state === "read" || state === "archived"
      ? state
      : ""
  })
  const [retentionReview, setRetentionReview] = useState(false)
  const [deletionScheduled, setDeletionScheduled] = useState(false)
  const [list, setList] = useState<ListState>({
    loading: true,
    items: [],
    nextCursor: "",
  })
  const requestController = useRef<AbortController>(undefined)
  const requestGeneration = useRef(0)
  const consumedCursors = useRef(new Set<string>())

  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedQuery(query), 300)
    return () => window.clearTimeout(timer)
  }, [query])

  const load = useCallback(
    async (cursor = "", append = false) => {
      const generation = ++requestGeneration.current
      requestController.current?.abort()
      const controller = new AbortController()
      requestController.current = controller
      if (append) {
        setList((current) => ({ ...current, loading: true, error: undefined }))
      } else {
        consumedCursors.current = new Set()
        setList({ loading: true, items: [], nextCursor: "" })
      }
      const seenCursors = append
        ? new Set(consumedCursors.current)
        : new Set<string>()
      let pageCursor = cursor
      try {
        for (;;) {
          if (pageCursor) seenCursors.add(pageCursor)
          const params = new URLSearchParams()
          if (debouncedQuery) params.set("q", debouncedQuery)
          if (stateFilter) params.set("state", stateFilter)
          if (pageCursor) params.set("cursor", pageCursor)
          if (retentionReview) params.set("retentionReview", "true")
          if (deletionScheduled) params.set("deletionScheduled", "true")
          const suffix = params.size > 0 ? `?${params.toString()}` : ""
          const { data } = await client.fetchJSON<ContactPageDTO>(
            `/api/admin/contacts${suffix}`,
            { signal: controller.signal },
          )
          if (
            controller.signal.aborted ||
            generation !== requestGeneration.current
          ) {
            return
          }
          if (data.nextCursor && seenCursors.has(data.nextCursor)) {
            consumedCursors.current = new Set()
            setList((current) => ({
              loading: false,
              items: append ? current.items : [],
              nextCursor: "",
              error: "Impossibile continuare: paginazione non valida",
            }))
            return
          }
          if (data.items.length > 0 || data.nextCursor === "") {
            consumedCursors.current = seenCursors
            setList((current) => ({
              loading: false,
              items: append
                ? appendUniqueContacts(current.items, data.items)
                : data.items,
              nextCursor: data.nextCursor,
            }))
            return
          }
          pageCursor = data.nextCursor
        }
      } catch {
        if (
          controller.signal.aborted ||
          generation !== requestGeneration.current
        ) {
          return
        }
        setList((current) => ({
          loading: false,
          items: append ? current.items : [],
          nextCursor: append ? cursor : "",
          error: "Impossibile caricare i contatti",
        }))
      }
    },
    [client, debouncedQuery, deletionScheduled, retentionReview, stateFilter],
  )

  useEffect(() => {
    void load()
    return () => {
      requestGeneration.current++
      requestController.current?.abort()
    }
  }, [load])

  return (
    <section>
      <PageHeading eyebrow="Corrispondenza" title="Contatti" />
      <div className="filters" role="search">
        <label>
          Cerca per nome o email
          <input
            aria-label="Cerca per nome o email"
            maxLength={120}
            onChange={(event) => setQuery(event.target.value.slice(0, 120))}
            type="search"
            value={query}
          />
        </label>
        <label>
          Stato
          <select
            aria-label="Stato"
            onChange={(event) =>
              setStateFilter(event.target.value as ContactState | "")
            }
            value={stateFilter}
          >
            <option value="">Tutti</option>
            <option value="new">Nuovi</option>
            <option value="read">Letti</option>
            <option value="archived">Archiviati</option>
          </select>
        </label>
        <label className="check-filter">
          <input
            checked={deletionScheduled}
            onChange={(event) => setDeletionScheduled(event.target.checked)}
            type="checkbox"
          />
          In eliminazione
        </label>
        <label className="check-filter">
          <input
            checked={retentionReview}
            onChange={(event) => setRetentionReview(event.target.checked)}
            type="checkbox"
          />
          Revisione conservazione
        </label>
      </div>
      {list.loading ? (
        <p aria-live="polite" role="status">
          Ricerca dei contatti in corso…
        </p>
      ) : null}
      {list.error ? <p role="alert">{list.error}</p> : null}
      {!list.loading &&
      !list.error &&
      list.items.length === 0 &&
      !list.nextCursor ? (
        <div className="empty-card">
          <h2>Nessun risultato</h2>
          <p>Nessun contatto corrisponde ai filtri selezionati.</p>
        </div>
      ) : null}
      {list.items.length > 0 ? (
        <ul className="contact-list">
          {list.items.map((contact) => (
            <li key={contact.id}>
              <Link to={`/contatti/${encodeURIComponent(contact.id)}`}>
                <span>
                  <strong>{contact.name}</strong>
                  <small>{contact.email}</small>
                </span>
                <StateLabel state={contact.state} />
              </Link>
            </li>
          ))}
        </ul>
      ) : null}
      {list.nextCursor ? (
        <button
          disabled={list.loading}
          onClick={() => void load(list.nextCursor, true)}
          type="button"
        >
          Carica altri
        </button>
      ) : null}
    </section>
  )
}

function ContactDetail({ client }: { client: AdminClient }) {
  const { id = "" } = useParams()
  const [contact, setContact] = useState<ContactDetailDTO>()
  const [etag, setETag] = useState("")
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [conflict, setConflict] = useState(false)
  const [confirmDeletion, setConfirmDeletion] = useState(false)
  const scheduleButton = useRef<HTMLButtonElement>(null)

  const mutate = useCallback(
    async (action: string, currentETag = etag) => {
      setBusy(true)
      setError("")
      setConflict(false)
      try {
        const result = await client.fetchJSON<ContactDetailDTO>(
          `/api/admin/contacts/${encodeURIComponent(id)}/${action}`,
          {
            method: "POST",
            ifMatch: currentETag,
          },
        )
        setContact(result.data)
        setETag(result.etag ?? "")
        return result.data
      } catch (reason) {
        if (reason instanceof APIError && reason.status === 409) {
          setConflict(true)
        } else {
          setError("Operazione non riuscita. Riprova.")
        }
        return undefined
      } finally {
        setBusy(false)
      }
    },
    [client, etag, id],
  )

  const load = useCallback(async () => {
    setLoading(true)
    setError("")
    setConflict(false)
    try {
      const result = await client.fetchJSON<ContactDetailDTO>(
        `/api/admin/contacts/${encodeURIComponent(id)}`,
      )
      let loaded = result.data
      let loadedETag = result.etag ?? ""
      if (loaded.state === "new") {
        const readResult = await client.fetchJSON<ContactDetailDTO>(
          `/api/admin/contacts/${encodeURIComponent(id)}/read`,
          {
            method: "POST",
            ifMatch: loadedETag,
          },
        )
        loaded = readResult.data
        loadedETag = readResult.etag ?? ""
      }
      setContact(loaded)
      setETag(loadedETag)
    } catch (reason) {
      if (reason instanceof APIError && reason.status === 409) setConflict(true)
      else setError("Impossibile caricare il contatto")
    } finally {
      setLoading(false)
    }
  }, [client, id])

  useEffect(() => {
    void load()
  }, [load])

  if (loading) return <p>Caricamento contatto…</p>
  if (!contact) {
    return (
      <section>
        {error ? <p role="alert">{error}</p> : null}
        {conflict ? <ConflictNotice onReload={() => void load()} /> : null}
      </section>
    )
  }

  return (
    <section>
      <Link className="back-link" to="/contatti">
        ← Tutti i contatti
      </Link>
      <PageHeading
        eyebrow={formatDate(contact.createdAt)}
        title={contact.name}
      />
      <div className="detail-status">
        <StateLabel state={contact.state} />
        {new Date(contact.reviewDueAt) <= new Date() ? (
          <span className="retention-flag">
            Revisione conservazione richiesta
          </span>
        ) : null}
      </div>
      {conflict ? <ConflictNotice onReload={() => void load()} /> : null}
      {error ? <p role="alert">{error}</p> : null}
      {contact.deletionDueAt ? (
        <div className="deletion-notice">
          <strong>Eliminazione programmata</strong>
          <p>Recuperabile fino al {formatDate(contact.deletionDueAt)}.</p>
          <button
            disabled={busy}
            onClick={() => void mutate("cancel-deletion")}
            type="button"
          >
            Annulla eliminazione programmata
          </button>
        </div>
      ) : null}
      <div className="detail-grid">
        <article className="message-card">
          <h2>Messaggio</h2>
          <p>{contact.message}</p>
        </article>
        <dl className="contact-data">
          <div>
            <dt>Email</dt>
            <dd>
              <a href={`mailto:${contact.email}`}>{contact.email}</a>
            </dd>
          </div>
          {contact.phone ? (
            <div>
              <dt>Telefono</dt>
              <dd>{contact.phone}</dd>
            </div>
          ) : null}
          <div>
            <dt>Consenso privacy</dt>
            <dd>
              {contact.consentVersion}, {formatDate(contact.privacyAcceptedAt)}
            </dd>
          </div>
        </dl>
      </div>
      <div className="actions">
        {contact.state === "read" ? (
          <button
            disabled={busy}
            onClick={() => void mutate("archive")}
            type="button"
          >
            Archivia
          </button>
        ) : null}
        {contact.state === "archived" ? (
          <button
            disabled={busy}
            onClick={() => void mutate("restore")}
            type="button"
          >
            Ripristina
          </button>
        ) : null}
        {!contact.deletionDueAt ? (
          <button
            className="danger-secondary"
            disabled={busy}
            onClick={() => setConfirmDeletion(true)}
            ref={scheduleButton}
            type="button"
          >
            Programma eliminazione
          </button>
        ) : null}
      </div>
      {confirmDeletion ? (
        <ConfirmationDialog
          onCancel={() => setConfirmDeletion(false)}
          onConfirm={() => {
            setConfirmDeletion(false)
            void mutate("schedule-deletion")
          }}
          returnFocus={scheduleButton.current}
        />
      ) : null}
    </section>
  )
}

function ConflictNotice({ onReload }: { onReload: () => void }) {
  return (
    <div className="conflict" role="alert">
      <p>Il contatto è stato modificato in un’altra sessione.</p>
      <button onClick={onReload} type="button">
        Ricarica contatto
      </button>
    </div>
  )
}

function ConfirmationDialog({
  onCancel,
  onConfirm,
  returnFocus,
}: {
  onCancel: () => void
  onConfirm: () => void
  returnFocus: HTMLElement | null
}) {
  const cancelButton = useRef<HTMLButtonElement>(null)
  const confirmButton = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    cancelButton.current?.focus()
  }, [])

  const close = (confirmed: boolean) => {
    if (confirmed) onConfirm()
    else onCancel()
    returnFocus?.focus()
  }

  const trapFocus = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Escape") {
      event.preventDefault()
      close(false)
      return
    }
    if (event.key !== "Tab") return
    if (event.shiftKey && document.activeElement === cancelButton.current) {
      event.preventDefault()
      confirmButton.current?.focus()
    } else if (
      !event.shiftKey &&
      document.activeElement === confirmButton.current
    ) {
      event.preventDefault()
      cancelButton.current?.focus()
    }
  }

  return (
    <div className="dialog-backdrop">
      <div
        aria-labelledby="deletion-dialog-title"
        aria-modal="true"
        className="dialog"
        onKeyDown={trapFocus}
        role="dialog"
      >
        <p className="eyebrow">Azione sensibile</p>
        <h2 id="deletion-dialog-title">Conferma eliminazione programmata</h2>
        <p>
          Il contatto resterà recuperabile per 30 giorni. La revisione dei 24
          mesi, da sola, non elimina mai un contatto.
        </p>
        <div className="dialog-actions">
          <button onClick={() => close(false)} ref={cancelButton} type="button">
            Annulla
          </button>
          <button
            className="danger"
            onClick={() => close(true)}
            ref={confirmButton}
            type="button"
          >
            Conferma, elimina tra 30 giorni
          </button>
        </div>
      </div>
    </div>
  )
}

function StateLabel({ state }: { state: ContactState }) {
  const labels: Record<ContactState, string> = {
    new: "Nuovo",
    read: "Letto",
    archived: "Archiviato",
  }
  return <span className={`state state-${state}`}>{labels[state]}</span>
}

const articleAreas = [
  "famiglia-e-persone",
  "successioni-e-donazioni",
  "obbligazioni-e-contratti",
  "recupero-crediti",
  "risarcimento-danni",
  "diritti-reali",
  "diritto-penale",
  "diritto-tributario",
]

function ArticleList({ client }: { client: AdminClient }) {
  const [status, setStatus] = useState<ArticleStatus | "">("")
  const [page, setPage] = useState<ArticlePageDTO>({
    items: [],
    nextCursor: "",
  })
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState("")
  const [retry, setRetry] = useState(0)
  const generation = useRef(0)
  const activeRequest = useRef<AbortController | null>(null)

  const load = useCallback(
    async (cursor: string, append: boolean, requestGeneration: number) => {
      activeRequest.current?.abort()
      const controller = new AbortController()
      activeRequest.current = controller
      append ? setLoadingMore(true) : setLoading(true)
      setError("")

      const seenCursors = new Set<string>()
      let currentCursor = cursor
      try {
        while (true) {
          if (seenCursors.has(currentCursor)) {
            throw new Error("article cursor cycle")
          }
          seenCursors.add(currentCursor)
          const query = new URLSearchParams()
          if (status) query.set("status", status)
          if (currentCursor) query.set("cursor", currentCursor)
          const suffix = query.size > 0 ? `?${query.toString()}` : ""
          const { data } = await client.fetchJSON<ArticlePageDTO>(
            `/api/admin/articles${suffix}`,
            { signal: controller.signal },
          )
          if (
            controller.signal.aborted ||
            generation.current !== requestGeneration
          ) {
            return
          }
          if (data.nextCursor !== "" && seenCursors.has(data.nextCursor)) {
            throw new Error("article cursor cycle")
          }

          if (data.items.length > 0 || data.nextCursor === "") {
            setPage((previous) => {
              const merged = append
                ? [...previous.items, ...data.items]
                : data.items
              return {
                items: Array.from(
                  new Map(
                    merged.map((article) => [article.id, article]),
                  ).values(),
                ),
                nextCursor: data.nextCursor,
              }
            })
            return
          }
          currentCursor = data.nextCursor
        }
      } catch (reason: unknown) {
        if (
          controller.signal.aborted ||
          generation.current !== requestGeneration ||
          (reason instanceof DOMException && reason.name === "AbortError")
        ) {
          return
        }
        setError("Impossibile caricare gli articoli")
      } finally {
        if (
          !controller.signal.aborted &&
          generation.current === requestGeneration
        ) {
          append ? setLoadingMore(false) : setLoading(false)
        }
      }
    },
    [client, status],
  )

  useEffect(() => {
    const requestGeneration = ++generation.current
    setPage({ items: [], nextCursor: "" })
    void load("", false, requestGeneration)
    return () => activeRequest.current?.abort()
  }, [load, retry])

  return (
    <section>
      <PageHeading eyebrow="Pubblicazione" title="Articoli" />
      <div className="article-list-actions">
        <Link className="primary" to="/articoli/nuovo">
          Nuovo articolo
        </Link>
        <label>
          Stato{" "}
          <select
            value={status}
            onChange={(event) =>
              setStatus(event.target.value as ArticleStatus | "")
            }
          >
            <option value="">Tutti</option>
            <option value="draft">Bozza</option>
            <option value="published">Pubblicato</option>
            <option value="withdrawn">Ritirato</option>
          </select>
        </label>
      </div>
      {loading ? <p>Caricamento articoli…</p> : null}
      {error ? (
        <div role="alert">
          <p>{error}</p>
          <button onClick={() => setRetry((value) => value + 1)} type="button">
            Riprova
          </button>
        </div>
      ) : null}
      {!loading && !error && page.items.length === 0 ? (
        <p>Nessun articolo</p>
      ) : null}
      <div className="article-cards">
        {page.items.map((article) => (
          <article className="article-admin-card" key={article.id}>
            <p className="eyebrow">{article.status}</p>
            <h2>
              <Link to={`/articoli/${article.id}`}>{article.title}</Link>
            </h2>
            <p>{article.summary}</p>
            <small>Aggiornato {formatDate(article.updatedAt)}</small>
          </article>
        ))}
      </div>
      {!loading && !error && page.nextCursor ? (
        <button
          disabled={loadingMore}
          onClick={() => void load(page.nextCursor, true, generation.current)}
          type="button"
        >
          {loadingMore ? "Caricamento…" : "Carica altri"}
        </button>
      ) : null}
    </section>
  )
}

function UnsavedChangesDialog({
  onLeave,
  onStay,
  returnFocus,
}: {
  onLeave: () => void
  onStay: () => void
  returnFocus: HTMLElement | null
}) {
  const stayButton = useRef<HTMLButtonElement>(null)
  const leaveButton = useRef<HTMLButtonElement>(null)
  const focusTarget = useRef(returnFocus)

  useEffect(() => {
    stayButton.current?.focus()
  }, [])

  const stay = () => {
    onStay()
    focusTarget.current?.focus()
  }
  const trapFocus = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Escape") {
      event.preventDefault()
      stay()
      return
    }
    if (event.key !== "Tab") return
    if (event.shiftKey && document.activeElement === stayButton.current) {
      event.preventDefault()
      leaveButton.current?.focus()
    } else if (
      !event.shiftKey &&
      document.activeElement === leaveButton.current
    ) {
      event.preventDefault()
      stayButton.current?.focus()
    }
  }

  return (
    <div className="dialog-backdrop">
      <div
        aria-labelledby="unsaved-dialog-title"
        aria-modal="true"
        className="dialog"
        onKeyDown={trapFocus}
        role="dialog"
      >
        <h2 id="unsaved-dialog-title">Modifiche non salvate</h2>
        <p>Se esci adesso, le modifiche locali andranno perse.</p>
        <div className="dialog-actions">
          <button onClick={stay} ref={stayButton} type="button">
            Resta
          </button>
          <button onClick={onLeave} ref={leaveButton} type="button">
            Esci
          </button>
        </div>
      </div>
    </div>
  )
}

function ArticleEditor({ client }: { client: AdminClient }) {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [form, setForm] = useState({
    slug: "",
    title: "",
    summary: "",
    area: articleAreas[0],
    coverId: "",
  })
  const [covers, setCovers] = useState<CoverDTO[]>([])
  const [etag, setETag] = useState<string | null>(null)
  const [status, setStatus] = useState<ArticleStatus>("draft")
  const [lifecycle, setLifecycle] = useState<ArticleLifecycleDTO>({
    hasUnpublishedChanges: false,
  })
  const [dirty, setDirty] = useState(false)
  const [reloadRequired, setReloadRequired] = useState(false)
  const [previewWidth, setPreviewWidth] = useState("100%")
  const [previewVersion, setPreviewVersion] = useState(0)
  const [message, setMessage] = useState("")
  const [pendingAction, setPendingAction] = useState<
    "save" | "publish" | "withdraw" | null
  >(null)
  const mutation = useRef<symbol | null>(null)
  const allowNavigation = useRef(false)
  const blocker = useBlocker(
    useCallback(() => dirty && !allowNavigation.current, [dirty]),
  )
  const editor = useEditor({
    extensions: [
      StarterKit.configure({
        heading: { levels: [2, 3] },
        code: false,
        codeBlock: false,
        strike: false,
        underline: false,
        link: false,
        horizontalRule: false,
        hardBreak: false,
      }),
      TiptapLink.configure({ openOnClick: false, autolink: false }),
    ],
    content: { type: "doc", content: [{ type: "paragraph" }] },
    onUpdate: () => setDirty(true),
  })
  useEffect(() => {
    void client
      .fetchJSON<CoverDTO[]>("/api/admin/covers")
      .then(({ data }) => setCovers(data))
  }, [client])
  useEffect(() => {
    if (!id || !editor) return
    const controller = new AbortController()
    void client
      .fetchJSON<ArticleDetailDTO>(`/api/admin/articles/${id}`, {
        signal: controller.signal,
      })
      .then(({ data, etag: freshETag }) => {
        setForm({
          slug: data.slug,
          title: data.title,
          summary: data.summary,
          area: data.area,
          coverId: data.coverId,
        })
        setStatus(data.status)
        setLifecycle(data)
        setETag(freshETag)
        editor.commands.setContent(data.body.document as never)
        setDirty(false)
      })
      .catch(() => setMessage("Impossibile caricare l’articolo"))
    return () => controller.abort()
  }, [client, editor, id])
  useEffect(() => {
    if (!dirty) return
    const unload = (event: BeforeUnloadEvent) => {
      event.preventDefault()
      event.returnValue = ""
    }
    window.addEventListener("beforeunload", unload)
    return () => {
      window.removeEventListener("beforeunload", unload)
    }
  }, [dirty])
  useEffect(() => {
    editor?.setEditable(pendingAction === null, false)
  }, [editor, pendingAction])
  const change = (field: keyof typeof form, value: string) => {
    setForm((current) => ({ ...current, [field]: value }))
    setDirty(true)
  }
  const payload = () => ({
    ...form,
    body: {
      schemaVersion: 1 as const,
      document: editor?.getJSON() ?? { type: "doc" },
    },
  })
  const beginMutation = (action: "save" | "publish" | "withdraw") => {
    if (mutation.current !== null || reloadRequired) return null
    const token = Symbol(action)
    mutation.current = token
    setPendingAction(action)
    setMessage(
      action === "save"
        ? "Salvataggio in corso…"
        : action === "publish"
          ? "Pubblicazione in corso…"
          : "Ritiro in corso…",
    )
    return token
  }
  const finishMutation = (token: symbol) => {
    if (mutation.current !== token) return
    mutation.current = null
    setPendingAction(null)
  }
  const save = async () => {
    if (!editor) return
    const token = beginMutation("save")
    if (token === null) return
    try {
      if (!id) {
        const result = await client.fetchJSON<ArticleDetailDTO>(
          "/api/admin/articles",
          { method: "POST", body: payload() },
        )
        allowNavigation.current = true
        setDirty(false)
        navigate(`/articoli/${result.data.id}`, { replace: true })
      } else {
        const result = await client.fetchJSON<ArticleMutationDTO>(
          `/api/admin/articles/${id}/draft`,
          { method: "PUT", ifMatch: etag ?? undefined, body: payload() },
        )
        setETag(result.etag)
        setStatus(result.data.status)
        setLifecycle(result.data)
        setDirty(false)
        setPreviewVersion((version) => version + 1)
        setMessage("Bozza salvata")
      }
    } catch (reason) {
      if (
        reason instanceof APIError &&
        (reason.code === "commit_unknown" || reason.code === "article_conflict")
      ) {
        setReloadRequired(true)
        setMessage(
          reason.code === "commit_unknown"
            ? "Esito del salvataggio incerto: ricarica prima di riprovare. Il testo locale è preservato."
            : "Conflitto con una versione più recente: il testo locale è preservato. Ricarica per riconciliare.",
        )
      } else
        setMessage(
          reason instanceof APIError
            ? reason.message
            : "Salvataggio non riuscito",
        )
    } finally {
      finishMutation(token)
    }
  }
  const transition = async (action: "publish" | "withdraw") => {
    if (!id || !etag || dirty) return
    const token = beginMutation(action)
    if (token === null) return
    try {
      const result = await client.fetchJSON<ArticleMutationDTO>(
        `/api/admin/articles/${id}/${action}`,
        { method: "POST", ifMatch: etag },
      )
      setETag(result.etag)
      setStatus(result.data.status)
      setLifecycle(result.data)
      setMessage(
        action === "publish" ? "Articolo pubblicato" : "Articolo ritirato",
      )
    } catch (reason) {
      if (
        reason instanceof APIError &&
        (reason.code === "commit_unknown" || reason.code === "article_conflict")
      ) {
        setReloadRequired(true)
        setMessage(
          reason.code === "commit_unknown"
            ? "Esito dell’operazione incerto: ricarica prima di riprovare. Lo stato locale è preservato."
            : "Conflitto con una versione più recente: lo stato locale è preservato. Ricarica per riconciliare.",
        )
      } else {
        setMessage(
          reason instanceof APIError
            ? reason.message
            : "Operazione non riuscita",
        )
      }
    } finally {
      finishMutation(token)
    }
  }
  return (
    <section className="article-editor">
      <PageHeading
        eyebrow={id ? status : "Nuova bozza locale"}
        title={id ? "Modifica articolo" : "Nuovo articolo"}
      />
      {dirty ? <p className="dirty-indicator">Modifiche non salvate</p> : null}
      {blocker.state === "blocked" ? (
        <UnsavedChangesDialog
          onLeave={() => {
            allowNavigation.current = true
            blocker.proceed()
          }}
          onStay={() => blocker.reset()}
          returnFocus={document.activeElement as HTMLElement | null}
        />
      ) : null}
      {message ? <p role="status">{message}</p> : null}
      {reloadRequired ? (
        <button type="button" onClick={() => window.location.reload()}>
          Ricarica per riconciliare
        </button>
      ) : null}
      {id && lifecycle.firstPublishedAt ? (
        <div className="article-lifecycle">
          {lifecycle.hasUnpublishedChanges ? (
            <p>Modifiche non pubblicate</p>
          ) : null}
          <p>Prima pubblicazione {formatDate(lifecycle.firstPublishedAt)}</p>
          {lifecycle.lastPublishedAt ? (
            <p>Ultima pubblicazione {formatDate(lifecycle.lastPublishedAt)}</p>
          ) : null}
          {lifecycle.published ? (
            <p>Versione pubblicata: {lifecycle.published.title}</p>
          ) : null}
        </div>
      ) : null}
      <div className="article-fields">
        <label>
          Titolo{" "}
          <input
            disabled={pendingAction !== null}
            value={form.title}
            onChange={(e) => change("title", e.target.value)}
          />
        </label>
        <label>
          Slug{" "}
          <input
            disabled={pendingAction !== null}
            value={form.slug}
            onChange={(e) => change("slug", e.target.value)}
          />
        </label>
        <label>
          Sommario{" "}
          <textarea
            disabled={pendingAction !== null}
            value={form.summary}
            onChange={(e) => change("summary", e.target.value)}
          />
        </label>
        <label>
          Area{" "}
          <select
            disabled={pendingAction !== null}
            value={form.area}
            onChange={(e) => change("area", e.target.value)}
          >
            {articleAreas.map((area) => (
              <option key={area}>{area}</option>
            ))}
          </select>
        </label>
        <label>
          Copertina{" "}
          <select
            disabled={pendingAction !== null}
            value={form.coverId}
            onChange={(e) => change("coverId", e.target.value)}
          >
            <option value="">Nessuna (fallback editoriale)</option>
            {covers.map((cover) => (
              <option key={cover.id} value={cover.id}>
                {cover.alt}
              </option>
            ))}
          </select>
        </label>
      </div>
      <div className="editor-toolbar" aria-label="Formattazione">
        <button
          disabled={pendingAction !== null}
          type="button"
          onClick={() => editor?.chain().focus().toggleBold().run()}
        >
          Grassetto
        </button>
        <button
          disabled={pendingAction !== null}
          type="button"
          onClick={() => editor?.chain().focus().toggleItalic().run()}
        >
          Corsivo
        </button>
        <button
          disabled={pendingAction !== null}
          type="button"
          onClick={() =>
            editor?.chain().focus().toggleHeading({ level: 2 }).run()
          }
        >
          Titolo 2
        </button>
        <button
          disabled={pendingAction !== null}
          type="button"
          onClick={() =>
            editor?.chain().focus().toggleHeading({ level: 3 }).run()
          }
        >
          Titolo 3
        </button>
        <button
          disabled={pendingAction !== null}
          type="button"
          onClick={() => editor?.chain().focus().toggleBulletList().run()}
        >
          Elenco
        </button>
        <button
          disabled={pendingAction !== null}
          type="button"
          onClick={() => editor?.chain().focus().toggleOrderedList().run()}
        >
          Numerato
        </button>
        <button
          disabled={pendingAction !== null}
          type="button"
          onClick={() => editor?.chain().focus().toggleBlockquote().run()}
        >
          Citazione
        </button>
        <button
          disabled={pendingAction !== null}
          type="button"
          onClick={() => {
            const href = window.prompt("Indirizzo del collegamento")
            if (href) editor?.chain().focus().setLink({ href }).run()
          }}
        >
          Collegamento
        </button>
      </div>
      <EditorContent className="tiptap-surface" editor={editor} />
      <div className="article-editor-actions">
        <button
          className="primary"
          disabled={reloadRequired || pendingAction !== null}
          type="button"
          onClick={() => void save()}
        >
          {id ? "Salva bozza" : "Crea bozza"}
        </button>
        {id ? (
          <>
            <button
              disabled={dirty || reloadRequired || pendingAction !== null}
              type="button"
              onClick={() => void transition("publish")}
            >
              {lifecycle.published ? "Ripubblica" : "Pubblica"}
            </button>
            {status === "published" ? (
              <button
                disabled={dirty || reloadRequired || pendingAction !== null}
                type="button"
                onClick={() => void transition("withdraw")}
              >
                Ritira
              </button>
            ) : null}
          </>
        ) : null}
      </div>
      {id ? (
        <div className="preview-panel">
          <h2>Anteprima ultima bozza salvata</h2>
          <div className="preview-sizes">
            <button type="button" onClick={() => setPreviewWidth("360px")}>
              360
            </button>
            <button type="button" onClick={() => setPreviewWidth("768px")}>
              768
            </button>
            <button type="button" onClick={() => setPreviewWidth("100%")}>
              Desktop
            </button>
            <a
              href={`/admin/preview/articles/${id}`}
              target="_blank"
              rel="noreferrer"
            >
              Apri in nuova scheda
            </a>
          </div>
          <iframe
            style={{ width: previewWidth }}
            title="Anteprima articolo salvato"
            src={`/admin/preview/articles/${id}?saved=${previewVersion}`}
          />
        </div>
      ) : null}
    </section>
  )
}

function PageHeading({ eyebrow, title }: { eyebrow: string; title: string }) {
  return (
    <header className="page-heading">
      <p className="eyebrow">{eyebrow}</p>
      <h1>{title}</h1>
    </header>
  )
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat("it-IT", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value))
}

function redirectToLogin(): void {
  window.location.assign("/admin/login")
}
