"""FastAPI app. For now it does exactly one thing: serve the compiled UI bundle."""

from fastapi import FastAPI
from fastapi.responses import HTMLResponse
from fastapi.staticfiles import StaticFiles

from api.app.config import get_settings

settings = get_settings()
app = FastAPI(title="litgraph")

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
