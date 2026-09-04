"""The Celery application.

Worker:  celery -A api.ingestion.celery_app worker --loglevel=info --concurrency=2

Concurrency is deliberately low. The pipeline's slowest stage is bounded by
NCBI's ~3 req/s tolerance, not by CPU, and the embedding stage holds a torch
model per process — more workers would multiply memory for no throughput.
"""

from __future__ import annotations

from celery import Celery

from api.app.config import get_settings

settings = get_settings()

celery_app = Celery(
    "litgraph",
    broker=settings.celery_broker_url,
    backend=settings.celery_result_backend,
    include=["api.ingestion.tasks"],
)

celery_app.conf.update(
    task_serializer="json",
    result_serializer="json",
    accept_content=["json"],
    timezone="UTC",
    enable_utc=True,
    # A task is acknowledged only once it finishes, so a killed worker returns
    # its work to the queue instead of dropping it.
    task_acks_late=True,
    worker_prefetch_multiplier=1,
    task_track_started=True,
    result_expires=3600,
    # Enforce NCBI politeness at the queue. Only the fetch task talks to them.
    task_annotations={
        "api.ingestion.tasks.fetch_paper": {"rate_limit": settings.pubtator_rate_limit}
    },
)
