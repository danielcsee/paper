from functools import lru_cache
from pathlib import Path
from typing import Optional

from pydantic_settings import BaseSettings, SettingsConfigDict

# api/app/config.py -> api/app -> api -> repo root
REPO_ROOT = Path(__file__).resolve().parents[2]


class Settings(BaseSettings):
    """Runtime configuration, overridable by environment or a root .env file."""

    model_config = SettingsConfigDict(
        env_file=REPO_ROOT / ".env", env_file_encoding="utf-8", extra="ignore"
    )

    # Where the compiled React bundle lives. The Docker image overrides this.
    litgraph_ui_dist: Path = REPO_ROOT / "ui" / "dist"

    # Datastore connections. Nothing connects to these yet; they are here so the
    # image and the host process read their addresses from one place.
    database_url: str = "postgresql://litgraph:litgraph@localhost:5432/litgraph"
    neo4j_uri: str = "bolt://localhost:7687"

    # --- NCBI ---
    pubtator_base_url: str = "https://www.ncbi.nlm.nih.gov/research/pubtator3-api"
    pmc_s3_base_url: str = "https://pmc-oa-opendata.s3.amazonaws.com"
    #: Root for downloaded articles; each gets a <papers_dir>/<PMCID>/ directory.
    papers_dir: Path = REPO_ROOT / "papers" / "pmc_subset"
    http_timeout_seconds: float = 60.0
    #: NCBI asks that clients identify a contact. Unset by default — nothing is
    #: sent to NCBI unless you put an address here yourself.
    ncbi_contact_email: Optional[str] = None

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


@lru_cache
def get_settings() -> Settings:
    return Settings()
