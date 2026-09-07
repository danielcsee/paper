# api/graph

The Neo4j knowledge graph, projected from the Postgres tables in
[`api/db`](../db).

Kept in sync by the pipeline: `graph_paper` is the last stage of every import,
and `PaperStageRun.FINAL_STAGE` is `"graph"`, so a paper is not in the corpus
listing or RAG results until it is in the graph. The bulk build remains for
rebuilds and for catching up a corpus imported before the stage existed —
idempotent either way.

## The model

Community edition constrains only **identity**, so `schema.cypher` holds four
uniqueness constraints plus read indexes. What it cannot express — which labels
exist, what connects to what — the loader enforces and this file records.

| Node | Key |
|---|---|
| `:Paper` (`in_corpus` false for cited works we lack) | `pmid` |
| `:Entity` + `:Gene`/`:Disease`/`:Chemical`/`:Species`/`:CellLine` | `entity_id` |
| `:Author` (`"surname, given names"`, casefolded) | `author_id` |
| `:Claim` — **constrained but unpopulated** | `claim_id` |

```
(:Author)-[:AUTHORED {ordinal}]->(:Paper)
(:Paper) -[:CITES]->(:Paper)
(:Paper) -[:MENTIONS {mention_count, sections}]->(:Entity)
```

Claims are designed, not built — extraction is undecided. They will add
`ASSERTS`, `SUBJECT`, `OBJECT` and `CONTRADICTS`. Purely additive: no migration.

## Files

**`keys.py`** — every node key, derived once so the bulk export and the
per-paper `graph` stage cannot disagree. `author_key` is a placeholder:
`paper_authors` has no ORCID, so normalisation stops at case and whitespace —
"German, Alexander J." stays distinct from "German, A. J.".

**`schema.cypher` / `schema.py`** — constraints and indexes. Creating a
constraint registers its label token, so `:Claim` appears in `db.labels()`.

**`export.py`** — one function per node or edge type, dependency-ordered. Nodes
`MERGE` on their key; edges `MATCH` both endpoints, so a missing node fails
loudly rather than leaving a stub. Each takes an optional `pmid`: `load_all`
projects the corpus, `load_paper` projects one, through the same statements.
Every use of that filter is `CAST(:pmid AS bigint)` — Postgres cannot infer the
type of a NULL bind, and `WHERE :pmid IS NULL` is exactly that case.

Edge properties are `SET`, never accumulated, so re-importing a paper rewrites
`mention_count` rather than doubling it.

**`driver.py`** — shared driver, `run` / `run_batched`.
**`build.py`** — CLI: `python -m api.graph.build [--schema-only|--reset]`.

## Dependencies

`neo4j` (Bolt), SQLAlchemy for the reads, and the Neo4j container with its
Graph Data Science plugin.
