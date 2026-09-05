# ui/src/components

Each component is a default export in its own file, per the frontend convention
that distinct components get their own `.tsx`.

## `ChatWindow.tsx`

The conversation pane. The landing block is held mounted with an exiting class
so it slides up and fades *before* the answer appears.

## `PaperExplorer.tsx`

The right-hand panel. Owns import state so `SearchPubTator` and
`ReferenceImporter` feed one shared `ImportStatus`. Search stays **mounted but
hidden** while references are shown, so its query, scroll and selection survive.

## `SearchPubTator.tsx`

Search against `/pb/search`. Infinite scroll via `IntersectionObserver`;
superseded requests abort via `AbortSignal`. `resultWarning()` disables results
that cannot be opened.

## `ReferenceImporter.tsx`

One paper's importable references, with Import Selected and Import All. Every
row is importable, so both counts are exact. Results are cached at **module
scope**: the component unmounts on close, so a ref-held cache would throw away
the heaviest call in the app.

## `CorpusView.tsx`

Imported papers, newest first, 20 at a time. A card opens its paper; the
corner icon opens one in the background.

## `PaperView.tsx`

One paper laid out for reading. Headings come from `chunk_type`. The orange
"view references" link opens that paper's references in the panel.

## `PaperTabs.tsx`

The **scrolling** half of the tab bar, separate so the My Corpus tab stays put.

## `PaperCard.tsx`

A preview's contents, shared by search, corpus, answers and references.

## `ImportStatus.tsx`

One row per paper: a coloured dot, the title and its state.

## `OpenInTabButton.tsx`

Opens a paper in a background tab. A **sibling** of the card, never a child.
