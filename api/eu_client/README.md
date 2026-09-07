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
| `naming.py` | Find unnamed concepts, resolve them, write the names back |
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
and return empty names, so they are asked about once per paper that names them
and never stick.

**Resolved before the write, not after.** Ingest scans the fetched paper in
memory, checks which identifiers the database already names — 113 of 116 across
the stored corpus — and resolves only the rest, so names go in with the insert.
Patching afterwards meant overwriting `name` on rows every other paper touches,
and a failed lookup left a value someone had to correct later.

**Nothing here can fail an import.** Resolution is fail-open and runs outside
any transaction: a missing label is cosmetic, and a rate-limited call inside a
transaction is how short locks become long ones.

## Dependencies

`httpx` via `api.ncbi`, `pydantic`, `SQLAlchemy` via `api.db`.
