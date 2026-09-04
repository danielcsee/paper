# ui/src

Application source. The layout is flat: an entry point, a root component,
shared types, navigation, the API client, and [`components/`](components).

## Files

**`main.tsx`** — mounts `<App />` into `#root` and imports `styles.css`.

**`App.tsx`** — owns chat state, the open paper tabs and the visit stack.
Closing a paper tab pops that stack, skipping entries whose tab has since
closed: that is how "go back to where I was" works. Renders the top bar and one
of chat / corpus / paper beside a permanently mounted `<Sidebar />`.
`handleSend` still appends a placeholder reply; the retrieval call goes there.

**`navigation.ts`** — `View`, the tab model, title truncation and the view↔URL
mapping. The corpus UI route is `/corpus-view`: `/corpus` is an API path.

**`api.ts`** — typed access to the `/pb`, `/import` and `/corpus` routes. Its
interfaces mirror the backend response models, so **changing one there means
changing this file too**. Throws `ApiError`, accepts an `AbortSignal`, and
exports the helpers for keying and warning on a result.

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
