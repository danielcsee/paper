# ui/src/components

The two panes of the main layout. Each is a default export in its own file, per
the frontend convention that distinct components get their own `.tsx`.

## `ChatWindow.tsx`

The conversation pane. Renders a landing state with clickable example questions
when there are no messages, and the message list once there are. Owns only its
draft text — the message list is passed in from `App.tsx` and appended to via
`onSend`.

Scrolls to the newest message with a `useEffect` on `messages`. Submitting
trims the draft and ignores it when empty.

## `Sidebar.tsx`

Paper search against `/pb/search`. The larger of the two, and the only
component that talks to the API.

- **Infinite scroll.** An `IntersectionObserver` watches a sentinel element and
  loads the next page when it comes within `SCROLL_MARGIN` (240px) of the
  viewport, rather than a "load more" button.
- **Cancellation.** In-flight requests are aborted through an `AbortSignal`
  when a new search supersedes them, so slow responses cannot overwrite newer
  results.
- **Selection.** Results can be toggled into a `Set` keyed by `resultKey()`.
  The action this feeds — kicking off an ingestion job for the selected
  papers — is not implemented yet.
- **Warnings.** `resultWarning()` labels results that cannot be opened:
  abstract-only papers with no PMCID, and the rarer download-only case with no
  PMID.

## Dependencies

`react` (hooks) and `../api` for the typed search call. No component library —
markup is plain elements with ARIA roles and hand-written CSS from
`../styles.css`.
