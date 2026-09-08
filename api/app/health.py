"""Liveness and readiness, split because a load balancer needs the first only.

`/health` answers "is this process serving?" and nothing else. It is what the
ALB target group should poll. Deliberately no database call: if Postgres goes
down, every task fails the check at once, the balancer drains the whole target
group, and visitors get its own 503 instead of the app's error. That turns a
degraded dependency into a total outage and hides the cause.

`/health/ready` answers "can this process do useful work?" and does touch the
dependencies. Use it after a deploy, from an alarm, or by hand -- not as the
balancer's health check.

Both are unauthenticated: a health check cannot present a token, and neither
route reveals anything an anonymous visitor could not already infer.
"""

from __future__ import annotations

import logging
import time
from typing import Dict

from fastapi import APIRouter, Response
from pydantic import BaseModel
from sqlalchemy import text

from api.app.config import get_settings
from api.db import session_scope

log = logging.getLogger(__name__)
router = APIRouter(tags=["health"])

#: Process start, for the uptime field. Monotonic so a clock change cannot make
#: a task look like it just restarted.
_STARTED = time.monotonic()

#: Readiness must not hang. A check slower than the balancer's own timeout is
#: indistinguishable from a failure, so give up first and say so.
_CHECK_TIMEOUT_SECONDS = 2.0


class HealthResponse(BaseModel):
    status: str
    #: "local" or "prod" -- confirms which configuration the running image took.
    environment: str
    #: Whatever SCITERM_VERSION was set to at build time, "unknown" otherwise.
    version: str
    #: Seconds this process has been serving. A value that keeps resetting is a
    #: crash loop, which is otherwise easy to mistake for a slow deploy.
    uptime_seconds: int


class ReadinessResponse(HealthResponse):
    #: Dependency name -> "ok" or a short reason. 503 if any is not "ok".
    checks: Dict[str, str]


def _base() -> Dict[str, object]:
    settings = get_settings()
    return {
        "status": "ok",
        "environment": settings.sciterm_env,
        "version": settings.sciterm_version,
        "uptime_seconds": int(time.monotonic() - _STARTED),
    }


@router.get("/health", response_model=HealthResponse, summary="Liveness")
def health() -> HealthResponse:
    """200 whenever the process can serve a request. Point the ALB here."""
    return HealthResponse(**_base())  # type: ignore[arg-type]


@router.get("/health/ready", response_model=ReadinessResponse, summary="Readiness")
def ready(response: Response) -> ReadinessResponse:
    """Check the dependencies this process actually uses, and report each.

    The API needs Postgres and the Celery broker; it does not touch Neo4j, so
    neither does this. A green readiness check here says nothing about whether
    the worker can reach the graph -- only an import can.
    """
    checks: Dict[str, str] = {}

    try:
        with session_scope() as session:
            session.execute(text("SELECT 1"))
        checks["postgres"] = "ok"
    except Exception as exc:
        log.warning("readiness: postgres failed: %s", exc)
        checks["postgres"] = f"error: {type(exc).__name__}"

    try:
        import redis

        client = redis.Redis.from_url(
            get_settings().celery_broker_url,
            socket_connect_timeout=_CHECK_TIMEOUT_SECONDS,
            socket_timeout=_CHECK_TIMEOUT_SECONDS,
        )
        try:
            client.ping()
            checks["broker"] = "ok"
        finally:
            client.close()
    except Exception as exc:
        log.warning("readiness: broker failed: %s", exc)
        checks["broker"] = f"error: {type(exc).__name__}"

    body = _base()
    if any(value != "ok" for value in checks.values()):
        body["status"] = "degraded"
        response.status_code = 503
    return ReadinessResponse(**body, checks=checks)  # type: ignore[arg-type]
