import { useEffect, useState } from 'react'
import {
  ApiError,
  fetchReferences,
  resultKey,
  type ImportPmids,
  type ReferenceList,
  type SearchResult,
} from '../api'
import PaperCard from './PaperCard'

/**
 * Per-session cache, at module scope on purpose.
 *
 * This component unmounts whenever the panel closes, so a ref-held cache would
 * be thrown away every time — and the lookup it protects is the heaviest call
 * in the app.
 */
const CACHE = new Map<number, ReferenceList>()

interface Props {
  paperId: number
  paperTitle: string
  onImport: (pmids: ImportPmids[], papers: SearchResult[]) => void
  importing: boolean
  onClose: () => void
}

/**
 * The importable references of one paper.
 *
 * Every row is guaranteed to exist as a full Paper in PubTator — the backend
 * has already discarded the rest — so both counts on the buttons are exact
 * rather than optimistic.
 */
export default function ReferenceImporter({
  paperId,
  paperTitle,
  onImport,
  importing,
  onClose,
}: Props) {
  const [data, setData] = useState<ReferenceList | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())

  useEffect(() => {
    const cached = CACHE.get(paperId)
    if (cached) {
      setData(cached)
      setLoading(false)
      setError(null)
      setSelected(new Set())
      return
    }

    const controller = new AbortController()
    setLoading(true)
    setError(null)
    setSelected(new Set())
    fetchReferences(paperId, controller.signal)
      .then((response) => {
        CACHE.set(paperId, response)
        setData(response)
      })
      .catch((err: unknown) => {
        if ((err as Error)?.name === 'AbortError') return
        setError(err instanceof ApiError ? err.message : 'Could not reach the server.')
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [paperId])

  const references = data?.references ?? []

  function toggle(key: string) {
    setSelected((prev) => {
      const next = new Set(prev)
      if (!next.delete(key)) next.add(key)
      return next
    })
  }

  function queue(papers: SearchResult[]) {
    if (papers.length === 0) return
    onImport(
      papers
        .filter((paper) => paper.pmid != null)
        .map((paper) => ({ pmid: paper.pmid as number, includeReferences: false })),
      papers,
    )
    setSelected(new Set())
  }

  return (
    <>
      <div className="refs-head">
        <div className="refs-actions">
          <button
            className="search-import"
            type="button"
            disabled={selected.size === 0 || importing || loading}
            onClick={() => queue(references.filter((r) => selected.has(resultKey(r))))}
          >
            Import Selected ({selected.size})
          </button>
          <button
            className="search-import refs-import-all"
            type="button"
            disabled={importing || loading || references.length === 0}
            onClick={() => queue(references)}
          >
            Import All ({references.length})
          </button>
        </div>
        <button className="refs-close" type="button" onClick={onClose} aria-label="Back to search">
          ✕
        </button>
      </div>

      <p className="refs-context" title={paperTitle}>
        References of <span className="refs-context-title">{paperTitle}</span>
      </p>

      <div className="results">
        {loading && (
          <div className="results-loading" role="status" aria-live="polite">
            <span className="spinner" aria-hidden="true" />
            <span>Checking references against PubTator…</span>
          </div>
        )}
        {error && <p className="results-message results-error">{error}</p>}

        {!loading && !error && data && (
          <p className="search-count">
            {references.length} importable of {data.with_pmid} with a PMID
            {data.total_references !== data.with_pmid
              ? ` · ${data.total_references} references in total`
              : ''}
            {data.truncated ? ' · list truncated' : ''}
          </p>
        )}

        {!loading && !error && references.length === 0 && data && (
          <p className="results-message">
            None of this paper's references exist as full papers in PubTator, so
            there is nothing to import.
          </p>
        )}

        <ul className="chips">
          {references.map((reference) => {
            const key = resultKey(reference)
            const isSelected = selected.has(key)
            return (
              <li key={key}>
                <button
                  type="button"
                  className={`chip${isSelected ? ' chip-selected' : ''}`}
                  aria-pressed={isSelected}
                  onClick={() => toggle(key)}
                >
                  <PaperCard
                    title={reference.title}
                    journal={reference.journal}
                    year={reference.date?.slice(0, 4)}
                    snippet={reference.snippet}
                    pmid={reference.pmid}
                    pmcid={reference.pmcid}
                  />
                </button>
              </li>
            )
          })}
        </ul>
      </div>
    </>
  )
}
