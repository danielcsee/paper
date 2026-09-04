interface Props {
  title: string | null
  journal?: string | null
  year?: string | number | null
  snippet?: string | null
  pmid?: number | null
  pmcid?: string | null
  /** Extra detail for the id line, e.g. "48 chunks". */
  extra?: string | null
}

/**
 * The contents of a paper preview, shared by search results and the corpus so
 * the two read identically. Returns a fragment rather than an element: the
 * caller supplies the wrapper, because a search result is a selectable
 * `<button>` while a corpus entry is static.
 */
export default function PaperCard({
  title,
  journal,
  year,
  snippet,
  pmid,
  pmcid,
  extra,
}: Props) {
  const meta = [journal, year].filter(Boolean).join(' · ')
  const ids = [pmid != null ? `PMID ${pmid}` : null, pmcid, extra]
    .filter(Boolean)
    .join(' · ')

  return (
    <>
      <span className="chip-title">{title ?? 'Untitled'}</span>
      {meta && <span className="chip-meta">{meta}</span>}
      {snippet && <span className="chip-snippet">{snippet}</span>}
      {ids && <span className="chip-ids">{ids}</span>}
    </>
  )
}
