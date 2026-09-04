"""Chunk embeddings via a local sentence-transformers model.

Loaded lazily and cached per worker process: the model is ~440MB of weights and
must not be re-read per task, but must also not be imported at module scope,
or the FastAPI web process would pay for it too.
"""

from __future__ import annotations

from typing import TYPE_CHECKING, Optional, Sequence

from api.app.config import get_settings

if TYPE_CHECKING:  # keeps torch out of the web process's import graph
    from sentence_transformers import SentenceTransformer

_model: Optional["SentenceTransformer"] = None


def get_model() -> "SentenceTransformer":
    """Return the process-wide model, loading it on first use."""
    raise NotImplementedError


def embed_texts(texts: Sequence[str]) -> list[list[float]]:
    """Encode chunk texts into normalised vectors of EMBEDDING_DIM.

    Normalised because the pgvector index is `vector_cosine_ops`. bge models
    also want an instruction prefix on *queries* but not on documents, so the
    query-side helper belongs with retrieval, not here.
    """
    raise NotImplementedError


def embedding_fingerprint() -> str:
    """Identity of this embedding configuration, for `paper_stage_runs`.

    Changing the model or its dimension must invalidate every stored vector;
    folding both into the fingerprint makes that automatic.
    """
    raise NotImplementedError
