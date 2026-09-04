# ui/src

Application source. The layout is flat: an entry point, a root component,
shared types, navigation, import tracking, the API client, and
[`components/`](components).

## Files

**`main.tsx`** — mounts `<App />` into `#root` and imports `styles.css`.

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
this file too**. Throws `ApiError`, which carries the HTTP status.

**`types.ts`** — shared UI types. API payload types live in `api.ts`.

**`styles.css`** — all styling, hand-written. No framework, no CSS modules.
