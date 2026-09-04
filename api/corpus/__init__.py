"""The user's corpus: papers that finished importing.

    GET /corpus?page=1&page_size=20

Read-only. Ingestion writes these tables; this package only reads them, and
only counts a paper as present once its final stage is done.
"""

from api.corpus.routes import router

__all__ = ["router"]
