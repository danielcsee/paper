# ui/src/components

Each component is a default export in its own file, per the frontend convention
that distinct components get their own `.tsx`.

## `ChatWindow.tsx`

The conversation pane. The landing block outlives the first question: held
mounted with an exiting class so it slides up and fades *before* the answer
appears.

## `Sidebar.tsx`

Paper search against `/pb/search`. Infinite scroll via `IntersectionObserver`;
superseded requests abort via `AbortSignal`. Selection is a `Map` keyed by
`resultKey()` holding whole results, since `/import` needs the objects.
`resultWarning()` disables results that cannot be opened: no PMCID
(abstract-only), or the rarer no-PMID case.

## `CorpusView.tsx`

Imported papers, newest first, 20 at a time. A card opens its paper; the
corner icon opens one in the background.

## `PaperView.tsx`

One paper laid out for reading: title, authors, citation, paragraphs under
section rules, references. Headings come from `chunk_type`.

## `PaperTabs.tsx`

The **scrolling** half of the tab bar, separate so the My Corpus tab stays
put. The fade appears only when the strip has more to the right.

## `PaperCard.tsx`

A preview's contents, shared by search, corpus and answers. Returns a
fragment; the caller supplies the wrapper.

## `RagResults.tsx`

The papers behind an answer — retrieval only, so the answer *is* the evidence.

## `ImportStatus.tsx`

One row per paper: a coloured dot (white queued, yellow started, red error,
green success), the title and its state. Titles come from the selection.
Presentational; `useImportStatus` polls.

## `OpenInTabButton.tsx`

Opens a paper in a background tab. A **sibling** of the card, never a child:
nesting a button inside a button is invalid HTML.
