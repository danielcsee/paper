/** Typed access to the FastAPI /pb routes. Mirrors api/pb_client/models.py. */

export interface SearchResult {
  pmid: number | null
  pmcid: string | null
  title: string | null
  journal: string | null
  authors: string[]
  date: string | null
  doi: string | null
  score: number | null
  /** Raw PubTator highlight, entity markup intact. */
  text_hl: string | null
  /** text_hl with the markup stripped — safe to render. */
  snippet: string | null
}

export interface SearchResponse {
  query: string
  page: number
  page_size: number
  total_results: number
  total_pages: number
  results: SearchResult[]
}

export class ApiError extends Error {
  readonly status: number

  constructor(message: string, status = 0) {
    super(message)
    this.status = status
  }
}

export async function searchPapers(
  text: string,
  page: number,
  signal?: AbortSignal,
): Promise<SearchResponse> {
  const params = new URLSearchParams({ text, page: String(page) })
  const response = await fetch(`/pb/search?${params}`, { signal })

  if (!response.ok) {
    // FastAPI errors are {"detail": ...}; fall back to the status line.
    let detail = `search failed (${response.status})`
    try {
      const body = await response.json()
      if (typeof body?.detail === 'string') detail = body.detail
    } catch {
      /* non-JSON error body — keep the status line */
    }
    throw new ApiError(detail, response.status)
  }
  return (await response.json()) as SearchResponse
}

/** A stable identity for a result, for React keys and selection. */
export function resultKey(result: SearchResult): string {
  if (result.pmid != null) return `pmid:${result.pmid}`
  if (result.pmcid) return `pmcid:${result.pmcid}`
  // Neither id: fall back to something still distinct, so React keys hold.
  return `doi:${result.doi ?? result.title ?? 'unknown'}`
}

/** Why a result cannot be opened, or null when it can. */
export function resultWarning(result: SearchResult): string | null {
  if (!result.pmcid) return 'no pmcid: abstract only.'
  if (result.pmid == null) return 'no pmid: download-only'
  return null
}

// --- import ---

export type ImportJobStatus =
  | 'queued'
  | 'in_progress'
  | 'already_imported'
  | 'rejected'

export interface ImportJob {
  pmid: number | null
  status: ImportJobStatus
  /** Set only for `queued`. */
  task_id: string | null
  /** Why a paper was rejected, or why it was not re-queued. */
  reason: string | null
}

export interface ImportResponse {
  jobs: ImportJob[]
}

/** A paper's overall progress, collapsed by the backend from its stage rows. */
export type PaperState = 'queued' | 'started' | 'success' | 'error'

export interface PaperProgress {
  pmid: number
  paper_id: number | null
  stages: Record<string, string>
  state: PaperState
  error: string | null
}

export interface ImportStatusResponse {
  papers: PaperProgress[]
}

/** Poll the ingestion ledger for the papers still in flight. */
export async function fetchImportStatus(
  pmids: number[],
  signal?: AbortSignal,
): Promise<ImportStatusResponse> {
  const params = new URLSearchParams()
  for (const pmid of pmids) params.append('pmids', String(pmid))
  const response = await fetch(`/import/status?${params}`, { signal })

  if (!response.ok) {
    let detail = `could not read import status (${response.status})`
    try {
      const body = await response.json()
      if (typeof body?.detail === 'string') detail = body.detail
    } catch {
      /* non-JSON error body — keep the status line */
    }
    throw new ApiError(detail, response.status)
  }
  return (await response.json()) as ImportStatusResponse
}

/** Queue the selected papers for ingestion. */
export async function importPapers(
  papers: SearchResult[],
  signal?: AbortSignal,
): Promise<ImportResponse> {
  const response = await fetch('/import', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ papers }),
    signal,
  })

  if (!response.ok) {
    let detail = `import failed (${response.status})`
    try {
      const body = await response.json()
      if (typeof body?.detail === 'string') detail = body.detail
    } catch {
      /* non-JSON error body — keep the status line */
    }
    throw new ApiError(detail, response.status)
  }
  return (await response.json()) as ImportResponse
}

