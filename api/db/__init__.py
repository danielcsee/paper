"""Postgres layer: schema, session factory, migrations.

The schema serves two jobs at once:

1. semantic / RAG retrieval  -> `paper_chunks.embedding`
2. a source for building the Neo4j graph
   -> `entities`, `paper_entity_mentions` (MENTIONS),
      `paper_relations` (CONTRADICTS seeds), `paper_references` (CITES)

Everything is populated from one PubTator response per paper. What cannot be
recomputed locally is stored; what can be (e.g. a paper-level embedding) is not.
"""

from api.db.base import Base, get_engine, get_sessionmaker, session_scope

__all__ = ["Base", "get_engine", "get_sessionmaker", "session_scope"]
