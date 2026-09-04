"""Passages -> chunks. Pure functions: no network, no database.

Rules settled earlier:

* Drop REF passages — they are bibliography entries, already structured in
  `paper_references`, and would pollute the embedding corpus.
* Pack consecutive passages up to `chunk_max_tokens`, never crossing a
  `section_type` boundary.
* Offsets stay in PubTator's document coordinate space, so a mention belongs to
  the chunk where `char_start <= mention.char_offset < char_end`.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Sequence

from api.pb_client.models import Passage


@dataclass(frozen=True)
class Chunk:
    ordinal: int
    section_type: str | None
    char_start: int
    char_end: int
    text: str
    token_count: int


def estimate_tokens(text: str) -> int:
    """Cheap token estimate used for packing."""
    raise NotImplementedError


def build_body_text(passages: Sequence[Passage]) -> str:
    """Reconstruct the document so string offsets equal PubTator's offsets.

    Passage offsets are not contiguous — there are gaps between them. Naive
    concatenation would shift every position and silently invalidate all
    annotation offsets, so each passage is written at its own offset and the
    gaps are padded.
    """
    raise NotImplementedError


def pack_passages(passages: Sequence[Passage], max_tokens: int) -> list[Chunk]:
    """Group passages into chunks under the rules above."""
    raise NotImplementedError


def resolve_chunk_for_offset(chunks: Sequence[Chunk], offset: int) -> Chunk | None:
    """Find the chunk containing an annotation offset, or None if it was
    dropped (a mention inside a REF passage, for instance)."""
    raise NotImplementedError
