import type { RagPaper } from '../api'
import PaperCard from './PaperCard'

interface Props {
  papers: RagPaper[]
  chunksConsidered: number
  onOpenPaper: (paperId: number, title: string | null) => void
}

/**
 * The papers behind an answer.
 *
 * There is no generated prose: the backend is retrieval only, so the "answer"
 * is the ranked evidence itself. Each card shows the strongest excerpt, which
 * is the part that actually justifies the ranking.
 */
export default function RagResults({ papers, chunksConsidered, onOpenPaper }: Props) {
  if (papers.length === 0) {
    return (
      <p className="rag-empty">
        Nothing in your corpus passed the relevance threshold. Import more
        papers, or try different wording.
      </p>
    )
  }

  return (
    <div className="rag">
      <p className="rag-lead">
        {papers.length} paper{papers.length === 1 ? '' : 's'} from{' '}
        {chunksConsidered} matching passage{chunksConsidered === 1 ? '' : 's'}:
      </p>
      <ul className="rag-list">
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
                pmid={paper.pmid}
                pmcid={paper.pmcid}
                extra={`${paper.matched_chunks} passage${
                  paper.matched_chunks === 1 ? '' : 's'
                } · best ${paper.best_score.toFixed(2)}`}
              />
            </button>
            {paper.chunks.length > 0 && (
              <blockquote className="rag-quote">
                {paper.chunks[0].section_type && (
                  <span className="rag-quote-section">{paper.chunks[0].section_type}</span>
                )}
                {paper.chunks[0].text}
              </blockquote>
            )}
          </li>
        ))}
      </ul>
    </div>
  )
}
