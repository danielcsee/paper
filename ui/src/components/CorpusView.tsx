import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, fetchCorpus, type CorpusPaper } from '../api'
import PaperCard from './PaperCard'

/** Distance from the bottom at which the next page starts loading. */
const SCROLL_MARGIN = '320px'

interface Props {
  onClose: () => void
  onOpenPaper: (paperId: number, title: string | null) => void
}

/** Every paper that finished importing, newest first, in one infinite column. */
export default function CorpusView({ onClose, onOpenPaper }: Props) {
  const [papers, setPapers] = useState<CorpusPaper[]>([])
  const [page, setPage] = useState(0)
  const [totalPages, setTotalPages] = useState(0)
  const [totalPapers, setTotalPapers] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const scrollRef = useRef<HTMLDivElement>(null)
  const sentinelRef = useRef<HTMLDivElement>(null)
  const abortRef = useRef<AbortController | null>(null)
  // Read inside the observer callback, which must not re-subscribe per page.
  const stateRef = useRef({ page, totalPages, loading })
  stateRef.current = { page, totalPages, loading }

  const loadPage = useCallback(async (nextPage: number) => {
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller

    setLoading(true)
    setError(null)
    try {
      const response = await fetchCorpus(nextPage, controller.signal)
      setPage(response.page)
      setTotalPages(response.total_pages)
      setTotalPapers(response.total_papers)
      setPapers((prev) => {
        if (nextPage === 1) return response.papers
        // A paper imported while the reader scrolls shifts the offsets, so the
        // same row can arrive twice. Keep the first copy: React keys must hold.
        const seen = new Set(prev.map((paper) => paper.paper_id))
        return [...prev, ...response.papers.filter((p) => !seen.has(p.paper_id))]
      })
    } catch (err) {
      if ((err as Error)?.name === 'AbortError') return
      setError(err instanceof ApiError ? err.message : 'Could not reach the server.')
      if (nextPage === 1) setPapers([])
    } finally {
      if (!controller.signal.aborted) setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadPage(1)
    return () => abortRef.current?.abort()
  }, [loadPage])

  useEffect(() => {
    const sentinel = sentinelRef.current
    const root = scrollRef.current
    if (!sentinel || !root) return

    const observer = new IntersectionObserver(
      ([entry]) => {
        const { page: p, totalPages: tp, loading: busy } = stateRef.current
        if (!entry.isIntersecting || busy || p === 0 || p >= tp) return
        void loadPage(p + 1)
      },
      { root, rootMargin: `0px 0px ${SCROLL_MARGIN} 0px` },
    )
    observer.observe(sentinel)
    return () => observer.disconnect()
  }, [loadPage])

  const exhausted = page > 0 && page >= totalPages
  const empty = !loading && !error && papers.length === 0

  return (
    <section className="corpus" aria-label="My Corpus">
      <header className="corpus-header">
        <div>
          <h1 className="corpus-title">My Corpus</h1>
          {totalPapers > 0 && (
            <p className="corpus-count">
              {totalPapers.toLocaleString()} paper{totalPapers === 1 ? '' : 's'}
            </p>
          )}
        </div>
        <button className="corpus-close" type="button" onClick={onClose} aria-label="Close">
          ✕
        </button>
      </header>

      <div className="corpus-scroll" ref={scrollRef}>
        {error && <p className="results-message results-error">{error}</p>}
        {empty && (
          <p className="results-message">
            Nothing imported yet. Search on the right, select some papers, and press Import.
          </p>
        )}

        <ul className="corpus-list">
          {papers.map((paper) => (
            <li key={paper.paper_id}>
              <button
                type="button"
                className="chip chip-openable"
                onClick={() => onOpenPaper(paper.paper_id, paper.title)}
                title="Open paper"
              >
                <PaperCard
                  title={paper.title}
                  journal={paper.journal}
                  year={paper.pub_year}
                  snippet={paper.snippet}
                  pmid={paper.pmid}
                  pmcid={paper.pmcid}
                  extra={`${paper.chunk_count} chunk${paper.chunk_count === 1 ? '' : 's'}`}
                />
              </button>
            </li>
          ))}
        </ul>

        {/* Below the list, with real height — a zero-area target is unreliable. */}
        <div className="results-sentinel" ref={sentinelRef} aria-hidden="true" />

        {loading && (
          <div className="results-loading" role="status" aria-live="polite">
            <span className="spinner" aria-hidden="true" />
            <span>Loading…</span>
          </div>
        )}
        {exhausted && !loading && papers.length > 0 && (
          <p className="results-message">End of corpus.</p>
        )}
      </div>
    </section>
  )
}
