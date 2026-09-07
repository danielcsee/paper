import secrets
from functools import lru_cache
from pathlib import Path
from typing import Any, Literal, Optional

from pydantic_settings import BaseSettings, SettingsConfigDict

# api/app/config.py -> api/app -> api -> repo root
REPO_ROOT = Path(__file__).resolve().parents[2]


class Settings(BaseSettings):
    """Runtime configuration, overridable by environment or a root .env file."""

    model_config = SettingsConfigDict(
        env_file=REPO_ROOT / ".env", env_file_encoding="utf-8", extra="ignore"
    )

    # Where the compiled React bundle lives. The Docker image overrides this.
    sciterm_ui_dist: Path = REPO_ROOT / "ui" / "dist"

    # Datastore connections, so the image and the host process read their
    # addresses from one place.
    database_url: str = "postgresql://sciterm:sciterm@localhost:5432/sciterm"
    neo4j_uri: str = "bolt://localhost:7687"
    #: The same "user/password" string the Neo4j container reads, so the server
    #: and its clients cannot drift apart on credentials.
    neo4j_auth: str = "neo4j/sciterm_dev_pw"
    #: Community edition serves exactly one user database, named "neo4j".
    neo4j_database: str = "neo4j"

    # --- NCBI ---
    pubtator_base_url: str = "https://www.ncbi.nlm.nih.gov/research/pubtator3-api"
    pmc_s3_base_url: str = "https://pmc-oa-opendata.s3.amazonaws.com"
    #: E-utilities, used only to put names on concepts PubTator leaves unnamed.
    eutils_base_url: str = "https://eutils.ncbi.nlm.nih.gov/entrez/eutils"
    #: Root for downloaded articles; each gets a <papers_dir>/<PMCID>/ directory.
    papers_dir: Path = REPO_ROOT / "papers" / "pmc_subset"
    http_timeout_seconds: float = 60.0
    #: NCBI asks that clients identify a contact. Unset by default — nothing is
    #: sent to NCBI unless you put an address here yourself.
    ncbi_contact_email: Optional[str] = None
    #: Requests per second across *every* process, PubTator and PMC together.
    #: NCBI rate-limits its services as a whole, not per endpoint.
    ncbi_rate_limit_per_second: float = 3.0
    #: Where the shared slot lives. The broker's Redis, deliberately: the key is
    #: ~50 bytes, that instance runs `noeviction`, and it needs no new container.
    rate_limit_redis_url: str = "redis://localhost:6379/0"

    # --- Celery ---
    celery_broker_url: str = "redis://localhost:6379/0"
    celery_result_backend: str = "redis://localhost:6379/1"

    # --- Document cache ---
    #: A SEPARATE Redis from the broker. Eviction is per-instance, so a cache
    #: sharing the broker's instance could discard queued tasks.
    redis_cache_url: str = "redis://localhost:6380/0"
    document_cache_enabled: bool = True
    #: 24h. Every entry is re-fetchable, so this trades memory for politeness.
    document_cache_ttl_seconds: int = 86400
    #: Give up on Redis quickly. A slow cache must never be slower than the
    #: PubTator call it exists to avoid.
    document_cache_timeout_seconds: float = 0.5

    # --- Embeddings ---
    #: Must produce vectors of api.db.models.EMBEDDING_DIM (768). Changing this
    #: invalidates every stored embedding via the stage fingerprint.
    embedding_model: str = "BAAI/bge-base-en-v1.5"
    embedding_batch_size: int = 32
    # --- RAG search ---
    #: Chunks scoring below this cosine similarity are discarded before any
    #: paper is scored. Calibrated on this corpus with the bge query prefix,
    #: over 4 on-topic and 3 off-topic queries: the worst on-topic best-chunk
    #: scored 0.660, the best off-topic one 0.444, and 0.55 is the midpoint of
    #: that gap. At 0.55 the off-topic queries keep zero chunks.
    #:
    #: It is an *absolute* cutoff, so the margin narrows as the corpus grows —
    #: more chunks means more chances at a spuriously high score. Override with
    #: RAG_SCORE_THRESHOLD while we work out whether top-k is better.
    rag_score_threshold: float = 0.55
    #: Papers returned per search.
    rag_top_papers: int = 3
    #: Best-matching chunks returned per paper, as evidence for the ranking.
    rag_chunks_per_paper: int = 3

    #: torch device for the encoder. None lets sentence-transformers choose,
    #: which is "mps" on Apple silicon. Set to "cpu" when the worker must run
    #: in a forked process — Metal cannot be initialised after fork.
    embedding_device: Optional[str] = None

    # --- Environment ---
    #: "local" runs the app wide open, exactly as it behaved before auth
    #: existed. "prod" gates the metered routes and requires `jwt_secret`.
    #: One runtime switch rather than a build flag, so the same UI bundle and
    #: the same image serve both.
    sciterm_env: Literal["local", "prod"] = "local"

    # --- Auth ---
    #: Signs access tokens. Required in prod; in local an ephemeral one is
    #: generated per process, so tokens simply do not survive a restart.
    jwt_secret: Optional[str] = None
    #: Access tokens are verified from their signature alone, so this is also
    #: how long a revoked session can keep working. Short on purpose.
    access_token_ttl_seconds: int = 900
    #: How long a *password* session lives, refreshed on use. Code-backed
    #: sessions ignore this: their deadline comes from the code.
    refresh_token_ttl_seconds: int = 60 * 60 * 24 * 14
    #: The free-access window, measured from a code's first redemption.
    free_code_window_hours: int = 48
    #: Whether the refresh cookie is marked Secure. Defaults to "yes in prod",
    #: which is right for any real deployment. Set false only to demo a prod
    #: build over plain HTTP: a Secure cookie is never sent over http://, so
    #: sessions would silently fail to survive a reload.
    cookie_secure: Optional[bool] = None


    def model_post_init(self, __context: Any) -> None:
        """Fail closed on a misconfigured prod, and stay effortless in local.

        A prod deployment without a signing secret would mint tokens anyone
        could forge, so it refuses to start rather than serving something that
        looks protected and is not.
        """
        if self.sciterm_env == "prod" and not self.jwt_secret:
            raise ValueError(
                "SCITERM_ENV=prod requires JWT_SECRET. Generate one with "
                "`python3 -c 'import secrets; print(secrets.token_urlsafe(48))'`."
            )
        if not self.jwt_secret:
            self.jwt_secret = secrets.token_urlsafe(48)

    @property
    def refresh_cookie_secure(self) -> bool:
        """`cookie_secure` if set, otherwise on exactly when auth is on."""
        if self.cookie_secure is None:
            return self.auth_required
        return self.cookie_secure

    @property
    def auth_required(self) -> bool:
        """Whether the metered routes are gated. False in local development."""
        return self.sciterm_env == "prod"

    @property
    def signing_secret(self) -> str:
        """`jwt_secret`, narrowed to `str` — `model_post_init` guarantees one."""
        assert self.jwt_secret is not None
        return self.jwt_secret

    @property
    def neo4j_credentials(self) -> tuple[str, str]:
        """`neo4j_auth` split into the (user, password) pair the driver wants."""
        user, _, password = self.neo4j_auth.partition("/")
        return user, password


@lru_cache
def get_settings() -> Settings:
    return Settings()
