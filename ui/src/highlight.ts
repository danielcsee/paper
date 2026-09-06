import type { EntitySpan } from './api'

/** A run of paragraph text, either plain or highlighted. */
export interface Segment {
  text: string
  highlighted: boolean
}

/**
 * Split one paragraph into plain and highlighted runs.
 *
 * Every span is verified against the text before it is drawn: the slice must
 * equal what the server said is there. Postgres counts characters while
 * JavaScript counts UTF-16 units, so one astral character earlier in the
 * paragraph would shift every later span — and a highlight over the wrong
 * words is worse than no highlight. A span that fails, or falls outside the
 * string, is dropped rather than clamped.
 *
 * Overlaps are merged. Two mentions of one entity should not overlap, but a
 * nested annotation would otherwise produce crossing <mark> elements.
 */
export function toSegments(text: string, spans: readonly EntitySpan[]): Segment[] {
  const usable = spans
    .filter(
      (span) =>
        span.start >= 0 &&
        span.length > 0 &&
        span.start + span.length <= text.length &&
        text.slice(span.start, span.start + span.length) === span.text,
    )
    .sort((a, b) => a.start - b.start)

  if (usable.length === 0) return [{ text, highlighted: false }]

  const merged: Array<{ start: number; end: number }> = []
  for (const span of usable) {
    const end = span.start + span.length
    const last = merged[merged.length - 1]
    if (last && span.start <= last.end) last.end = Math.max(last.end, end)
    else merged.push({ start: span.start, end })
  }

  const segments: Segment[] = []
  let cursor = 0
  for (const range of merged) {
    if (range.start > cursor) {
      segments.push({ text: text.slice(cursor, range.start), highlighted: false })
    }
    segments.push({ text: text.slice(range.start, range.end), highlighted: true })
    cursor = range.end
  }
  if (cursor < text.length) segments.push({ text: text.slice(cursor), highlighted: false })
  return segments
}
