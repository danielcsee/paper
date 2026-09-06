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

One paper laid out for reading, with `PaperEntities` as its left column.
Headings come from `chunk_type`. The orange "view references" link opens that
paper's references in the panel.

## `PaperEntities.tsx`

The concepts PubTator grounded in this paper, as oval pills, most-mentioned
first. Hovering one shows the wordings the paper itself used, above the pill.

Labels prefer `entities.name`, falling back to the commonest surface form when
that name is really the identifier. PubTator names no Species, so taxon 9685
arrives called "9685"; `api.eu_client` resolves it to "domestic cat", and the
fallback covers what E-utilities cannot name (Cellosaurus, OMIM, merged taxa).

Clicking a pill highlights every occurrence of that entity in the text and
scrolls the first into view; clicking it again, or Escape, clears it. The
highlight is `--highlight` (highlighter yellow), deliberately not the orange
accent — an orange highlight beside orange-accented controls reads as another
control rather than as marked text.

The tooltip is `position: fixed` and placed in a layout effect, not an
absolutely-positioned child: the list scrolls, so a child would be clipped for
every pill near the top edge, which is where the most-mentioned entities are.
It flips below the pill when there is no room above, measuring against the
panel's top rather than the viewport's so it never covers the app header.

## Highlighting

`../highlight.ts` turns a paragraph and a list of spans into plain and marked
runs. Every span is checked against the text before it is drawn — the slice
must equal what the server said is there — and dropped otherwise. Postgres
counts characters where JavaScript counts UTF-16 units, so one astral character
earlier in a paragraph would shift every later span, and a highlight over the
wrong words is worse than none. Overlaps are merged so `<mark>` elements cannot
cross.

Scrolling to the first match is instant, not smooth: the first mention can be
thousands of pixels away, and `behavior: 'smooth'` measured as a no-op in the
test browser, so it would have silently done nothing.

## `PaperTabs.tsx`

The **scrolling** half of the tab bar, separate so the My Corpus tab stays put.

## `PaperCard.tsx`

A preview's contents, shared by search, corpus, answers and references.

## `ImportStatus.tsx`

One row per paper: a coloured dot, the title and its state.

A new import replaces the panel rather than adding to it, so finished rows from
an earlier batch do not linger. Papers still in flight are kept — dropping those
would hide running work — and they clear themselves once they finish. The rule
lives in `nextTrackedPapers`, split out of the hook so it can be tested alone.

## `OpenInTabButton.tsx`

Opens a paper in a background tab. A **sibling** of the card, never a child.
