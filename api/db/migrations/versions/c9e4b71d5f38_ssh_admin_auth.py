"""admin_challenges, and at most one admin user

Revision ID: c9e4b71d5f38
Revises: b6d1f0a94c22
Create Date: 2026-09-08 07:10:00.000000

The admin endpoints stop taking a bearer token and start taking an SSH
signature over a single-use nonce, so nonces need somewhere to live. The
partial unique index makes "only one admin may exist" a property of the table.
"""

from typing import Sequence, Union

import sqlalchemy as sa
from alembic import op

revision: str = "c9e4b71d5f38"
down_revision: Union[str, Sequence[str], None] = "b6d1f0a94c22"
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    op.create_table(
        "admin_challenges",
        sa.Column("id", sa.BigInteger(), autoincrement=True, nullable=False),
        sa.Column("nonce", sa.String(length=64), nullable=False),
        sa.Column("action", sa.String(length=32), nullable=False),
        sa.Column(
            "created_at",
            sa.DateTime(timezone=True),
            server_default=sa.text("now()"),
            nullable=False,
        ),
        sa.Column("expires_at", sa.DateTime(timezone=True), nullable=False),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("nonce"),
    )
    op.create_index("ix_admin_challenges_expires_at", "admin_challenges", ["expires_at"])

    # Unique on a column restricted to the rows where it is true: a second
    # admin row would index the same value `true` and collide.
    op.create_index(
        "uq_users_single_admin",
        "users",
        ["is_admin"],
        unique=True,
        postgresql_where=sa.text("is_admin"),
    )


def downgrade() -> None:
    op.drop_index("uq_users_single_admin", table_name="users")
    op.drop_index("ix_admin_challenges_expires_at", table_name="admin_challenges")
    op.drop_table("admin_challenges")
