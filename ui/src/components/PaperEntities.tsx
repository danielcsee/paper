import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { ApiError, entityLabel, fetchPaperEntities, type PaperEntity } from '../api'

interface Props {
  paperId: number
}

interface TipState {
  entity: PaperEntity
  /** Viewport box of the pill this describes. */
  anchor: { left: number; right: number; top: number; bottom: number }
}

/** Keep the tooltip this far from a pill and from the viewport edge. */
const TIP_GAP = 8

/**
 * The concepts PubTator grounded in one paper, as a column of pills.
 *
 * The tooltip is positioned in script and rendered `position: fixed` rather
 * than as an absolutely-positioned child. The list scrolls, and a child
 * tooltip would be clipped by that scroll container for every pill near the
 * top edge — which is where the most-mentioned entities are.
 */
export default function PaperEntities({ paperId }: Props) {
  const [entities, setEntities] = useState<PaperEntity[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [tip, setTip] = useState<TipState | null>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const tipRef = useRef<HTMLDivElement>(null)
  const panelRef = useRef<HTMLElement>(null)

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(null)
    setTip(null)
    fetchPaperEntities(paperId, controller.signal)
      .then((list) => setEntities(list.entities))
      .catch((err: unknown) => {
        if ((err as Error)?.name === 'AbortError') return
        setError(err instanceof ApiError ? err.message : 'Could not load entities.')
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [paperId])

  // A tooltip pinned to the viewport would otherwise hang in place while the
  // list scrolls out from under it.
  useEffect(() => {
    if (!tip) return
    const dismiss = () => setTip(null)
    const list = listRef.current
    list?.addEventListener('scroll', dismiss, { passive: true })
    window.addEventListener('scroll', dismiss, { passive: true, capture: true })
    window.addEventListener('resize', dismiss)
    return () => {
      list?.removeEventListener('scroll', dismiss)
      window.removeEventListener('scroll', dismiss, { capture: true })
      window.removeEventListener('resize', dismiss)
    }
  }, [tip])

  /**
   * Place the tooltip once it has been measured.
   *
   * Imperative rather than more state: the position depends on the tooltip's
   * own size, so deriving it in render would mean a second render per hover.
   * Above the pill as asked, but flipped below when there is no room: the
   * ceiling is the panel's own top edge, not the viewport's, so a tooltip
   * never lands on top of the app header. The most-mentioned entities sit in
   * that first row, so this is the common case, not the corner case.
   */
  useLayoutEffect(() => {
    const element = tipRef.current
    if (!element || !tip) return
    const box = element.getBoundingClientRect()
    const { anchor } = tip

    const centred = (anchor.left + anchor.right) / 2 - box.width / 2
    const maxLeft = window.innerWidth - box.width - TIP_GAP
    element.style.left = `${Math.max(TIP_GAP, Math.min(centred, maxLeft))}px`

    const ceiling = (panelRef.current?.getBoundingClientRect().top ?? 0) + TIP_GAP
    const above = anchor.top - box.height - TIP_GAP
    element.style.top = above >= ceiling ? `${above}px` : `${anchor.bottom + TIP_GAP}px`
    element.style.visibility = 'visible'
  }, [tip])

  function show(entity: PaperEntity, element: HTMLElement) {
    const box = element.getBoundingClientRect()
    setTip({
      entity,
      anchor: { left: box.left, right: box.right, top: box.top, bottom: box.bottom },
    })
  }

  return (
    <aside className="paper-entities" aria-label="Entities in this paper" ref={panelRef}>
      <h2 className="paper-entities-title">
        Entities{!loading && !error && entities.length > 0 && ` (${entities.length})`}
      </h2>

      {loading && (
        <p className="paper-entities-note" role="status">
          Loading…
        </p>
      )}
      {error && <p className="paper-entities-note">{error}</p>}
      {!loading && !error && entities.length === 0 && (
        <p className="paper-entities-note">No grounded entities in this paper.</p>
      )}

      <div className="paper-entities-list" ref={listRef}>
        {entities.map((entity) => (
          <button
            key={entity.entity_id}
            type="button"
            className="entity-pill"
            title=""
            onMouseEnter={(event) => show(entity, event.currentTarget)}
            onMouseLeave={() => setTip(null)}
            onFocus={(event) => show(entity, event.currentTarget)}
            onBlur={() => setTip(null)}
          >
            {entityLabel(entity)}
          </button>
        ))}
      </div>

      {tip && (
        <div
          className="entity-tip"
          role="tooltip"
          ref={tipRef}
          // Hidden until the layout effect has measured and placed it, so it
          // never flashes at the top-left corner on the way to its position.
          style={{ visibility: 'hidden' }}
        >
          <span className="entity-tip-names">
            {tip.entity.names.length > 0
              ? tip.entity.names.join(', ')
              : tip.entity.name ?? tip.entity.identifier}
          </span>
          <span className="entity-tip-meta">
            {tip.entity.entity_type} · {tip.entity.identifier} · {tip.entity.mention_count}{' '}
            mention{tip.entity.mention_count === 1 ? '' : 's'}
          </span>
        </div>
      )}
    </aside>
  )
}
