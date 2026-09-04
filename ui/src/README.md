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

**`navigation.ts`** — `View`, the tab model, title truncation, the view↔URL
mapping, and `loadTabs`/`saveTabs`. The corpus UI route is `/corpus-view`,
because `/corpus` is an API path.

Open tabs persist to `localStorage`; the active view does not, since the URL
carries it and should win for a shared link. Reads are validated and every
access guarded — the store throws outright in a private window, and its
contents may predate this shape.

**`api.ts`** — typed access to the `/pb`, `/import` and `/corpus` routes. Its
interfaces mirror the backend response models, so **changing one there means
changing this file too**. Throws `ApiError`, which carries the HTTP status so
callers can tell "gone" from "broken", and accepts an `AbortSignal`.

**`types.ts`** — shared UI types (`Role`, `Message`). API payload types live in
`api.ts`, next to the calls that return them.

**`styles.css`** — all styling, hand-written. No framework, no CSS modules;
class names are plain and global.