// --- corpus ---

export interface CorpusPaper {
  paper_id: number
  pmid: number
  pmcid: string | null
  title: string | null
  journal: string | null
  pub_year: number | null
  doi: string | null
  authors: string[]
  snippet: string | null
  chunk_count: number
  has_full_text: boolean
  imported_at: string | null
}

export interface CorpusPage {
  page: number
  page_size: number
  total_papers: number
  total_pages: number
  papers: CorpusPaper[]
}

/** Matches the backend's DEFAULT_PAGE_SIZE. */
export const CORPUS_PAGE_SIZE = 20

export async function fetchCorpus(
  page: number,
  signal?: AbortSignal,
): Promise<CorpusPage> {
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(CORPUS_PAGE_SIZE),
  })
  const response = await fetch(`/corpus?${params}`, { signal })

  if (!response.ok) {
    let detail = `could not load your corpus (${response.status})`
    try {
      const body = await response.json()
      if (typeof body?.detail === 'string') detail = body.detail
    } catch {
      /* non-JSON error body — keep the status line */
    }
    throw new ApiError(detail, response.status)
  }
  return (await response.json()) as CorpusPage
}

export interface PaperParagraph {
  ordinal: number
  section_type: string | null
  /** PubTator's passage kind; anything containing "title" is a heading. */
  chunk_type: string | null
  text: string
}

export interface PaperReference {
  ordinal: number
  title: string | null
  pmid: string | null
  doi: string | null
  source: string | null
  year: string | null
  volume: string | null
  fpage: string | null
  lpage: string | null
}

export interface PaperDetail {
  paper_id: number
  pmid: number
  pmcid: string | null
  title: string | null
  journal: string | null
  journal_title: string | null
  pub_year: number | null
  volume: string | null
  fpage: string | null
  lpage: string | null
  doi: string | null
  has_full_text: boolean
  imported_at: string | null
  authors: string[]
  paragraphs: PaperParagraph[]
  references: PaperReference[]
}

export function isHeading(paragraph: PaperParagraph): boolean {
  return Boolean(paragraph.chunk_type && paragraph.chunk_type.includes('title'))
}

export async function fetchPaper(
  paperId: number,
  signal?: AbortSignal,
): Promise<PaperDetail> {
  const response = await fetch(`/corpus/${paperId}`, { signal })
  if (!response.ok) {
    let detail =
      response.status === 404
        ? 'That paper is not in your corpus.'
        : `could not load the paper (${response.status})`
    try {
      const body = await response.json()
      if (typeof body?.detail === 'string') detail = body.detail
    } catch {
      /* non-JSON error body — keep the status line */
    }
    throw new ApiError(detail, response.status)
  }
  return (await response.json()) as PaperDetail
}

// --- rag search ---

export interface RagChunk {
  chunk_id: number
  section_type: string | null
  text: string
  score: number
}

export interface RagPaper {
  paper_id: number
  pmid: number
  pmcid: string | null
  title: string | null
  journal: string | null
  pub_year: number | null
  score: number
  matched_chunks: number
  best_score: number
  chunks: RagChunk[]
}

export interface RagSearchResponse {
  query: string
  threshold: number
  aggregator: string
  chunks_considered: number
  papers: RagPaper[]
}

/** Retrieval only — the backend runs no LLM, so this returns ranked papers. */
export async function ragSearch(
  query: string,
  signal?: AbortSignal,
): Promise<RagSearchResponse> {
  const params = new URLSearchParams({ query })
  const response = await fetch(`/corpus/rag_search?${params}`, { signal })

  if (!response.ok) {
    let detail = `search failed (${response.status})`
    try {
      const body = await response.json()
      if (typeof body?.detail === 'string') detail = body.detail
    } catch {
      /* non-JSON error body — keep the status line */
    }
    throw new ApiError(detail, response.status)
  }
  return (await response.json()) as RagSearchResponse
}
