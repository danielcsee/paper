"""FastAPI app: the /pb NCBI client routes, plus the compiled UI bundle."""

from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi.responses import HTMLResponse
from fastapi.staticfiles import StaticFiles

from api.app.config import get_settings
from api.corpus import router as corpus_router
from api.ingestion import router as ingestion_router
from api.pb_client import PmcClient, PubTatorClient
from api.pb_client import http as pb_http
from api.pb_client import router as pb_router

settings = get_settings()


@asynccontextmanager
async def lifespan(app: FastAPI):
    """One pooled HTTP client for the process lifetime, shared by both clients."""
    client = pb_http.build_client(
        timeout=settings.http_timeout_seconds,
        contact_email=settings.ncbi_contact_email,
    )
    app.state.http = client
    app.state.pubtator = PubTatorClient(client, settings.pubtator_base_url)
    app.state.pmc = PmcClient(client, settings.pmc_s3_base_url, settings.papers_dir)
    try:
        yield
    finally:
        await client.aclose()


app = FastAPI(title="litgraph", lifespan=lifespan)

# Routers first: StaticFiles below is mounted at "/" and would otherwise
# swallow every path, /pb included.
app.include_router(pb_router)
app.include_router(ingestion_router)
app.include_router(corpus_router)

dist = settings.litgraph_ui_dist

if (dist / "index.html").is_file():
    # html=True serves index.html at "/" and 404s unknown paths, which is what we
    # want while the UI is a single page. A client-side router would need a
    # catch-all fallback here instead.
    app.mount("/", StaticFiles(directory=dist, html=True), name="ui")
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
