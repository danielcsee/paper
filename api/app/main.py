"""FastAPI app: the /pb NCBI client routes, plus the compiled UI bundle."""

import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi.responses import FileResponse, HTMLResponse
from fastapi.staticfiles import StaticFiles

from api.app.config import get_settings
from api.app.health import router as health_router
from api.auth import admin_router, router as auth_router
from api.auth.config import get_auth_settings
from api.cache import DocumentCache
from api.redis_conn import close_client as close_redis
from api.corpus import protected_router as corpus_protected_router
from api.corpus import router as corpus_router
from api.ingestion import router as ingestion_router
from api.ncbi import http as ncbi_http
from api.pb_client import PubTatorClient
from api.pb_client import router as pb_router
from api.pm_client import PmcClient
from api.pm_client import router as pm_router

log = logging.getLogger(__name__)
settings = get_settings()


@asynccontextmanager
async def lifespan(app: FastAPI):
    """One pooled HTTP client for the process lifetime, shared by both clients.

    Shared deliberately: the rate limiter it carries is per-client-object, so
    two clients would mean two budgets against one organisation.
    """
    client = ncbi_http.build_client(
        timeout=settings.http_timeout_seconds,
        contact_email=settings.ncbi_contact_email,
        limiter=ncbi_http.RedisRateLimiter(
            settings.rate_limit_redis_url, settings.ncbi_rate_limit_per_second
        ),
    )
    app.state.http = client
    app.state.cache = DocumentCache(
        settings.redis_cache_url,
        ttl_seconds=settings.document_cache_ttl_seconds,
        timeout=settings.document_cache_timeout_seconds,
        enabled=settings.document_cache_enabled,
    )
    app.state.pubtator = PubTatorClient(
        client, settings.pubtator_base_url, cache=app.state.cache
    )
    app.state.pmc = PmcClient(client, settings.pmc_s3_base_url, settings.papers_dir)
    log.info(
        "sciterm starting in %s (auth %s)",
        settings.sciterm_env,
        "required" if get_auth_settings().auth_required else "off",
    )
    try:
        yield
    finally:
        await client.aclose()
        await app.state.cache.aclose()
        await close_redis(settings.rate_limit_redis_url)


app = FastAPI(title="sciterm", lifespan=lifespan)

# Routers first: StaticFiles below is mounted at "/" and would otherwise
# swallow every path, /pb included.
app.include_router(health_router)
app.include_router(auth_router)
app.include_router(admin_router)
app.include_router(pb_router)
app.include_router(pm_router)
app.include_router(ingestion_router)
# Before the free corpus router, not after: "/corpus/rag_search" would
# otherwise be matched by "/corpus/{paper_id}" and rejected as a bad integer.
app.include_router(corpus_protected_router)
app.include_router(corpus_router)

dist = settings.sciterm_ui_dist

if (dist / "index.html").is_file():
    # Hashed bundles are immutable and can be served straight from disk.
    app.mount("/assets", StaticFiles(directory=dist / "assets"), name="assets")

    @app.get("/{full_path:path}", include_in_schema=False)
    def spa(full_path: str) -> FileResponse:
        """Serve the bundle for any path the routers above did not claim.

        The UI has client-side routes now (`/paper/12`), so a reload or a
        shared link must return index.html rather than 404. Registered last, so
        every API route still wins; a real file in dist (favicon, robots.txt)
        is served as itself.
        """
        candidate = (dist / full_path).resolve()
        if full_path and dist.resolve() in candidate.parents and candidate.is_file():
            return FileResponse(candidate)
        return FileResponse(dist / "index.html")

else:

    @app.get("/", response_class=HTMLResponse)
    def missing_bundle() -> HTMLResponse:
        """Explain the problem instead of 404ing on a fresh checkout."""
        return HTMLResponse(
            "<h1>UI bundle not built</h1>"
            f"<p>Expected <code>{dist / 'index.html'}</code>.</p>"
            "<p>Run <code>npm --prefix ui run build</code>, "
            "or use <code>./scripts/dev.sh</code> for the hot-reloading dev server.</p>",
            status_code=503,
        )
