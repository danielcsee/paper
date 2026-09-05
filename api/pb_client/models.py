"""Response models. These are ours, not NCBI's — upstream fields are normalised
here so callers don't depend on the shape of either service's JSON."""

from typing import Optional

from pydantic import BaseModel, Field

class SearchResult(BaseModel):
    pmid: Optional[int] = None
    pmcid: Optional[str] = None
    title: Optional[str] = None
    journal: Optional[str] = None
    authors: list[str] = Field(default_factory=list)
    date: Optional[str] = None
    doi: Optional[str] = None
    score: Optional[float] = None
    #: Upstream highlight string, with PubTator's inline entity markup intact
    #: (e.g. "@GENE_BRCA1 @GENE_672 @@@<m>BRCA1</m>@@@").
    text_hl: Optional[str] = None
    #: `text_hl` with that markup stripped — safe to render directly.
    snippet: Optional[str] = None


class SearchResponse(BaseModel):
    query: str
    page: int
    page_size: int
    total_results: int
    total_pages: int
    results: list[SearchResult]


# --------------------------------------------------------------------------
# Full paper (PubTator BioC JSON, `full=true`)
# --------------------------------------------------------------------------


class Author(BaseModel):
    surname: Optional[str] = None
    given_names: Optional[str] = None

    @property
    def display(self) -> str:
        return " ".join(p for p in (self.surname, self.given_names) if p)


class Annotation(BaseModel):
    """One grounded (or ungrounded) entity mention.

    `offset` is absolute within the document's reconstructed text — the same
    coordinate space as `Passage.offset` — so a passage always contains its own
    annotations' spans.
    """

    id: Optional[str] = None
    type: Optional[str] = None
    text: Optional[str] = None
    offset: int
    length: int
    #: Prefixed concept id, e.g. "MESH:D002118" or an NCBI Gene number.
    identifier: Optional[str] = None
    #: Bare form of the same id. Upstream types this inconsistently (str for
    #: MeSH, int for taxonomy); always a string here.
    normalized_id: Optional[str] = None
    database: Optional[str] = None
    name: Optional[str] = None
    biotype: Optional[str] = None
    #: False when upstream returned identifier "-". Kept rather than dropped so
    #: the caller decides; ingestion is expected to discard these.
    grounded: bool = True


class Passage(BaseModel):
    """A paragraph-sized unit of the paper, as PubTator segments it."""

    ordinal: int
    #: TITLE, ABSTRACT, INTRO, METHODS, RESULTS, DISCUSS, CONCL, FIG, TABLE,
    #: SUPPL, REF, AUTH_CONT, COMP_INT, ABBR, ACK_FUND, CASE, KEYWORD, APPENDIX.
    section_type: Optional[str] = None
    #: Finer-grained kind: front, abstract, abstract_title_1, title_1,
    #: paragraph, ref, table_caption, ...
    type: Optional[str] = None
    offset: int
    text: str
    annotations: list[Annotation] = Field(default_factory=list)


class Relation(BaseModel):
    """A document-level assertion between two concepts, with polarity.

    Association / Positive_Correlation / Negative_Correlation / Cotreatment /
    Bind. Opposite polarity over the same concept pair across two papers is the
    contradiction signal.
    """

    id: Optional[str] = None
    type: Optional[str] = None
    score: Optional[float] = None
    role1_identifier: Optional[str] = None
    role1_name: Optional[str] = None
    role1_type: Optional[str] = None
    role2_identifier: Optional[str] = None
    role2_name: Optional[str] = None
    role2_type: Optional[str] = None


class Reference(BaseModel):
    """A bibliography entry. `title` is the REF passage text; everything else
    comes from its infons, which is where PubTator puts the structured citation."""

    ordinal: int
    title: Optional[str] = None
    pmid: Optional[str] = None
    doi: Optional[str] = None
    source: Optional[str] = None
    year: Optional[str] = None
    volume: Optional[str] = None
    fpage: Optional[str] = None
    lpage: Optional[str] = None
    authors: list[Author] = Field(default_factory=list)


class PaperResponse(BaseModel):
    pmcid: Optional[str] = None
    pmid: Optional[int] = None
    doi: Optional[str] = None
    title: Optional[str] = None
    journal: Optional[str] = None
    journal_title: Optional[str] = None
    date: Optional[str] = None
    year: Optional[str] = None
    volume: Optional[str] = None
    fpage: Optional[str] = None
    lpage: Optional[str] = None
    authors: list[Author] = Field(default_factory=list)
    #: True when the response carried body sections, not just title+abstract.
    has_full_text: bool = False
    passages: list[Passage] = Field(default_factory=list)
    relations: list[Relation] = Field(default_factory=list)
    references: list[Reference] = Field(default_factory=list)
    #: Counts by section_type across the returned passages.
    section_counts: dict[str, int] = Field(default_factory=dict)
