# Third-party APIs

Every external request this project makes, and what we know about each. Both
services are NCBI's, so both draw on **one shared budget of ~3 requests/second**
enforced in [`api/ncbi/http.py`](api/ncbi/http.py).

Nothing else is called. There are no API keys: both services are open, and
`NCBI_CONTACT_EMAIL` — if set — is appended to the `User-Agent` and is the only
thing we volunteer about ourselves.

---

## PubTator3

Base: `https://www.ncbi.nlm.nih.gov/research/pubtator3-api`
Client: [`api/pb_client/pubtator.py`](api/pb_client/pubtator.py) → exposed as `/pb/*`

### `GET /search/`

| Param | Notes |
|---|---|
| `text` | Free-text query. Required, non-empty. |
| `page` | 1-based. |

Response: `{current, page_size, count, total_pages, results[]}`. We trust
`current` over what we asked for — requesting page 9e9 returns an empty page
rather than an error.

Each result carries `pmid`, `pmcid`, `title`, `journal`, `authors`, `date`,
`doi`, `score`, `text_hl`. **`page_size` is not a parameter** — upstream always
returns 10 per page, so `search()` reports the size that came back instead of
accepting one.

`text_hl` is marked up as `@GENE_BRCA1 @GENE_672 @@@<m>BRCA1</m>@@@ suppresses`
— concept tags, then the matched span fenced in `@@@` with query terms in `<m>`.
`strip_highlight_markup` removes all of it for display.

### `GET /publications/export/biocjson`

| Param | Notes |
|---|---|
| `pmids` | Comma-separated. **Max 100 per request**; we chunk at that. |
| `full` | `true` for full text; omitted returns title + abstract. |

Returns BioC JSON: passages, annotations, relations, references. Three response
shapes, all handled by `_parse_documents` — a bare object, a `{"PubTator3": […]}`
wrapper, or one JSON document per line, depending on how many ids were asked
for. An **error** comes back as a bare list of strings, e.g.
`["pmids is a mandatory parameter."]`, which a success never is.

Two measured constraints:

- **PMID-keyed, always.** Passing `pmcids=` alone is rejected with HTTP 400.
  This costs nothing: a paper has full text exactly when it is also in PMC, and
  search returns both ids.
- **`full=true` is mandatory if you need `pmcid`.** The light export reports
  `pmcid: null` even for papers that *are* in PMC — the field only appears when
  full text is actually returned. So "does a full paper exist for this PMID?"
  cannot be answered by a cheaper call.

There is no citation graph here: references come from the paper's own
bibliography, and nothing reports who cites *it*.

---

## PMC Open Access (S3)

Base: `https://pmc-oa-opendata.s3.amazonaws.com`
Client: [`api/pm_client/pmc.py`](api/pm_client/pmc.py) → exposed as `/pm/download`

A plain S3 bucket, not an API: no query interface, no search, keyed on PMCID.
It replaced NCBI's retired OA FTP service in August 2026. PubTator cannot serve
these files, which is why this integration exists at all.

### `GET /metadata/<PMCID>.<version>.json`

Per-article metadata. We try `.1` first — almost every article is version 1 —
and only fall back to a listing when that 404s, so the common case is one
request rather than two.

Fields used: `is_pmc_openaccess` (we refuse to download anything else),
`version`, `pmcid`, `pmid`, `doi`, `title`, `citation`, `license_code`,
`is_retracted`, and the `s3://` locations `xml_url`, `text_url`, `pdf_url`,
`media_urls[]`.

### `GET /?list-type=2&prefix=<PMCID>.&delimiter=/`

S3 ListObjectsV2, the version fallback. Returns **XML**, not JSON, namespaced
`http://s3.amazonaws.com/doc/2006-03-01/`; we read `CommonPrefixes/Prefix`.

### `GET /<key>`

The files themselves — JATS XML, plain text, PDF, supplementary media. Streamed
to a `.part` file that is renamed on completion, so an interrupted download
cannot be mistaken for a finished one. Fetched serially: one article can carry a
long tail of media files.

---

## Not used

**NCBI E-utilities** (`efetch`/`esearch`). Noted because it is the obvious thing
to reach for: `efetch` on `pubmed` takes PMIDs and returns abstracts only,
`efetch` on `pmc` takes PMCIDs and returns full text. The same PMID/PMCID split
applies there, so it buys us nothing the two services above do not already give.

See [`pmcid vs pmid.txt`](pmcid%20vs%20pmid.txt) for the measurements behind the
id constraints.
