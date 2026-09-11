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
  BrowserRouter,
  Link,
  Navigate,
  NavLink,
  Outlet,
  Route,
  Routes,
  useParams,
  useSearchParams,
} from "react-router-dom"

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
  return (
    <BrowserRouter basename="/admin">
      <Routes>
        <Route
          element={
            <AdminShell client={client} username={state.session.username} />
          }
        >
          <Route index element={<Dashboard client={client} />} />
          <Route path="contatti" element={<ContactList client={client} />} />
          <Route
            path="contatti/:id"
            element={<ContactDetail client={client} />}
          />
          <Route path="*" element={<Navigate replace to="/" />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
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
