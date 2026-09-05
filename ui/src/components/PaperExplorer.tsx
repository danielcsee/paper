import { useRef, useState } from 'react'
import { ApiError, importPapers, type ImportPmids, type SearchResult } from '../api'
import { useImportStatus } from '../useImportStatus'
import ImportStatus from './ImportStatus'
import ReferenceImporter from './ReferenceImporter'
import SearchPubTator from './SearchPubTator'

export interface ReferenceTarget {
  paperId: number
  title: string
}

interface Props {
  /** Set when the reader asked to see a paper's references. */
  referencesFor: ReferenceTarget | null
  onCloseReferences: () => void
}

/**
 * The right-hand panel: search PubTator, or browse a paper's references, with
 * one shared import queue beneath.
 *
 * Import state lives here rather than in either panel so both feed a single
 * `ImportStatus`, and so switching panels never abandons an import in flight.
 */
export default function PaperExplorer({ referencesFor, onCloseReferences }: Props) {
  const [importing, setImporting] = useState(false)
  const [importCount, setImportCount] = useState(0)
  const [importError, setImportError] = useState<string | null>(null)
  const importStatus = useImportStatus()
  const abortRef = useRef<AbortController | null>(null)

  async function handleImport(pmids: ImportPmids[], papers: SearchResult[]) {
    if (pmids.length === 0) return
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller

    setImporting(true)
    setImportCount(pmids.length)
    setImportError(null)
    try {
      const response = await importPapers(pmids, controller.signal)
      // Titles come from the caller: a queued paper has none stored yet.
      importStatus.track(response, papers)
    } catch (err) {
      if ((err as Error)?.name === 'AbortError') return
      setImportError(
        err instanceof ApiError ? err.message : 'Could not reach the import service.',
      )
    } finally {
      if (!controller.signal.aborted) setImporting(false)
    }
  }

  const showingReferences = referencesFor !== null

  return (
    <aside className="sidebar" aria-label={showingReferences ? 'Paper references' : 'Paper search'}>
      {/* SearchPubTator stays mounted while references are shown: hiding it
          rather than unmounting preserves the query, scroll position and any
          pending selection for when the reader comes back. */}
      <div className="explorer-panel" hidden={showingReferences}>
        <SearchPubTator
          onImport={(pmids, papers) => void handleImport(pmids, papers)}
          importing={importing}
        />
      </div>

      {showingReferences && (
        <div className="explorer-panel">
          <ReferenceImporter
            paperId={referencesFor.paperId}
            paperTitle={referencesFor.title}
            onImport={(pmids, papers) => void handleImport(pmids, papers)}
            importing={importing}
            onClose={onCloseReferences}
          />
        </div>
      )}

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
    </aside>
  )
}
