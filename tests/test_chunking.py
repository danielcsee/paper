"""`api.ingestion.chunking` — passages become chunks without moving offsets.

Offsets stay in PubTator's document coordinate space, because
`paper_entity_mentions.chunk_id` is resolved by containment against them.
"""

from __future__ import annotations

from typing import Optional

from api.ingestion.chunking import (
    build_body_text,
    chunks_from_passages,
    find_chunk_ordinal,
)
from api.pb_client.models import Passage


def _passage(
    ordinal: int,
    offset: int,
    text: str,
    section_type: Optional[str] = None,
    type_: Optional[str] = None,
) -> Passage:
    return Passage(
        ordinal=ordinal,
        offset=offset,
        text=text,
        section_type=section_type,
        type=type_,
    )


def test_chunks_from_passages_renumbers_gaplessly() -> None:
    """Upstream ordinals skip filtered REF passages; chunk ordinals must not.

    `uq_paper_chunks_paper_ordinal` is what makes re-ingesting a paper
    idempotent, so a gap in the sequence is a constraint violation waiting.
    """
    passages = [
        _passage(0, 0, "Title", section_type="TITLE", type_="title_1"),
        _passage(4, 20, "Abstract body", section_type="ABSTRACT", type_="abstract"),
        _passage(9, 60, "Methods body", section_type="METHODS", type_="paragraph"),
    ]

    chunks = chunks_from_passages(passages)

    assert [c.ordinal for c in chunks] == [0, 1, 2]
    assert [c.section_type for c in chunks] == ["TITLE", "ABSTRACT", "METHODS"]
    assert [c.chunk_type for c in chunks] == ["title_1", "abstract", "paragraph"]


def test_chunks_from_passages_drops_empty_passages() -> None:
    """An empty passage would violate `ck_paper_chunks_span` and embeds nothing."""
    passages = [
        _passage(0, 0, "Kept"),
        _passage(1, 10, ""),
        _passage(2, 20, "Also kept"),
    ]

    chunks = chunks_from_passages(passages)

    assert [c.text for c in chunks] == ["Kept", "Also kept"]
    # Still gapless after the drop.
    assert [c.ordinal for c in chunks] == [0, 1]


def test_chunks_from_passages_span_matches_text_length() -> None:
    """char_end is exclusive and derived from the text, never from `length`."""
    chunks = chunks_from_passages([_passage(0, 100, "abcde")])

    assert (chunks[0].char_start, chunks[0].char_end) == (100, 105)
    assert chunks[0].char_end > chunks[0].char_start


def test_chunks_from_passages_empty_input() -> None:
    assert chunks_from_passages([]) == []


def test_build_body_text_preserves_pubtator_offsets() -> None:
    """Each passage must be sliceable back out at its own absolute offset.

    Concatenating instead would shift every position and silently invalidate
    all annotation offsets.
    """
    passages = [_passage(0, 0, "Title"), _passage(1, 20, "Body text")]

    body = build_body_text(passages)

    for passage in passages:
        start = passage.offset
        assert body[start : start + len(passage.text)] == passage.text


def test_build_body_text_pads_gaps_with_spaces() -> None:
    """Gaps come from filtered REF passages and are padded, not closed."""
    body = build_body_text([_passage(0, 0, "ab"), _passage(1, 5, "cd")])

    assert body == "ab   cd"
    assert len(body) == 7


def test_build_body_text_empty_input() -> None:
    assert build_body_text([]) == ""


def test_find_chunk_ordinal_containment_is_half_open() -> None:
    """char_start is inclusive, char_end exclusive — the containment rule
    `char_start <= mention.char_offset < char_end`."""
    chunks = chunks_from_passages([_passage(0, 0, "abcde"), _passage(1, 10, "fghij")])

    assert find_chunk_ordinal(chunks, 0) == 0
    assert find_chunk_ordinal(chunks, 4) == 0
    # The boundary belongs to neither chunk's interior: 5 is past the first.
    assert find_chunk_ordinal(chunks, 5) is None
    assert find_chunk_ordinal(chunks, 10) == 1
    assert find_chunk_ordinal(chunks, 14) == 1


def test_find_chunk_ordinal_returns_none_for_filtered_span() -> None:
    """None is normal: an annotation inside a dropped REF passage has nowhere
    to land, and its mention is stored with a null `chunk_id`."""
    chunks = chunks_from_passages([_passage(0, 0, "abcde")])

    assert find_chunk_ordinal(chunks, 99) is None
    assert find_chunk_ordinal([], 0) is None
