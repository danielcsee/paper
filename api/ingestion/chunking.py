"""Passages -> chunks. Pure functions: no network, no database, no torch.

Chunks are **1:1 with PubTator passages**. PubTator already segments a paper
into paragraph-sized units carrying `section_type` and character offsets, so
there is nothing to pack and no token budget to respect here; a chunk is a
passage. That also means this module needs no tokenizer — when a token count is
wanted it is computed from the text by `embedding.count_tokens`.

Offsets stay in PubTator's document coordinate space, which is what makes
`paper_entity_mentions.chunk_id` resolvable by containment:

    char_start <= mention.char_offset < char_end
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Optional, Sequence

from api.pb_client.models import Passage


@dataclass(frozen=True)
class Chunk:
    """One passage, ready to become a `paper_chunks` row."""

    ordinal: int
    section_type: Optional[str]
    char_start: int
    char_end: int
    text: str


def chunks_from_passages(passages: Sequence[Passage]) -> list[Chunk]:
    """One chunk per passage, renumbered from zero.

    `Passage.ordinal` is the index in the upstream document, which skips the
    REF passages `/pb/paper` filters out. Chunk ordinals must be gapless
    instead, because `uq_paper_chunks_paper_ordinal` is what makes re-ingesting
    a paper idempotent.

    Empty passages are dropped: they would violate `ck_paper_chunks_span`
    (`char_end > char_start`) and carry nothing to embed.
    """
    chunks: list[Chunk] = []
    for passage in passages:
        text = passage.text
        if not text:
            continue
        chunks.append(
            Chunk(
                ordinal=len(chunks),
                section_type=passage.section_type,
                char_start=passage.offset,
                char_end=passage.offset + len(text),
                text=text,
            )
        )
    return chunks


def build_body_text(passages: Sequence[Passage]) -> str:
    """Reconstruct the document so string offsets equal PubTator's offsets.

    Passage offsets are not contiguous — there are gaps between them, and REF
    passages are removed before we get here, leaving larger ones. Concatenating
    would shift every position and silently invalidate all annotation offsets,
    so each passage is written at its own offset and the gaps are padded.

    The result is only for display and highlighting; nothing in the pipeline
    depends on it, since chunks and mentions carry their own offsets.
    """
    if not passages:
        return ""
    extent = max(p.offset + len(p.text) for p in passages)
    # A character list, not bytes: PubTator's offsets are character offsets,
    # verified by slicing annotation spans out of passage text exactly.
    chars = [" "] * extent
    for passage in passages:
        chars[passage.offset : passage.offset + len(passage.text)] = list(passage.text)
    return "".join(chars)


def find_chunk_ordinal(chunks: Sequence[Chunk], offset: int) -> Optional[int]:
    """Ordinal of the chunk containing an absolute offset, or None.

    None is normal rather than exceptional: an annotation inside a passage that
    was filtered out (a REF entry) has nowhere to land, and its mention is
    stored with a null `chunk_id`.
    """
    for chunk in chunks:
        if chunk.char_start <= offset < chunk.char_end:
            return chunk.ordinal
    return None
