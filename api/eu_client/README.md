# api/eu_client

Client for **NCBI E-utilities**, used for exactly one job: naming concepts
PubTator leaves unnamed.

PubTator sends `"name": "9606"` for Species and `"name": "4362"` for CellLine —
the identifier again, because it has no label for those types. Measured on this
corpus: every `ncbi_taxonomy` and `cvcl` row arrives that way, while `ncbi_gene`
and `ncbi_mesh` carry real names.

## Files

| File | Purpose |
|---|---|
| `eutils.py` | `EutilsClient.names(db, uids)` — batched `esummary` lookups |
| `naming.py` | Find unnamed entities, resolve them, write the names back |
| `backfill.py` | `python -m api.eu_client.backfill` for rows already stored |
| `models.py` | `ConceptName` — preferred label, plus common and formal names |

## Worth knowing

**One request, many ids.** 62 taxon ids resolve in a single `esummary` call. No
API key; the shared NCBI rate limiter in [`api/ncbi`](../ncbi) applies.

**Common name first.** A reader recognises "domestic cat" faster than *Felis
catus*, and the formal name is kept alongside it.

**Only NCBI is resolvable.** `CVCL:` is Cellosaurus and `OMIM:` needs a key, so
8 rows here keep the label they have. `RESOLVABLE` is the map of what can be
looked up.

**Some taxa never resolve.** Three in this corpus are `status: merged` at NCBI
and return empty names. Ingest-time naming is scoped to the importing paper's
own concepts, so those are not re-queried on every import.

**Nothing here can fail an import.** The ingest hook runs after the stage is
marked done and swallows its errors: a missing label is cosmetic.

## Dependencies

`httpx` via `api.ncbi`, `pydantic`, `SQLAlchemy` via `api.db`.
