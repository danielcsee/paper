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
      return '/corpus-view'
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
  return path === '/corpus-view' ? CORPUS : CHAT
}
