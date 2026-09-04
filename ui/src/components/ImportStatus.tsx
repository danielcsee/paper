import type { ImportResponse } from '../api'

interface Props {
  pending: boolean
  /** Number of papers in the request that is in flight. */
  pendingCount: number
  result: ImportResponse | null
  error: string | null
  onDismiss: () => void
}

/** Outcome of the most recent /import call, shown under the search bar. */
export default function ImportStatus({
  pending,
  pendingCount,
  result,
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

  if (!result) return null

  const queued = result.jobs.filter((job) => job.status === 'queued').length
  const already = result.jobs.filter((job) => job.status === 'already_imported').length
  const rejected = result.jobs.filter((job) => job.status === 'rejected')

  const parts = [
    `${queued} queued`,
    already > 0 ? `${already} already imported` : null,
    rejected.length > 0 ? `${rejected.length} rejected` : null,
  ].filter(Boolean)

  return (
    <div className="import-status" role="status" aria-live="polite">
      <div className="import-summary">
        <span>{parts.join(' · ')}</span>
        <button className="import-dismiss" type="button" onClick={onDismiss}>
          Dismiss
        </button>
      </div>
      {rejected.length > 0 && (
        <ul className="import-rejected">
          {rejected.map((job, index) => (
            <li key={job.pmid ?? `rejected-${index}`}>
              {job.pmid != null ? `PMID ${job.pmid}: ` : ''}
              {job.reason ?? 'rejected'}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
