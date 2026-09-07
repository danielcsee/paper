"""The user's corpus: papers that finished importing.

    GET /corpus?page=1&page_size=20

Read-only. Ingestion writes these tables; this package only reads them, and
only counts a paper as present once its final stage is done.

Two routers: `router` is free to read, `protected_router` carries the two
metered routes (`rag_search` and `references`) and requires a token.
"""

from api.corpus.routes import protected_router, router

__all__ = ["protected_router", "router"]
