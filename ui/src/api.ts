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

export class ApiError extends Error {}

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
    throw new ApiError(detail)
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
