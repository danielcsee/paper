"""drop token_count, rename ingest stages

Revision ID: 8c430ca9be25
Revises: 738f0cf2e8f0
Create Date: 2026-09-04 08:15:13.620798
"""

from typing import Sequence, Union

from alembic import op
import sqlalchemy as sa


revision: str = '8c430ca9be25'
down_revision: Union[str, None] = '738f0cf2e8f0'
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    # Chunks are 1:1 with PubTator passages, so nothing packs by token budget
    # any more. A token count is recomputed from the text when needed, with the
    # model's own tokenizer, rather than stored and allowed to drift.
    op.drop_column('paper_chunks', 'token_count')

    # Alembic does not autogenerate CHECK constraint changes. The pipeline
    # collapsed to two tasks, so the stage vocabulary changes with it:
    #   fetch/chunk/entities -> ingest, and embed stays. 'graph' is allowed
    #   ahead of the Neo4j step so adding it needs no further migration.
    op.drop_constraint("ck_stage_runs_stage", "paper_stage_runs", type_="check")
    op.create_check_constraint(
        "ck_stage_runs_stage", "paper_stage_runs", "stage in ('ingest','embed','graph')"
    )


def downgrade() -> None:
    op.drop_constraint("ck_stage_runs_stage", "paper_stage_runs", type_="check")
    op.create_check_constraint(
        "ck_stage_runs_stage",
        "paper_stage_runs",
        "stage in ('fetch','chunk','embed','entities','graph')",
    )
    op.add_column(
        'paper_chunks',
        sa.Column('token_count', sa.INTEGER(), autoincrement=False, nullable=True),
    )
