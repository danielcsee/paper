# ui/src/components

Each component is a default export in its own file, per the frontend convention
that distinct components get their own `.tsx`.

## `ChatWindow.tsx`

The conversation pane: a landing state with example questions, the message list
once there are messages. Owns only its draft.

## `Sidebar.tsx`

Paper search against `/pb/search`; the only component that writes. Infinite
scroll via `IntersectionObserver`; superseded requests abort via `AbortSignal`;
selection is a `Map` keyed by `resultKey()` holding whole results, since
`/import` needs the objects. `resultWarning()` disables results that cannot be
opened.

## `CorpusView.tsx`

Papers that finished importing, newest first, 20 at a time. Cards open the
paper in a new tab.

## `PaperView.tsx`

One stored paper laid out for reading: title, authors, citation, paragraphs in
order under section rules, then references. Headings come from `chunk_type`;
without it a heading is indistinguishable from a paragraph.

## `PaperTabs.tsx`

The **scrolling** half of the tab bar. Separate from the My Corpus tab on
purpose: that one must stay put, and the only way to guarantee it is for the
overflow to live on a container that excludes it. The right-hand fade appears
only when the strip really has more to the right.

## `PaperCard.tsx`

A preview's contents, shared by search and corpus. Returns a fragment: the
caller supplies the wrapper, since a search result is a selectable `<button>`
and a corpus card an opening one.

## `ImportStatus.tsx`

The last `/import` call's outcome, naming every status so no paper is dropped.

## Dependencies

`react`, `../api`, `../navigation`. No component library.
