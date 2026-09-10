// The SQLite ledger. WAL mode, immutable run identifiers, and normalized
// summaries beside content-hashed artifacts on disk.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;

CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS inventory_runs (
  id TEXT PRIMARY KEY,
  started_at TEXT NOT NULL,
  completed_at TEXT,
  status TEXT NOT NULL,
  root TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS symbols (
  id INTEGER PRIMARY KEY,
  symbol_key TEXT NOT NULL UNIQUE,
  language TEXT NOT NULL,
  qualified_name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS symbol_versions (
  id INTEGER PRIMARY KEY,
  symbol_id INTEGER NOT NULL REFERENCES symbols(id),
  inventory_run_id TEXT NOT NULL REFERENCES inventory_runs(id),
  semantic_hash TEXT NOT NULL,
  signature_hash TEXT NOT NULL,
  body_hash TEXT NOT NULL,
  path TEXT NOT NULL,
  kind TEXT NOT NULL,
  start_line INTEGER NOT NULL,
  end_line INTEGER NOT NULL,
  executable_lines_json TEXT NOT NULL,
  UNIQUE(symbol_id, inventory_run_id)
);
CREATE INDEX IF NOT EXISTS symbol_versions_hash_idx
  ON symbol_versions(symbol_id, semantic_hash);

CREATE TABLE IF NOT EXISTS dispositions (
  id INTEGER PRIMARY KEY,
  symbol_key TEXT NOT NULL,
  semantic_hash TEXT,
  disposition TEXT NOT NULL CHECK(disposition IN ('skip', 'excluded', 'generated', 'abstract')),
  reason TEXT NOT NULL,
  approved_by TEXT NOT NULL DEFAULT 'human',
  created_at TEXT NOT NULL,
  expires_at TEXT
);
CREATE INDEX IF NOT EXISTS dispositions_symbol_idx ON dispositions(symbol_key);

CREATE TABLE IF NOT EXISTS test_runs (
  id TEXT PRIMARY KEY,
  inventory_run_id TEXT REFERENCES inventory_runs(id),
  started_at TEXT NOT NULL,
  finished_at TEXT,
  status TEXT NOT NULL,
  exit_code INTEGER,
  command_json TEXT NOT NULL,
  artifact_directory TEXT NOT NULL,
  infrastructure_error TEXT
);

CREATE TABLE IF NOT EXISTS tests (
  id INTEGER PRIMARY KEY,
  test_key TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS test_case_results (
  id INTEGER PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES test_runs(id),
  test_id INTEGER NOT NULL REFERENCES tests(id),
  outcome TEXT NOT NULL,
  phase TEXT NOT NULL,
  duration_seconds REAL NOT NULL DEFAULT 0,
  failure_category TEXT,
  message TEXT,
  traceback TEXT,
  stdout_excerpt TEXT,
  stderr_excerpt TEXT
);
CREATE INDEX IF NOT EXISTS test_case_results_run_idx ON test_case_results(run_id);

CREATE TABLE IF NOT EXISTS coverage_observations (
  id INTEGER PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES test_runs(id),
  test_id INTEGER NOT NULL REFERENCES tests(id),
  symbol_id INTEGER NOT NULL REFERENCES symbols(id),
  semantic_hash TEXT NOT NULL,
  executed_lines_json TEXT NOT NULL,
  executed_count INTEGER NOT NULL,
  executable_count INTEGER NOT NULL,
  line_percent REAL NOT NULL,
  UNIQUE(run_id, test_id, symbol_id)
);
CREATE INDEX IF NOT EXISTS coverage_symbol_hash_idx
  ON coverage_observations(symbol_id, semantic_hash, line_percent);

CREATE TABLE IF NOT EXISTS symbol_coverage_details (
  run_id TEXT NOT NULL REFERENCES test_runs(id),
  symbol_id INTEGER NOT NULL REFERENCES symbols(id),
  semantic_hash TEXT NOT NULL,
  missing_lines_json TEXT NOT NULL,
  missing_branches_json TEXT NOT NULL,
  PRIMARY KEY(run_id, symbol_id)
);
CREATE INDEX IF NOT EXISTS coverage_details_symbol_idx
  ON symbol_coverage_details(symbol_id, semantic_hash);

CREATE TABLE IF NOT EXISTS artifacts (
  id INTEGER PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES test_runs(id),
  kind TEXT NOT NULL,
  path TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  byte_count INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS test_proposals (
  id TEXT PRIMARY KEY,
  status TEXT NOT NULL CHECK(status IN ('proposed', 'approved', 'rejected', 'implemented', 'verified', 'obsolete')),
  rationale TEXT NOT NULL,
  cases_json TEXT NOT NULL,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS proposal_targets (
  proposal_id TEXT NOT NULL REFERENCES test_proposals(id),
  symbol_key TEXT NOT NULL,
  semantic_hash TEXT NOT NULL,
  PRIMARY KEY(proposal_id, symbol_key)
);

CREATE TABLE IF NOT EXISTS proposal_decisions (
  id INTEGER PRIMARY KEY,
  proposal_id TEXT NOT NULL REFERENCES test_proposals(id),
  decision TEXT NOT NULL CHECK(decision IN ('approved', 'rejected')),
  decided_by TEXT NOT NULL,
  reason TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS intended_test_links (
  proposal_id TEXT NOT NULL REFERENCES test_proposals(id),
  symbol_key TEXT NOT NULL,
  test_key TEXT NOT NULL,
  verified_run_id TEXT REFERENCES test_runs(id),
  verified_at TEXT,
  PRIMARY KEY(proposal_id, symbol_key, test_key)
);

CREATE TABLE IF NOT EXISTS failure_diagnoses (
  id INTEGER PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES test_runs(id),
  test_id INTEGER NOT NULL REFERENCES tests(id),
  category TEXT NOT NULL,
  explanation TEXT NOT NULL,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS failure_diagnoses_result_idx ON failure_diagnoses(run_id,test_id,id);

CREATE TABLE IF NOT EXISTS async_jobs (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('queued', 'running', 'completed', 'failed', 'interrupted')),
  arguments_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  result_run_id TEXT REFERENCES test_runs(id),
  error TEXT
);
CREATE INDEX IF NOT EXISTS async_jobs_status_idx ON async_jobs(status, created_at);

CREATE TABLE IF NOT EXISTS mutation_runs (
  id TEXT PRIMARY KEY,
  language TEXT NOT NULL,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  status TEXT NOT NULL,
  exit_code INTEGER,
  command_json TEXT NOT NULL,
  artifact_directory TEXT NOT NULL,
  infrastructure_error TEXT
);

CREATE TABLE IF NOT EXISTS mutation_results (
  id INTEGER PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES mutation_runs(id),
  symbol_key TEXT NOT NULL,
  operator TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('killed','survived','timeout','error','skipped')),
  test_key TEXT,
  detail TEXT
);
CREATE INDEX IF NOT EXISTS mutation_results_symbol_idx ON mutation_results(symbol_key, status);

INSERT OR IGNORE INTO schema_migrations(version, applied_at)
VALUES (1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
INSERT OR IGNORE INTO schema_migrations(version, applied_at)
VALUES (2, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
INSERT OR IGNORE INTO schema_migrations(version, applied_at)
VALUES (3, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
`

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize sqlite: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
