import type { Source } from '../types'

interface Props {
  sources: Source[]
}

export default function Sidebar({ sources }: Props) {
  return (
    <aside className="sidebar" aria-label="Sources">
      <div className="sidebar-header">
        <h2 className="sidebar-title">Sources</h2>
        {sources.length > 0 && <span className="sidebar-count">{sources.length}</span>}
      </div>

      {sources.length === 0 ? (
        <p className="sidebar-empty">
          Papers cited in an answer appear here, with the confidence score the
          graph assigned them.
        </p>
      ) : (
        <ul className="source-list">
          {sources.map((source) => (
            <li key={source.pmcid} className="source">
              <div className="source-title">{source.title}</div>
              <div className="source-meta">
                {[source.journal, source.year].filter(Boolean).join(' · ')}
              </div>
              <div className="source-ids">{source.pmcid}</div>
            </li>
          ))}
        </ul>
      )}
    </aside>
  )
}
