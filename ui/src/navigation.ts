/**
 * Which view is on screen, which paper tabs are open, and how to get back.
 *
 * Closing a paper tab must return the user to whatever they were looking at
 * *before* that paper, which a single "current view" cannot answer. So this
 * keeps a visit stack and pops it, skipping entries whose tab has since
 * closed. Views also map to URLs, so a reload or a shared link lands somewhere
 * sensible.
 */

export type View =
  | { kind: 'chat' }
  | { kind: 'corpus' }
  | { kind: 'paper'; paperId: number }

export interface PaperTab {
  paperId: number
  title: string
}

export const CHAT: View = { kind: 'chat' }
export const CORPUS: View = { kind: 'corpus' }

/** Tab labels are truncated to this many characters, then an ellipsis. */
export const TAB_TITLE_MAX = 20

export function sameView(a: View, b: View): boolean {
  if (a.kind !== b.kind) return false
  return a.kind !== 'paper' || a.paperId === (b as { paperId: number }).paperId
}

export function viewKey(view: View): string {
  return view.kind === 'paper' ? `paper:${view.paperId}` : view.kind
}

export function truncateTitle(title: string | null, max = TAB_TITLE_MAX): string {
  const text = (title ?? 'Untitled').trim()
  return text.length <= max ? text : `${text.slice(0, max).trimEnd()}…`
}

export function viewToPath(view: View): string {
  switch (view.kind) {
    case 'corpus':
      return '/my-corpus'
    case 'paper':
      return `/paper/${view.paperId}`
    default:
      return '/'
  }
}

export function pathToView(path: string): View {
  if (path.startsWith('/paper/')) {
    const id = Number(path.slice('/paper/'.length))
    if (Number.isInteger(id) && id > 0) return { kind: 'paper', paperId: id }
  }
  return path === '/my-corpus' ? CORPUS : CHAT
}


// --- persistence -----------------------------------------------------------
//
// Open tabs survive a reload. The active view does not need storing: the URL
// already carries it, and it stays the authority so a shared link still wins.

const STORAGE_KEY = 'litgraph.openTabs.v1'

/** Enough for any real session, and a bound on what a corrupt write can grow to. */
const MAX_STORED_TABS = 50
const MAX_STORED_TITLE = 300

function isPaperTab(value: unknown): value is PaperTab {
  if (typeof value !== 'object' || value === null) return false
  const tab = value as Record<string, unknown>
  return (
    typeof tab.paperId === 'number' &&
    Number.isInteger(tab.paperId) &&
    tab.paperId > 0 &&
    typeof tab.title === 'string'
  )
}

/**
 * Read the stored tabs, or an empty list.
 *
 * Every failure is non-fatal. `localStorage` throws outright in some contexts
 * (private windows, blocked site data), and the stored value may predate a
 * change to this shape or have been edited by hand — none of which should stop
 * the app from starting.
 */
export function loadTabs(): PaperTab[] {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY)
    if (!raw) return []
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    const seen = new Set<number>()
    const tabs: PaperTab[] = []
    for (const value of parsed) {
      if (!isPaperTab(value) || seen.has(value.paperId)) continue
      seen.add(value.paperId)
      tabs.push({ paperId: value.paperId, title: value.title.slice(0, MAX_STORED_TITLE) })
      if (tabs.length === MAX_STORED_TABS) break
    }
    return tabs
  } catch {
    return []
  }
}

export function saveTabs(tabs: PaperTab[]): void {
  try {
    window.localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify(
        tabs.slice(0, MAX_STORED_TABS).map((tab) => ({
          paperId: tab.paperId,
          title: tab.title.slice(0, MAX_STORED_TITLE),
        })),
      ),
    )
  } catch {
    // A full or unavailable store costs the user their tab list on reload,
    // which is not worth breaking the session over.
  }
}
