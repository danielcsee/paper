# ui/src

Application source. The layout is flat: an entry point, a root component,
shared types, navigation, import tracking, the API client, plus
[`auth/`](auth) and [`components/`](components).

## Files

**`main.tsx`** — mounts `<App />` into `#root`, wrapped in `<AuthProvider>`,
and imports `styles.css`.

**`App.tsx`** — owns chat state, the open paper tabs and the visit stack.
Closing a paper tab pops that stack, skipping entries whose tab has since
closed: that is how "go back to where I was" works. `handleSend` calls
`/corpus/rag_search`; the backend runs no LLM, so answers are ranked evidence.

**`navigation.ts`** — `View`, the tab model, title truncation, the view↔URL
mapping, and `loadTabs`/`saveTabs`. The corpus UI route is `/my-corpus`, clear
of the `/corpus` API path. Open tabs persist to `localStorage`; the active view
does not, since the URL carries it. Reads are validated and access guarded —
the store throws outright in a private window.

**`useImportStatus.ts`** — tracks an import and polls `/import/status` until
every paper is terminal. Polling, not push: the Celery worker is a separate
process from the API, so pushing would need a Redis pub/sub bridge, and
Postgres is already the durable source of truth. Fast ticks first, then backing
off, with a five-minute cap.

**`api.ts`** — typed access to the `/pb`, `/import` and `/corpus` routes,
mirroring the backend response models, so **changing one there means changing
this file too**. Throws `ApiError`, which carries the HTTP status. Every call
goes through `authFetch` from [`auth/`](auth), which attaches the access token
and retries once through `/auth/refresh` on a 401.

**`types.ts`** — shared UI types. API payload types live in `api.ts`.

**`auth/`** — the access-code gate: token store, `useAuth()`, and the modal.
The app loads whole for everyone; this decides which controls work. See its
[README](auth) before wiring a new gated control — the gate takes the *work*,
never the gated function itself.

**`styles.css`** — all styling, hand-written. No framework, no CSS modules.
