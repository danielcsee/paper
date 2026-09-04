"""The paper ingestion pipeline: Celery tasks, chunking, embedding, /import.

    POST /import  ->  chain(fetch_paper | persist_paper | embed_chunks)

Everything here is stubbed. See README.md for what is built and what is not.
"""

from api.ingestion.celery_app import celery_app
from api.ingestion.routes import router

__all__ = ["celery_app", "router"]
