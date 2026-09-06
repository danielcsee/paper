"""Require entities.database — an entity without provenance is not stored

`database` is where a concept came from ("ncbi_gene", "ncbi_mesh"). Without it
we cannot say what an identifier means, and cannot qualify a bare one, so the
row is not worth keeping.

Rejection happens per concept at ingest, never per paper: a paper with one
unattributable annotation is imported without that entity, with a warning.
This migration only cleans up anything already stored.

The CHECK is slightly stronger than NOT NULL: an empty or oddly-shaped database
is as useless as a missing one, and could not serve as an identifier prefix.

Revision ID: e83b1c47a5d9
Revises: d5e2a91c7f03
"""

from typing import Sequence, Union

import sqlalchemy as sa
from alembic import op

revision: str = "e83b1c47a5d9"
down_revision: Union[str, Sequence[str], None] = "d5e2a91c7f03"
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None

CONSTRAINT = "ck_entities_database_format"
DATABASE_RE = r"^[A-Za-z][A-Za-z0-9_]*$"


def upgrade() -> None:
    conn = op.get_bind()
    doomed = conn.exec_driver_sql(
        f"SELECT id, identifier, database FROM entities WHERE database IS NULL"
        f" OR database !~ '{DATABASE_RE}' ORDER BY id"
    ).fetchall()
    if doomed:
        mentions = conn.exec_driver_sql(
            "SELECT count(*) FROM paper_entity_mentions WHERE entity_id = ANY(%(ids)s)",
            {"ids": [row[0] for row in doomed]},
        ).scalar()
        print(
            f"  removing {len(doomed)} entity row(s) with no usable provenance "
            f"and {mentions} dependent mention(s):"
        )
        for row in doomed[:10]:
            print(f"    id={row[0]} identifier={row[1]!r} database={row[2]!r}")
        conn.exec_driver_sql(
            f"DELETE FROM entities WHERE database IS NULL OR database !~ '{DATABASE_RE}'"
        )

    op.alter_column("entities", "database", existing_type=sa.String(64), nullable=False)
    op.create_check_constraint(CONSTRAINT, "entities", f"database ~ '{DATABASE_RE}'")


def downgrade() -> None:
    op.drop_constraint(CONSTRAINT, "entities", type_="check")
    op.alter_column("entities", "database", existing_type=sa.String(64), nullable=True)
