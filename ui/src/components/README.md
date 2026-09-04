# ui/src/components

Each component is a default export in its own file, per the frontend convention
that distinct components get their own `.tsx`.

## `ChatWindow.tsx`

The conversation pane: a landing state with example questions when there are no
messages, the message list once there are. Owns only its draft.

## `Sidebar.tsx`

Paper search against `/pb/search`; the only component that writes.

- **Infinite scroll.** An `IntersectionObserver` on a sentinel loads the next
  page within `SCROLL_MARGIN`.
- **Cancellation.** Superseded requests abort via `AbortSignal`, so a slow
  response cannot overwrite newer results.
- **Selection.** A `Map` keyed by `resultKey()` holding whole results —
  `/import` needs the objects, and a chip can scroll out of view first. Cleared
  on a new search.
- **Warnings.** `resultWarning()` disables results that cannot be opened: no
  PMCID (abstract-only), or the rarer no-PMID case.
- **Import.** Posts the selection to `/import`, clearing it on success.

## `CorpusView.tsx`

Papers that finished importing, newest first, in one infinite column of 20. Its
scroll container doubles as the observer root.

## `PaperCard.tsx`

A paper preview's contents, shared by search results and the corpus so the two
read identically. Returns a fragment: the caller supplies the wrapper, since a
search result is a selectable `<button>` and a corpus entry a static
`<article>`.

## `ImportStatus.tsx`

The last `/import` call's outcome — a spinner, a summary naming every status
so no paper is dropped, or a dismissible error.

## Dependencies

`react` and `../api`. No component library: plain elements with ARIA roles and
hand-written CSS from `../styles.css`.
