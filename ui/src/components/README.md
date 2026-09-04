# ui/src/components

Each component is a default export in its own file, per the frontend convention
that distinct components get their own `.tsx`.

## `ChatWindow.tsx`

The conversation pane: a landing state with example questions, then the message
list. Owns only its draft.

## `Sidebar.tsx`

Paper search against `/pb/search`; the only component that writes. Infinite
scroll via `IntersectionObserver`; superseded requests abort via `AbortSignal`;
selection is a `Map` keyed by `resultKey()` holding whole results, since
`/import` needs the objects.

## `CorpusView.tsx`

Papers that finished importing, newest first, 20 at a time. A card opens its
paper; the corner icon opens one in the background.

## `PaperView.tsx`

One stored paper laid out for reading: title, authors, citation, paragraphs
under section rules, then references. Headings come from `chunk_type`; without
it a heading is indistinguishable from a paragraph.

## `PaperTabs.tsx`

The **scrolling** half of the tab bar. Separate from the My Corpus tab on
purpose: that one must stay put, so the overflow lives on a container that
excludes it. The fade appears only when the strip really has more to the
right.

## `PaperCard.tsx`

A preview's contents, shared by search and corpus. Returns a fragment: the
caller supplies the wrapper.

## `OpenInTabButton.tsx`

Opens a paper in a background tab, leaving the reader where they are. A
**sibling** of the card, never a child — the card is itself a `<button>`, and
nesting one inside another is invalid HTML.

## `ImportStatus.tsx`

The last `/import` call's outcome, naming every status.

## Dependencies

`react`, `../api`, `../navigation`. No component library.
