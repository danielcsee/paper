# ui/src/components

Each component is a default export in its own file, per the frontend convention
that distinct components get their own `.tsx`.

## `ChatWindow.tsx`

The conversation pane. The landing block outlives the first question: held
mounted with an exiting class so it slides up and fades *before* the answer
appears, rather than vanishing the instant state changes.

## `Sidebar.tsx`

Paper search against `/pb/search`. Infinite scroll via `IntersectionObserver`;
superseded requests abort via `AbortSignal`.

## `CorpusView.tsx`

Papers that finished importing, newest first, 20 at a time. A card opens its
paper; the corner icon opens one in the background.

## `PaperView.tsx`

One stored paper laid out for reading: title, authors, citation, paragraphs
under section rules, then references. Headings come from `chunk_type`.

## `PaperTabs.tsx`

The **scrolling** half of the tab bar. Separate from the My Corpus tab so that
one stays put; the fade appears only when the strip has more to the right.

## `PaperCard.tsx`

A preview's contents, shared by search and corpus. Returns a fragment: the
caller supplies the wrapper.

## `RagResults.tsx`

The papers behind an answer. The backend is retrieval only, so the answer *is*
the ranked evidence: each card carries the excerpt that justifies its rank.

## `OpenInTabButton.tsx`

Opens a paper in a background tab, leaving the reader where they are. A
**sibling** of the card, never a child — the card is itself a `<button>`, and
nesting one inside another is invalid HTML.

## `ImportStatus.tsx`

The last `/import` call's outcome, naming every status.

## Dependencies

`react`, `../api`, `../navigation`. No component library.
