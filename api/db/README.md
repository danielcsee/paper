# api/db

The Postgres layer: schema, session plumbing, and migrations. Everything here
is populated from one PubTator response per paper.

The schema serves two jobs at once:

1. **Semantic / RAG retrieval** — `paper_chunks.embedding` (pgvector)
2. **A source for building the Neo4j graph** — `entities` and
   `paper_entity_mentions` (`MENTIONS`), `paper_relations` (`CONTRADICTS`
   seeds), `paper_references` (`CITES`)

## Files

**`base.py`** — `Base`, the lazily-built engine, and `session_scope()`, which
commits on success and rolls back on failure. `normalise_url()` forces the
psycopg3 driver, because a bare `postgresql://` URL makes SQLAlchemy reach for
psycopg2, which is not installed.

**`models.py`** — the tables: `papers`, `paper_pubtator_docs`, `paper_authors`,
`paper_chunks`, `entities`, `paper_entity_mentions`, `paper_relations`,
`paper_references`, `paper_stage_runs`.

## Two design rules encoded here

**Store what cannot be recomputed locally.** Authors, references, annotations
and relations all come from a rate-limited external API, so re-deriving them
means re-fetching. A paper-level embedding is deliberately absent — it is
rebuildable from text we already hold, so a migration can add it later for free.

**`pmid` is the natural key, not `pmcid`.** PubTator is keyed on PMID and
search always returns one; `pmcid` is null for roughly a quarter of results,
which are abstract-only. Those papers still get a row, with
`has_full_text = false`.

Vocabularies NCBI controls (`section_type`, `entity_type`, `relation_type`) are
plain text with no CHECK constraint, so an added upstream value cannot turn
into an ingest failure. Vocabularies we own (`stage`, `status`) are constrained.

## Subdirectories

- [`migrations/`](migrations) — Alembic environment and versioned migrations

## Dependencies

`SQLAlchemy` 2.x, `alembic`, `psycopg` (v3), `pgvector`.
