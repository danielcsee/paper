"""What we keep from an E-utilities summary: a name, and where it came from."""

from typing import Optional

from pydantic import BaseModel


class ConceptName(BaseModel):
    """The names NCBI holds for one concept id."""

    uid: str
    #: Preferred label. The common name when there is one, else the formal one.
    name: str
    #: Kept apart so callers can show both — "domestic cat" and "Felis catus".
    common_name: Optional[str] = None
    scientific_name: Optional[str] = None
