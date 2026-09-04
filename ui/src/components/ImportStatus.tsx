import type { ImportJobStatus, ImportResponse } from '../api'

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

  const count = (status: ImportJobStatus) =>
    result.jobs.filter((job) => job.status === status).length
  const rejected = result.jobs.filter((job) => job.status === 'rejected')

  // Every status is named here, so a paper can never vanish from the summary.
  const parts = [
    `${count('queued')} queued`,
    count('in_progress') > 0 ? `${count('in_progress')} already running` : null,
    count('already_imported') > 0 ? `${count('already_imported')} already imported` : null,
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
