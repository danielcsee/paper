"""Chunk embeddings via a local sentence-transformers model.

The model is loaded lazily and cached per worker process: ~440MB of weights
that must not be re-read per task, but must also not be imported at module
scope, or the FastAPI web process would pay for torch on startup too.

Token counts are always computed from the text with the model's own tokenizer.
Nothing is estimated, and no count is stored — a stored count is one that can
drift from the text it describes.
"""

from __future__ import annotations

import logging
from typing import TYPE_CHECKING, Optional, Sequence

from api.app.config import get_settings
from api.db.models import EMBEDDING_DIM

if TYPE_CHECKING:  # keeps torch out of the web process's import graph
    from sentence_transformers import SentenceTransformer

log = logging.getLogger(__name__)

_model: Optional["SentenceTransformer"] = None


def get_model() -> "SentenceTransformer":
    """The process-wide model, loaded on first use."""
    global _model
    if _model is None:
        from sentence_transformers import SentenceTransformer

        settings = get_settings()
        name = settings.embedding_model
        log.info(
            "loading embedding model %s (device=%s)", name, settings.embedding_device or "auto"
        )
        _model = SentenceTransformer(name, device=settings.embedding_device)
        dim = _model.get_sentence_embedding_dimension()
        if dim != EMBEDDING_DIM:
            raise RuntimeError(
                f"{name} produces {dim}-d vectors but paper_chunks.embedding is "
                f"{EMBEDDING_DIM}-d; changing the model needs a migration"
            )
    return _model


def max_sequence_tokens() -> int:
    """The model's input limit. Longer chunks are truncated by the encoder."""
    return int(get_model().max_seq_length)


def count_tokens(text: str) -> int:
    """Exact token count for `text`, via the model's own tokenizer."""
    return len(get_model().tokenizer.encode(text, add_special_tokens=True))


#: bge is trained asymmetrically: the instruction goes on the *query*, never on
#: the documents. Ours were embedded bare, which is the matching half of this.
QUERY_INSTRUCTION = "Represent this sentence for searching relevant passages: "


def embed_query(query: str) -> list[float]:
    """Encode a search query, with the instruction prefix bge expects.

    Absolute similarities come out roughly 0.05 lower than for an unprefixed
    query (measured: 0.742 -> 0.688 on the same text), so a threshold tuned
    against one convention does not transfer to the other.
    """
    return embed_texts([QUERY_INSTRUCTION + query.strip()])[0]


def embed_texts(texts: Sequence[str]) -> list[list[float]]:
    """Encode chunk texts into normalised vectors of `EMBEDDING_DIM`.

    Normalised because the pgvector index is `vector_cosine_ops`. Chunks longer
    than the model's limit are truncated by the encoder rather than split; the
    count of those is logged so the loss is visible.

    bge wants an instruction prefix on *queries* but not on documents, so the
    query-side helper belongs with retrieval, not here.
    """
    if not texts:
        return []

    model = get_model()
    limit = max_sequence_tokens()
    truncated = sum(1 for text in texts if count_tokens(text) > limit)
    if truncated:
        log.warning(
            "%d/%d chunks exceed %d tokens and will be truncated by the encoder",
            truncated,
            len(texts),
            limit,
        )

    vectors = model.encode(
        list(texts),
        batch_size=get_settings().embedding_batch_size,
        normalize_embeddings=True,
        convert_to_numpy=True,
        show_progress_bar=False,
    )
    return [vector.tolist() for vector in vectors]


def embedding_fingerprint() -> str:
    """Identity of this embedding configuration, for `paper_stage_runs`.

    Changing the model must invalidate every stored vector; folding the name
    and dimension into the fingerprint makes that automatic rather than a thing
    someone has to remember.
    """
    return f"{get_settings().embedding_model}:{EMBEDDING_DIM}"
