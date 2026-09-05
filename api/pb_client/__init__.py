"""Client for PubTator3 — text search over the annotated literature.

Search returns paper metadata; the export endpoint returns one paper's full
annotated text, structured into passages. Both are keyed on PMID.

Downloading the article *files* is a different service and lives in
`api.pm_client`. What the two share — connection pool, rate limit, error
types — lives in `api.ncbi`.
"""

from api.pb_client.pubtator import PubTatorClient
from api.pb_client.routes import router

__all__ = ["PubTatorClient", "router"]
