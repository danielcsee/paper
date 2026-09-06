"""Qualify bare entity identifiers with their source database

A bare number is unique only *within* one NCBI database. Gene 9606 and taxon
9606 are different concepts, and `entities.identifier` is unique, so the two
would collide on one row. Prefixing with the database the id came from --
"672" -> "ncbi_gene:672" -- makes the key globally unique.

Only colon-less identifiers are touched. MESH:D010051 and CVCL:0031 already
carry their namespace and are left exactly as they are.

The CHECK constraint is widened first: its prefix pattern was
[A-Za-z][A-Za-z0-9]* with no underscore, so the very ids this migration
creates would have violated it.

Revision ID: d5e2a91c7f03
Revises: c41f7b9d20ae
"""

from typing import Sequence, Union

from alembic import op

revision: str = "d5e2a91c7f03"
down_revision: Union[str, Sequence[str], None] = "c41f7b9d20ae"
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None

CONSTRAINT = "ck_entities_identifier_format"
OLD_RE = r"^([0-9]+|[A-Za-z][A-Za-z0-9]*:[A-Za-z0-9._\-]+)$"
NEW_RE = r"^([0-9]+|[A-Za-z][A-Za-z0-9_]*:[A-Za-z0-9._\-]+)$"


def upgrade() -> None:
    conn = op.get_bind()

    op.drop_constraint(CONSTRAINT, "entities", type_="check")
    op.create_check_constraint(CONSTRAINT, "entities", f"identifier ~ '{NEW_RE}'")

    # Only rows whose database we know can be qualified. Any left bare stay
    # legal and keep working; they are reported rather than dropped.
    rows = conn.exec_driver_sql(
        "SELECT id, identifier, database FROM entities"
        " WHERE identifier !~ ':' ORDER BY id"
    ).fetchall()
    qualifiable = [r for r in rows if r[2]]
    if qualifiable:
        print(f"  qualifying {len(qualifiable)} bare identifier(s), e.g.")
        for row in qualifiable[:3]:
            print(f"    id={row[0]}  {row[1]} -> {row[2]}:{row[1]}")
        conn.exec_driver_sql(
            "UPDATE entities SET identifier = database || ':' || identifier"
            " WHERE identifier !~ ':' AND database IS NOT NULL AND database <> ''"
        )
    orphans = [r for r in rows if not r[2]]
    if orphans:
        print(f"  {len(orphans)} identifier(s) left bare -- no source database recorded:")
        for row in orphans:
            print(f"    id={row[0]} identifier={row[1]!r}")


def downgrade() -> None:
    # Strip the database prefix back off the ids this migration added. Only
    # those: an id whose prefix is not its own `database` column was never bare.
    conn = op.get_bind()
    conn.exec_driver_sql(
        "UPDATE entities SET identifier = split_part(identifier, ':', 2)"
        " WHERE database IS NOT NULL AND identifier = database || ':' || split_part(identifier, ':', 2)"
    )
    op.drop_constraint(CONSTRAINT, "entities", type_="check")
    op.create_check_constraint(CONSTRAINT, "entities", f"identifier ~ '{OLD_RE}'")
