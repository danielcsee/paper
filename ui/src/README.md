# ui/src

Application source. Small enough that the layout is flat: an entry point, a
root component, shared types, the API client, and a
[`components/`](components) directory.

## Files

**`main.tsx`** — entry point. Mounts `<App />` into `#root` and imports
`styles.css`.

**`App.tsx`** — the root component and the only owner of chat state. Renders
the top bar, `<ChatWindow />`, and `<Sidebar />`. `handleSend` currently
appends the user's message plus a fixed placeholder reply; this is where the
retrieval call will go once the pipeline exists.

**`api.ts`** — typed access to the FastAPI `/pb` and `/import` routes. Its interfaces mirror
`api/pb_client/models.py`, so **changing a response model there means changing
this file too**. Throws `ApiError` on failure and accepts an `AbortSignal` so
superseded searches can be cancelled. Also exports the helpers for keying and
warning on a result.

**`types.ts`** — shared UI types (`Role`, `Message`). Types describing API
payloads live in `api.ts` instead, next to the calls that return them.

**`styles.css`** — all styling, hand-written. There is no CSS framework and no
CSS modules; class names are plain and global.

## Subdirectories

- [`components/`](components) — the two panes

## Dependencies

`react` (hooks only). Nothing else at runtime.

## Notes

Fetches use relative URLs (`/pb/...`) so the same code works behind the Vite
proxy in development and same-origin in production.
