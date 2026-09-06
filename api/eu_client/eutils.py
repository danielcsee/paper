"""NCBI E-utilities, used for exactly one job: naming concepts PubTator does not.

PubTator sends `"name": "9606"` for Species and `"name": "4362"` for CellLine —
the identifier again, because it has no label for those types. Verified against
the corpus: every `ncbi_taxonomy` and `cvcl` row arrives that way, while
`ncbi_gene` and `ncbi_mesh` carry real names.

`esummary` resolves the NCBI ones in a single batched call and needs no API key:

    esummary.fcgi?db=taxonomy&id=9685,9606&retmode=json
      9685 -> scientificname "Felis catus",  commonname "domestic cat"
      9606 -> scientificname "Homo sapiens", commonname "human"

Cellosaurus (`CVCL:`) and OMIM are not NCBI and are not resolvable here.
"""

from __future__ import annotations

import logging
from typing import Optional, Sequence

import httpx

from api.eu_client.models import ConceptName
from api.ncbi import http
from api.ncbi.errors import UpstreamError

log = logging.getLogger(__name__)

#: Our `entities.database` value -> the E-utilities database that resolves it.
#: Absent means unresolvable: `cvcl` is Cellosaurus, `omim` needs a key, and
#: `ncbi_mesh` is already named by PubTator.
RESOLVABLE = {"ncbi_taxonomy": "taxonomy", "ncbi_gene": "gene"}

#: ids per request. E-utilities takes several hundred on a GET; well under that
#: keeps the URL short and one failure cheap.
SUMMARY_BATCH_LIMIT = 200


def _name_from(db: str, record: dict) -> Optional[ConceptName]:
    uid = str(record.get("uid") or "").strip()
    if not uid:
        return None

    if db == "taxonomy":
        scientific = (record.get("scientificname") or "").strip() or None
        common = (
            (record.get("commonname") or "").strip()
            or (record.get("genbankcommonname") or "").strip()
            or None
        )
        # Common name first: a reader recognises "domestic cat" faster than
        # "Felis catus", and the formal name is kept alongside either way.
        name = common or scientific
        if not name:
            return None
        return ConceptName(
            uid=uid, name=name, common_name=common, scientific_name=scientific
        )

    # db == "gene": the symbol is the label, the description is the long form.
    symbol = (record.get("name") or "").strip() or None
    description = (record.get("description") or "").strip() or None
    name = symbol or description
    if not name:
        return None
    return ConceptName(uid=uid, name=name, common_name=description, scientific_name=symbol)


class EutilsClient:
    """Read-only, and deliberately narrow: summaries by id, nothing else."""

    def __init__(self, client: httpx.AsyncClient, base_url: str) -> None:
        self._client = client
        self._base_url = base_url.rstrip("/")

    async def names(self, db: str, uids: Sequence[str]) -> dict[str, ConceptName]:
        """uid -> name, for the ids NCBI recognises. Unknown ids are absent.

        Batched: 62 taxon ids are one request, not 62. Ids upstream does not
        know are simply missing from the result rather than raising, since a
        concept we cannot name is not an error — it keeps its fallback label.
        """
        unique = list(dict.fromkeys(str(uid).strip() for uid in uids if str(uid).strip()))
        found: dict[str, ConceptName] = {}

        for start in range(0, len(unique), SUMMARY_BATCH_LIMIT):
            batch = unique[start : start + SUMMARY_BATCH_LIMIT]
            response = await http.get(
                self._client,
                f"{self._base_url}/esummary.fcgi",
                params={"db": db, "id": ",".join(batch), "retmode": "json"},
            )
            if response.status_code != 200:
                raise UpstreamError(
                    f"E-utilities esummary returned HTTP {response.status_code}: "
                    f"{response.text[:200]}"
                )
            try:
                payload = response.json()
            except ValueError as exc:
                raise UpstreamError(f"E-utilities returned non-JSON: {exc}") from exc

            result = payload.get("result") or {}
            for uid in result.get("uids") or []:
                record = result.get(str(uid))
                if not isinstance(record, dict):
                    continue
                concept = _name_from(db, record)
                if concept is not None:
                    found[concept.uid] = concept

        return found
