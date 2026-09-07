"""The Celery application.

Worker:  celery -A api.ingestion.celery_app worker --loglevel=info --pool=solo

**Use a non-forking pool.** On macOS sentence-transformers selects the Metal
(`mps`) device, which cannot be initialised in a `fork()`ed child: the default
prefork pool dies with SIGABRT the moment `embed_paper` encodes anything. The
solo pool sidesteps it. On Linux, prefork works — or set `embedding_device` to
"cpu" and use whichever pool you like.

Serialising tasks costs little here anyway: the pipeline is bounded by NCBI's
~3 req/s tolerance, and the embedding stage holds a torch model per process, so
more workers would multiply memory for little throughput.
"""

from __future__ import annotations

from celery import Celery

from api.app.config import get_settings

settings = get_settings()

celery_app = Celery(
    "sciterm",
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
    # No rate limit here. NCBI politeness is enforced in the HTTP transport
    # (api/ncbi/http.py), which covers the web process too — a queue-level limit
    # would only have covered the worker, and only for tasks that name it.
)
