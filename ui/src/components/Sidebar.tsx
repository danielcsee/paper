import { useCallback, useEffect, useRef, useState } from 'react'
import {
  ApiError,
  importPapers,
  resultKey,
  resultWarning,
  searchPapers,
  type SearchResult,
} from '../api'
import { useImportStatus } from '../useImportStatus'
import ImportStatus from './ImportStatus'
import PaperCard from './PaperCard'

/** Pixels from the bottom at which the next page starts loading. */
const SCROLL_MARGIN = '240px'

export default function Sidebar() {
  const [draft, setDraft] = useState('')
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchResult[]>([])
  const [page, setPage] = useState(0)
  const [totalPages, setTotalPages] = useState(0)
  const [totalResults, setTotalResults] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // Keyed by resultKey, holding the whole result: /import needs the objects,
  // and a chip can scroll out of `results` before the user hits Import.
  const [selected, setSelected] = useState<ReadonlyMap<string, SearchResult>>(new Map())
  const [importing, setImporting] = useState(false)
  const [importCount, setImportCount] = useState(0)
  const [importError, setImportError] = useState<string | null>(null)
  // Owns the tracked rows and the polling loop that keeps them current.
  const importStatus = useImportStatus()

  const scrollRef = useRef<HTMLDivElement>(null)
  const importAbortRef = useRef<AbortController | null>(null)
  const sentinelRef = useRef<HTMLDivElement>(null)
  const abortRef = useRef<AbortController | null>(null)
  // Read inside the observer callback, which must not re-subscribe on every
  // keystroke or page append.
  const stateRef = useRef({ query, page, totalPages, loading })
  stateRef.current = { query, page, totalPages, loading }

  const runSearch = useCallback(async (text: string, nextPage: number) => {
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller

    setLoading(true)
    setError(null)
    try {
      const response = await searchPapers(text, nextPage, controller.signal)
      setPage(response.page)
      setTotalPages(response.total_pages)
      setTotalResults(response.total_results)
      setResults((prev) => {
        if (nextPage === 1) return response.results
        // PubTator can repeat a record across pages; keep the first copy so
        // React keys stay unique.
        const seen = new Set(prev.map(resultKey))
        return [...prev, ...response.results.filter((r) => !seen.has(resultKey(r)))]
      })
    } catch (err) {
      if ((err as Error)?.name === 'AbortError') return
      setError(err instanceof ApiError ? err.message : 'Could not reach the search service.')
      if (nextPage === 1) setResults([])
    } finally {
      if (!controller.signal.aborted) setLoading(false)
    }
  }, [])

  function submit(event: React.FormEvent) {
    event.preventDefault()
    const text = draft.trim()
    if (!text) return
    setQuery(text)
    setResults([])
    setPage(0)
    setTotalPages(0)
    // Selection refers to the results on screen; carrying it across a new
    // search would queue papers the user can no longer see.
    setSelected(new Map())
    setImportError(null)
    scrollRef.current?.scrollTo({ top: 0 })
    void runSearch(text, 1)
  }

  // Infinite scroll: a sentinel below the list pulls the next page into view.
  useEffect(() => {
    const sentinel = sentinelRef.current
    const root = scrollRef.current
    if (!sentinel || !root) return

    const observer = new IntersectionObserver(
      ([entry]) => {
        const { query: q, page: p, totalPages: tp, loading: busy } = stateRef.current
        if (!entry.isIntersecting || busy || !q || p >= tp) return
        void runSearch(q, p + 1)
      },
      { root, rootMargin: `0px 0px ${SCROLL_MARGIN} 0px` },
    )
    observer.observe(sentinel)
    return () => observer.disconnect()
  }, [runSearch])

  // Drop any in-flight request if the panel goes away.
  useEffect(
    () => () => {
      abortRef.current?.abort()
      importAbortRef.current?.abort()
    },
    [],
  )

  function toggle(key: string, result: SearchResult) {
    setSelected((prev) => {
      const next = new Map(prev)
      if (!next.delete(key)) next.set(key, result)
      return next
    })
  }

  async function handleImport() {
    const papers = [...selected.values()]
    if (papers.length === 0) return

    importAbortRef.current?.abort()
    const controller = new AbortController()
    importAbortRef.current = controller

    setImporting(true)
    setImportCount(papers.length)
    setImportError(null)
    try {
      const response = await importPapers(papers, controller.signal)
      // Titles come from the selection: a queued paper has none stored yet.
      importStatus.track(response, papers)
      setSelected(new Map())
    } catch (err) {
      if ((err as Error)?.name === 'AbortError') return
      setImportError(
        err instanceof ApiError ? err.message : 'Could not reach the import service.',
      )
    } finally {
      if (!controller.signal.aborted) setImporting(false)
    }
  }

  const exhausted = query !== '' && page > 0 && page >= totalPages
  const empty = query !== '' && !loading && !error && results.length === 0

  return (
    <aside className="sidebar" aria-label="Paper search">
      <form className="search" onSubmit={submit} role="search">
        <div className="search-field">
          <input
            className="search-input"
            type="search"
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            placeholder="Search PubTator…"
            aria-label="Search papers"
          />
          <button
            className="search-go"
            type="submit"
            disabled={!draft.trim()}
            aria-label="Search"
            title="Search"
          >
            <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true" focusable="false">
              <path
                d="M8 13.5V3.2M8 3.2 3.6 7.6M8 3.2l4.4 4.4"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
          </button>
        </div>
        <button
          className="search-import"
          type="button"
          onClick={handleImport}
          disabled={selected.size === 0 || importing}
          title={
            selected.size === 0
              ? 'Select one or more results to import'
              : `Import ${selected.size} selected`
          }
        >
          Import{selected.size > 0 ? ` (${selected.size})` : ''}
        </button>
      </form>

      <ImportStatus
        pending={importing}
        pendingCount={importCount}
        papers={importStatus.papers}
        polling={importStatus.polling}
        gaveUp={importStatus.gaveUp}
        error={importError}
        onDismiss={() => {
          importStatus.clear()
          setImportError(null)
        }}
      />

      {totalResults > 0 && (
        <p className="search-count">
          {totalResults.toLocaleString()} result{totalResults === 1 ? '' : 's'}
        </p>
      )}

      <div className="results" ref={scrollRef}>
        {error && <p className="results-message results-error">{error}</p>}
        {empty && <p className="results-message">No papers matched that query.</p>}

        <ul className="chips">
          {results.map((result) => {
            const key = resultKey(result)
            const isSelected = selected.has(key)
            // Fetching a full paper needs a PMID to query with and a PMCID for
            // full text to exist at all; without both there is nothing to open.
            const warning = resultWarning(result)
            const disabled = warning !== null
            return (
              <li key={key}>
                {warning && <p className="chip-warning">{warning}</p>}
                <button
                  type="button"
                  className={`chip${isSelected ? ' chip-selected' : ''}`}
                  aria-pressed={isSelected}
                  disabled={disabled}
                  onClick={() => toggle(key, result)}
                >
                  <PaperCard
                    title={result.title}
                    journal={result.journal}
                    year={result.date?.slice(0, 4)}
                    snippet={result.snippet}
                    pmid={result.pmid}
                    pmcid={result.pmcid}
                  />
                </button>
              </li>
            )
          })}
        </ul>

        {/* Must sit below the list for the observer to mean "reached the end",
            and needs real height — a zero-area target is unreliable to observe. */}
        <div className="results-sentinel" ref={sentinelRef} aria-hidden="true" />

        {loading && (
          <div className="results-loading" role="status" aria-live="polite">
            <span className="spinner" aria-hidden="true" />
            <span>Loading…</span>
          </div>
        )}
        {exhausted && !loading && results.length > 0 && (
          <p className="results-message">End of results.</p>
        )}
      </div>
    </aside>
  )
}
