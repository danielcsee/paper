import { useEffect, useRef, useState } from 'react'
import { ApiError, fetchPaper, isHeading, type PaperDetail } from '../api'

interface Props {
  paperId: number
  /** Lets the tab title update once the full title arrives. */
  onLoaded?: (paper: PaperDetail) => void
  /** The paper is gone (404), so its tab should not outlive this session. */
  onMissing?: (paperId: number) => void
}

/** Human labels for PubTator's section codes, for the section rules. */
const SECTION_LABELS: Record<string, string> = {
  ABSTRACT: 'Abstract',
  INTRO: 'Introduction',
  METHODS: 'Methods',
  RESULTS: 'Results',
  DISCUSS: 'Discussion',
  CONCL: 'Conclusion',
  FIG: 'Figures',
  TABLE: 'Tables',
  SUPPL: 'Supplementary',
  APPENDIX: 'Appendix',
  CASE: 'Case',
  ABBR: 'Abbreviations',
  AUTH_CONT: 'Author contributions',
  COMP_INT: 'Competing interests',
  ACK_FUND: 'Acknowledgements',
  KEYWORD: 'Keywords',
}

function formatReference(reference: {
  source: string | null
  year: string | null
  volume: string | null
  fpage: string | null
  lpage: string | null
}): string {
  const pages = [reference.fpage, reference.lpage].filter(Boolean).join('–')
  return [reference.source, reference.year, reference.volume, pages]
    .filter(Boolean)
    .join(' · ')
}

/** One stored paper, laid out for reading. */
export default function PaperView({ paperId, onLoaded, onMissing }: Props) {
  const [paper, setPaper] = useState<PaperDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(null)
    fetchPaper(paperId, controller.signal)
      .then((loaded) => {
        setPaper(loaded)
        onLoaded?.(loaded)
      })
      .catch((err: unknown) => {
        if ((err as Error)?.name === 'AbortError') return
        setError(err instanceof ApiError ? err.message : 'Could not reach the server.')
        // 404 means the paper left the corpus. Keep the tab for this session so
        // the reader sees why, but tell App to stop persisting it.
        if (err instanceof ApiError && err.status === 404) onMissing?.(paperId)
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
    // onLoaded is a fresh closure each render; re-fetching on it would loop.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [paperId])

  // A different paper starts at its own top, not where the last one was left.
  useEffect(() => {
    scrollRef.current?.scrollTo({ top: 0 })
  }, [paperId])

  if (loading) {
    return (
      <section className="paper" aria-busy="true">
        <div className="paper-scroll">
          <div className="results-loading" role="status">
            <span className="spinner" aria-hidden="true" />
            <span>Loading paper…</span>
          </div>
        </div>
      </section>
    )
  }

  if (error || !paper) {
    return (
      <section className="paper">
        <div className="paper-scroll">
          <p className="results-message results-error">{error ?? 'Paper unavailable.'}</p>
        </div>
      </section>
    )
  }

  const citation = [
    paper.journal_title ?? paper.journal,
    paper.pub_year,
    paper.volume && `vol. ${paper.volume}`,
    [paper.fpage, paper.lpage].filter(Boolean).join('–'),
  ]
    .filter(Boolean)
    .join(' · ')

  let lastSection: string | null = null

  return (
    <section className="paper" aria-label={paper.title ?? 'Paper'}>
      <div className="paper-scroll" ref={scrollRef}>
        <article className="paper-doc">
          <header className="paper-doc-header">
            <h1 className="paper-doc-title">{paper.title ?? 'Untitled'}</h1>
            {paper.authors.length > 0 && (
              <p className="paper-doc-authors">{paper.authors.join(', ')}</p>
            )}
            {citation && <p className="paper-doc-citation">{citation}</p>}
            <p className="paper-doc-ids">
              {[
                `PMID ${paper.pmid}`,
                paper.pmcid,
                paper.doi ? `doi:${paper.doi}` : null,
                paper.has_full_text ? null : 'abstract only',
              ]
                .filter(Boolean)
                .join(' · ')}
            </p>
          </header>

          {paper.paragraphs.map((paragraph) => {
            // A rule whenever the section changes, so the document reads as
            // sections rather than an undifferentiated wall of paragraphs.
            const startsSection =
              paragraph.section_type !== null && paragraph.section_type !== lastSection
            const label = startsSection
              ? SECTION_LABELS[paragraph.section_type as string] ?? paragraph.section_type
              : null
            lastSection = paragraph.section_type ?? lastSection

            return (
              <div key={paragraph.ordinal}>
                {label && <h2 className="paper-doc-section">{label}</h2>}
                {isHeading(paragraph) ? (
                  <h3 className="paper-doc-heading">{paragraph.text}</h3>
                ) : (
                  <p className="paper-doc-para">{paragraph.text}</p>
                )}
              </div>
            )
          })}

          {paper.references.length > 0 && (
            <section className="paper-doc-refs">
              <h2 className="paper-doc-section">References</h2>
              <ol className="paper-doc-reflist">
                {paper.references.map((reference) => (
                  <li key={reference.ordinal}>
                    <span className="paper-ref-title">{reference.title ?? 'Untitled'}</span>
                    <span className="paper-ref-meta">
                      {[
                        formatReference(reference),
                        reference.pmid ? `PMID ${reference.pmid}` : null,
                        reference.doi ? `doi:${reference.doi}` : null,
                      ]
                        .filter(Boolean)
                        .join(' · ')}
                    </span>
                  </li>
                ))}
              </ol>
            </section>
          )}
        </article>
      </div>
    </section>
  )
}
