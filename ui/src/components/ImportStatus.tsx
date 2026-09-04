import type { PaperState } from '../api'
import type { TrackedPaper } from '../useImportStatus'

interface Props {
  pending: boolean
  /** Number of papers in the request that is in flight. */
  pendingCount: number
  papers: TrackedPaper[]
  polling: boolean
  /** Polling stopped with work still unfinished. */
  gaveUp: boolean
  error: string | null
  onDismiss: () => void
}

const STATE_LABEL: Record<PaperState, string> = {
  queued: 'Queued',
  started: 'Importing',
  success: 'Imported',
  error: 'Failed',
}

/** Live progress for the most recent import, one row per paper. */
export default function ImportStatus({
  pending,
  pendingCount,
  papers,
  polling,
  gaveUp,
  error,
  onDismiss,
}: Props) {
  if (pending) {
    return (
      <div className="import-status" role="status" aria-live="polite">
        <span className="spinner" aria-hidden="true" />
        <span>
          Queuing {pendingCount} paper{pendingCount === 1 ? '' : 's'}…
        </span>
      </div>
    )
  }

  if (error) {
    return (
      <div className="import-status import-status-error" role="alert">
        <span>{error}</span>
        <button className="import-dismiss" type="button" onClick={onDismiss}>
          Dismiss
        </button>
      </div>
    )
  }

  if (papers.length === 0) return null

  const done = papers.filter((paper) => paper.state === 'success').length
  const failed = papers.filter((paper) => paper.state === 'error').length

  return (
    <div className="import-status" role="status" aria-live="polite">
      <div className="import-summary">
        <span>
          {done}/{papers.length} imported
          {failed > 0 ? ` · ${failed} failed` : ''}
          {polling ? ' · working…' : ''}
        </span>
        <button className="import-dismiss" type="button" onClick={onDismiss}>
          Dismiss
        </button>
      </div>

      <ul className="import-list">
        {papers.map((paper, index) => (
          <li key={paper.pmid ?? `rejected-${index}`} className="import-row">
            <span
              className={`import-dot import-dot-${paper.state}`}
              aria-hidden="true"
            />
            <span className="import-row-text">
              <span className="import-row-title" title={paper.title}>
                {paper.title}
              </span>
              <span className="import-row-state">
                {STATE_LABEL[paper.state]}
                {paper.reason ? ` — ${paper.reason}` : ''}
              </span>
            </span>
          </li>
        ))}
      </ul>

      {gaveUp && (
        <p className="import-note">
          Still running after five minutes — reopen later to check.
        </p>
      )}
    </div>
  )
}
