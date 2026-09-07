"""Project every already-imported paper into the graph and close its ledger.

    python -m api.ingestion.backfill_graph
    python -m api.ingestion.backfill_graph --dry-run

A one-off for the corpus as it stood before the `graph` stage existed. New
papers get there through the pipeline; this catches up the ones that were
imported when "fully imported" still meant `embed`.

One `load_all` pass rather than a per-paper loop: the bulk projection is the
same code with no pmid filter, and it is one traversal of each table instead of
thirty. The ledger rows are written afterwards, so a failed load leaves them
unmarked and the backfill can simply be re-run.
"""

from __future__ import annotations

import argparse
import logging

from sqlalchemy import text

from api.db import session_scope
from api.db.models import PaperStageRun
from api.graph import export as graph
from api.ingestion.tasks import GRAPH_VERSION

log = logging.getLogger(__name__)

#: Papers that finished the stage before this one but have no graph row yet.
PENDING_SQL = """
    SELECT p.id, p.pmid
    FROM papers p
    JOIN paper_stage_runs e ON e.paper_id = p.id AND e.stage = 'embed' AND e.status = 'done'
    LEFT JOIN paper_stage_runs g ON g.paper_id = p.id AND g.stage = 'graph'
    WHERE g.status IS DISTINCT FROM 'done'
    ORDER BY p.id
"""


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dry-run", action="store_true", help="report, change nothing")
    args = parser.parse_args()
    logging.basicConfig(level=logging.INFO, format="%(message)s")

    with session_scope() as session:
        pending = session.execute(text(PENDING_SQL)).all()
    if not pending:
        print("nothing to do: every imported paper is already in the graph")
        return 0
    print(f"{len(pending)} paper(s) embedded but not yet in the graph")

    if args.dry_run:
        for paper_id, pmid in pending[:10]:
            print(f"  would project paper {paper_id} (pmid {pmid})")
        if len(pending) > 10:
            print(f"  ... and {len(pending) - 10} more")
        return 0

    with session_scope() as session:
        counts = graph.load_all(session)
    print(f"projected the corpus: {counts._asdict()}")

    with session_scope() as session:
        for paper_id, _ in pending:
            session.execute(
                text(
                    """
                    INSERT INTO paper_stage_runs
                        (paper_id, stage, status, attempt, finished_at, input_fingerprint)
                    VALUES (:paper_id, 'graph', 'done', 1, now(), :fingerprint)
                    ON CONFLICT (paper_id, stage) DO UPDATE
                    SET status = 'done', error = NULL, finished_at = now(),
                        input_fingerprint = :fingerprint
                    """
                ),
                {"paper_id": paper_id, "fingerprint": GRAPH_VERSION},
            )
    print(f"marked {len(pending)} paper(s) complete at stage '{PaperStageRun.FINAL_STAGE}'")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
