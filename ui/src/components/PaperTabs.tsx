import { useCallback, useEffect, useRef, useState } from 'react'
import { truncateTitle, type PaperTab, type View } from '../navigation'

interface Props {
  tabs: PaperTab[]
  active: View
  onSelect: (paperId: number) => void
  onClose: (paperId: number) => void
}

/**
 * The scrolling half of the tab bar.
 *
 * Separate from the My Corpus tab on purpose: that one must stay put while
 * these scroll, and the only way to guarantee that is for the overflow to live
 * on a container that does not include it.
 */
export default function PaperTabs({ tabs, active, onSelect, onClose }: Props) {
  const stripRef = useRef<HTMLDivElement>(null)
  const [overflowing, setOverflowing] = useState(false)
  const activeId = active.kind === 'paper' ? active.paperId : null

  // The fade means "there is more to the right", so it must appear only when
  // that is true — not whenever the strip happens to be narrower than its slot.
  const measure = useCallback(() => {
    const strip = stripRef.current
    if (!strip) return
    const remaining = strip.scrollWidth - strip.clientWidth - strip.scrollLeft
    setOverflowing(remaining > 1)
  }, [])

  useEffect(() => {
    measure()
    const strip = stripRef.current
    if (!strip) return
    const observer = new ResizeObserver(measure)
    observer.observe(strip)
    strip.addEventListener('scroll', measure, { passive: true })
    return () => {
      observer.disconnect()
      strip.removeEventListener('scroll', measure)
    }
  }, [measure, tabs.length])

  // A newly opened tab is appended off-screen once the strip overflows; bring
  // the selected one into view rather than making the user hunt for it.
  useEffect(() => {
    if (activeId === null) return
    stripRef.current
      ?.querySelector(`[data-paper-id="${activeId}"]`)
      ?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
  }, [activeId, tabs.length])

  if (tabs.length === 0) return null

  return (
    <div className="paper-tabs">
      <div className="paper-tabs-strip" ref={stripRef} role="tablist" aria-label="Open papers">
        {tabs.map((tab) => {
          const selected = tab.paperId === activeId
          return (
            <span
              key={tab.paperId}
              className={`paper-tab${selected ? ' paper-tab-active' : ''}`}
              data-paper-id={tab.paperId}
            >
              <button
                type="button"
                role="tab"
                aria-selected={selected}
                className="paper-tab-label"
                title={tab.title}
                onClick={() => onSelect(tab.paperId)}
              >
                {truncateTitle(tab.title)}
              </button>
              <button
                type="button"
                className="paper-tab-close"
                aria-label={`Close ${tab.title}`}
                onClick={() => onClose(tab.paperId)}
              >
                ✕
              </button>
            </span>
          )
        })}
      </div>
      {/* Signals that there is more to the right once the strip overflows. */}
      {overflowing && <span className="paper-tabs-fade" aria-hidden="true" />}
    </div>
  )
}
