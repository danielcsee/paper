# ui/src/components

Each component is a default export in its own file, per the frontend convention
that distinct components get their own `.tsx`.

## `ChatWindow.tsx`

The conversation pane: a landing state with clickable example questions when
there are no messages, the message list once there are. Owns only its draft
text; messages come from `App.tsx` via `onSend`.

## `Sidebar.tsx`

Paper search against `/pb/search`; the only component that calls the API.

- **Infinite scroll.** An `IntersectionObserver` watches a sentinel element and
  loads the next page when it comes within `SCROLL_MARGIN` (240px), rather than
  a "load more" button.
- **Cancellation.** In-flight requests abort via `AbortSignal` when superseded,
  so slow responses cannot overwrite newer results.
- **Selection.** Results toggle into a `Map` keyed by `resultKey()`, holding
  the whole result: `/import` needs the objects, and a chip can scroll out of
  view first. Cleared on a new search.
- **Import.** Enabled once something is selected; posts the selection to
  `/import` and clears it on success.
- **Warnings.** `resultWarning()` labels and disables results that cannot be
  opened: abstract-only papers with no PMCID, and the rarer no-PMID case.

## `ImportStatus.tsx`

The outcome of the last `/import` call: a spinner while queuing, a summary
naming every status (`queued · already running · already imported · rejected`)
so no paper is silently dropped, or a dismissible error. Purely presentational
— `Sidebar` owns the state.

## Dependencies

`react` (hooks) and `../api`. No component library — plain elements with ARIA
roles and hand-written CSS from `../styles.css`.
