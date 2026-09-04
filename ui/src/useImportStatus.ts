import { useCallback, useEffect, useRef, useState } from 'react'
import {
  fetchImportStatus,
  type ImportResponse,
  type PaperState,
  type SearchResult,
} from './api'

/** One row in the import panel. */
export interface TrackedPaper {
  pmid: number | null
  /** Captured client-side: a queued paper has no stored title to fetch. */
  title: string
  state: PaperState
  /** Why it failed, or why it was never queued. */
  reason: string | null
}

/**
 * Most state changes land in the first few seconds — ingest is about half a
 * second per paper and embedding under one after the model is warm — so poll
 * fast at first and ease off rather than hammering a job that has stalled.
 */
const POLL_SCHEDULE_MS = [1000, 1000, 1000, 1000, 1000, 3000, 3000, 3000, 5000] as const
const POLL_IDLE_MS = 5000

/** Give up after this long rather than polling a wedged job forever. */
const POLL_LIMIT_MS = 5 * 60 * 1000

const TERMINAL: ReadonlySet<PaperState> = new Set<PaperState>(['success', 'error'])

function seedFromResponse(
  response: ImportResponse,
  titles: ReadonlyMap<number, string>,
): TrackedPaper[] {
  return response.jobs.map((job) => {
    // A rejected paper never gets a ledger row, so it is terminal on arrival.
    // Left as 'queued' it would sit there forever — the bug this replaces.
    if (job.status === 'rejected') {
      return {
        pmid: job.pmid,
        title: job.pmid != null ? titles.get(job.pmid) ?? `PMID ${job.pmid}` : 'Unknown paper',
        state: 'error' as PaperState,
        reason: job.reason ?? 'rejected',
      }
    }
    const state: PaperState =
      job.status === 'already_imported'
        ? 'success'
        : job.status === 'in_progress'
          ? 'started'
          : 'queued'
    return {
      pmid: job.pmid,
      title: job.pmid != null ? titles.get(job.pmid) ?? `PMID ${job.pmid}` : 'Unknown paper',
      state,
      reason: job.status === 'in_progress' ? job.reason : null,
    }
  })
}

export interface ImportStatusState {
  papers: TrackedPaper[]
  polling: boolean
  /** True once polling stopped with work still unfinished. */
  gaveUp: boolean
  track: (response: ImportResponse, papers: SearchResult[]) => void
  clear: () => void
}

/**
 * Tracks an import and keeps it current by polling `/import/status`.
 *
 * Polling rather than a push channel: the Celery worker is a separate process
 * from the API, so pushing would need a Redis pub/sub bridge and per-connection
 * fan-out — and Postgres is already the durable source of truth, so a push
 * channel would be an extra thing to keep in sync rather than a replacement.
 */
export function useImportStatus(): ImportStatusState {
  const [papers, setPapers] = useState<TrackedPaper[]>([])
  const [polling, setPolling] = useState(false)
  const [gaveUp, setGaveUp] = useState(false)

  const timerRef = useRef<number | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  const tickRef = useRef(0)
  const startedAtRef = useRef(0)
  // Read inside the poll loop, which must not restart on every state change.
  const pendingRef = useRef<number[]>([])

  const stop = useCallback(() => {
    if (timerRef.current !== null) window.clearTimeout(timerRef.current)
    timerRef.current = null
    abortRef.current?.abort()
    abortRef.current = null
    setPolling(false)
  }, [])

  const poll = useCallback(async () => {
    const pmids = pendingRef.current
    if (pmids.length === 0) {
      stop()
      return
    }
    if (Date.now() - startedAtRef.current > POLL_LIMIT_MS) {
      setGaveUp(true)
      stop()
      return
    }

    const controller = new AbortController()
    abortRef.current = controller
    try {
      const response = await fetchImportStatus(pmids, controller.signal)
      const byPmid = new Map(response.papers.map((p) => [p.pmid, p]))
      setPapers((prev) =>
        prev.map((paper) => {
          const update = paper.pmid == null ? undefined : byPmid.get(paper.pmid)
          if (!update) return paper
          return { ...paper, state: update.state, reason: update.error ?? paper.reason }
        }),
      )
    } catch (err) {
      if ((err as Error)?.name === 'AbortError') return
      // A failed poll is not a failed import. Keep the last known state and
      // try again on the next tick rather than reporting papers as broken.
    }

    const index = Math.min(tickRef.current, POLL_SCHEDULE_MS.length - 1)
    const delay = POLL_SCHEDULE_MS[index] ?? POLL_IDLE_MS
    tickRef.current += 1
    timerRef.current = window.setTimeout(() => void poll(), delay)
  }, [stop])

  // Whenever the tracked set changes, recompute what still needs watching.
  useEffect(() => {
    pendingRef.current = papers
      .filter((paper) => paper.pmid != null && !TERMINAL.has(paper.state))
      .map((paper) => paper.pmid as number)
    if (pendingRef.current.length === 0) stop()
  }, [papers, stop])

  useEffect(() => () => stop(), [stop])

  const track = useCallback(
    (response: ImportResponse, selected: SearchResult[]) => {
      const titles = new Map<number, string>()
      for (const result of selected) {
        if (result.pmid != null) titles.set(result.pmid, result.title ?? `PMID ${result.pmid}`)
      }
      const incoming = seedFromResponse(response, titles)

      // Merge by pmid: a paper still importing must not vanish because a
      // second batch was queued.
      setPapers((prev) => {
        const merged = new Map<string, TrackedPaper>()
        for (const paper of [...prev, ...incoming]) {
          merged.set(paper.pmid != null ? `pmid:${paper.pmid}` : `rejected:${merged.size}`, paper)
        }
        return [...merged.values()]
      })

      setGaveUp(false)
      tickRef.current = 0
      startedAtRef.current = Date.now()
      if (incoming.some((paper) => paper.pmid != null && !TERMINAL.has(paper.state))) {
        setPolling(true)
        if (timerRef.current !== null) window.clearTimeout(timerRef.current)
        timerRef.current = window.setTimeout(() => void poll(), 400)
      }
    },
    [poll],
  )

  const clear = useCallback(() => {
    stop()
    setPapers([])
    setGaveUp(false)
  }, [stop])

  return { papers, polling, gaveUp, track, clear }
}
