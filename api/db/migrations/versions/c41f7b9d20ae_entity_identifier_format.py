"""Require entities.identifier to be a well-formed NCBI concept id

Two shapes are legal, because those are the two NCBI produces:

    672, 9606                bare number -- NCBI Gene, NCBI Taxonomy
    MESH:D065627, CVCL:M023  prefixed -- MeSH, Cellosaurus, OMIM

The suffix is alphanumeric rather than numeric on purpose. MeSH ids are a
letter followed by digits and were 83% of the table when this was written, so a
`PREFIX:number` rule would have rejected almost every real row.

Blank, whitespace-only and NULL are all excluded by the pattern, which is the
defect this closes: `_to_annotation` grounded on `identifier != "-"`, and
`bool(" ")` is True in Python, so a whitespace identifier reached the table.

Revision ID: c41f7b9d20ae
Revises: a0ca3165ecb2
"""

from typing import Sequence, Union

from alembic import op

revision: str = "c41f7b9d20ae"
down_revision: Union[str, Sequence[str], None] = "a0ca3165ecb2"
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None

IDENTIFIER_RE = r"^([0-9]+|[A-Za-z][A-Za-z0-9]*:[A-Za-z0-9._\-]+)$"
CONSTRAINT = "ck_entities_identifier_format"


def upgrade() -> None:
    # A CHECK cannot be added over violating rows, so they go first. This
    # DELETEs: entity rows that fail the pattern carry no usable concept, and
    # paper_entity_mentions.entity_id is ON DELETE CASCADE, so their mentions
    # go with them. Both counts are reported rather than done silently.
    conn = op.get_bind()
    doomed = conn.exec_driver_sql(
        f"SELECT id, identifier FROM entities WHERE identifier !~ '{IDENTIFIER_RE}'"
    ).fetchall()
    if doomed:
        ids = tuple(row[0] for row in doomed)
        mentions = conn.exec_driver_sql(
            "SELECT count(*) FROM paper_entity_mentions WHERE entity_id = ANY(%(ids)s)",
            {"ids": list(ids)},
        ).scalar()
        print(
            f"  removing {len(doomed)} malformed entity row(s) "
            f"and {mentions} dependent mention(s):"
        )
        for row in doomed:
            print(f"    id={row[0]} identifier={row[1]!r}")
        conn.exec_driver_sql(
            f"DELETE FROM entities WHERE identifier !~ '{IDENTIFIER_RE}'"
        )

    op.create_check_constraint(CONSTRAINT, "entities", f"identifier ~ '{IDENTIFIER_RE}'")


def downgrade() -> None:
    # Only the constraint comes back off. The deleted rows were malformed and
    # are re-derivable by re-importing their papers.
    op.drop_constraint(CONSTRAINT, "entities", type_="check")
