package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/danielcsee/sciterm/testledger/internal/model"
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

type PreviousSymbol struct {
	Key          string
	SemanticHash string
}

func (s *Store) LatestInventory(ctx context.Context) (string, string, error) {
	var id, completed string
	err := s.db.QueryRowContext(ctx, `SELECT id, COALESCE(completed_at, '') FROM inventory_runs
WHERE status='completed' ORDER BY completed_at DESC LIMIT 1`).Scan(&id, &completed)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return id, completed, err
}

func (s *Store) SymbolsForInventory(ctx context.Context, runID string) (map[string]PreviousSymbol, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.symbol_key, sv.semantic_hash
FROM symbol_versions sv JOIN symbols s ON s.id=sv.symbol_id WHERE sv.inventory_run_id=?`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]PreviousSymbol{}
	for rows.Next() {
		var p PreviousSymbol
		if err := rows.Scan(&p.Key, &p.SemanticHash); err != nil {
			return nil, err
		}
		out[p.Key] = p
	}
	return out, rows.Err()
}

func (s *Store) SaveInventory(ctx context.Context, runID, root string, started time.Time, symbols []model.Symbol) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_runs(id, started_at, status, root) VALUES(?,?,?,?)`,
		runID, started.UTC().Format(time.RFC3339Nano), "running", root); err != nil {
		return err
	}
	for _, symbol := range symbols {
		if _, err := tx.ExecContext(ctx, `INSERT INTO symbols(symbol_key, language, qualified_name)
VALUES(?,?,?) ON CONFLICT(symbol_key) DO UPDATE SET language=excluded.language, qualified_name=excluded.qualified_name`,
			symbol.Key(), symbol.Language, symbol.QualifiedName); err != nil {
			return err
		}
		var symbolID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM symbols WHERE symbol_key=?`, symbol.Key()).Scan(&symbolID); err != nil {
			return err
		}
		lines, _ := json.Marshal(symbol.ExecutableLines)
		if _, err := tx.ExecContext(ctx, `INSERT INTO symbol_versions(
symbol_id, inventory_run_id, semantic_hash, signature_hash, body_hash, path, kind, start_line, end_line, executable_lines_json)
VALUES(?,?,?,?,?,?,?,?,?,?)`, symbolID, runID, symbol.SemanticHash, symbol.SignatureHash, symbol.BodyHash,
			symbol.Path, symbol.Kind, symbol.StartLine, symbol.EndLine, string(lines)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE inventory_runs SET status='completed', completed_at=? WHERE id=?`,
		time.Now().UTC().Format(time.RFC3339Nano), runID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ActiveDisposition(ctx context.Context, symbolKey, semanticHash string, now time.Time) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dispositions
WHERE symbol_key=? AND (semantic_hash IS NULL OR semantic_hash=?)
AND (expires_at IS NULL OR expires_at > ?)`, symbolKey, semanticHash, now.UTC().Format(time.RFC3339Nano)).Scan(&count)
	return count > 0, err
}

func (s *Store) AddSkip(ctx context.Context, symbolKey, semanticHash, reason, approvedBy string, expiresAt *time.Time) error {
	var expiry any
	if expiresAt != nil {
		expiry = expiresAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO dispositions(
symbol_key, semantic_hash, disposition, reason, approved_by, created_at, expires_at) VALUES(?,?,?,?,?,?,?)`,
		symbolKey, nullString(semanticHash), "skip", reason, approvedBy,
		time.Now().UTC().Format(time.RFC3339Nano), expiry)
	return err
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

type CoverageSummary struct {
	Count int
	Best  float64
}

func (s *Store) CoverageForSymbol(ctx context.Context, symbolKey, semanticHash string, minimum float64) (CoverageSummary, error) {
	var summary CoverageSummary
	err := s.db.QueryRowContext(ctx, `SELECT
COUNT(DISTINCT CASE WHEN co.line_percent>=? AND tr.outcome='passed' THEN co.test_id END),
COALESCE(MAX(CASE WHEN tr.outcome='passed' THEN co.line_percent ELSE 0 END), 0)
FROM coverage_observations co
JOIN symbols s ON s.id=co.symbol_id
JOIN test_case_results tr ON tr.run_id=co.run_id AND tr.test_id=co.test_id
WHERE s.symbol_key=? AND co.semantic_hash=?`,
		minimum, symbolKey, semanticHash).Scan(&summary.Count, &summary.Best)
	return summary, err
}

func (s *Store) BeginTestRun(ctx context.Context, result model.TestRunResult, inventoryID string, command []string) error {
	commandJSON, _ := json.Marshal(command)
	_, err := s.db.ExecContext(ctx, `INSERT INTO test_runs(
id, inventory_run_id, started_at, status, command_json, artifact_directory)
VALUES(?,?,?,?,?,?)`, result.RunID, nullString(inventoryID), result.StartedAt.UTC().Format(time.RFC3339Nano),
		"running", string(commandJSON), result.ArtifactDirectory)
	return err
}

func (s *Store) FinishTestRun(ctx context.Context, result model.TestRunResult) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE test_runs SET finished_at=?, status=?, exit_code=?, infrastructure_error=? WHERE id=?`,
		result.FinishedAt.UTC().Format(time.RFC3339Nano), result.Status, result.ExitCode,
		nullString(result.InfrastructureErr), result.RunID); err != nil {
		return err
	}
	for _, tc := range result.Cases {
		if _, err := tx.ExecContext(ctx, `INSERT INTO tests(test_key) VALUES(?) ON CONFLICT(test_key) DO NOTHING`, tc.TestKey); err != nil {
			return err
		}
		var testID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM tests WHERE test_key=?`, tc.TestKey).Scan(&testID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO test_case_results(
run_id, test_id, outcome, phase, duration_seconds, failure_category, message, traceback, stdout_excerpt, stderr_excerpt)
VALUES(?,?,?,?,?,?,?,?,?,?)`, result.RunID, testID, tc.Outcome, tc.Phase, tc.DurationSeconds,
			nullString(tc.FailureCategory), nullString(tc.Message), nullString(tc.Traceback),
			nullString(tc.Stdout), nullString(tc.Stderr)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type CoverageObservation struct {
	TestKey         string
	SymbolKey       string
	SemanticHash    string
	ExecutedLines   []int
	ExecutedCount   int
	ExecutableCount int
	LinePercent     float64
}

type CoverageDetail struct {
	SymbolKey       string
	SemanticHash    string
	MissingLines    []int
	MissingBranches []model.Branch
}

func (s *Store) SaveCoverage(ctx context.Context, runID string, observations []CoverageObservation) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, observation := range observations {
		var testID, symbolID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM tests WHERE test_key=?`, observation.TestKey).Scan(&testID); err != nil {
			continue
		}
		if err := tx.QueryRowContext(ctx, `SELECT id FROM symbols WHERE symbol_key=?`, observation.SymbolKey).Scan(&symbolID); err != nil {
			continue
		}
		lines, _ := json.Marshal(observation.ExecutedLines)
		_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO coverage_observations(
run_id, test_id, symbol_id, semantic_hash, executed_lines_json, executed_count, executable_count, line_percent)
VALUES(?,?,?,?,?,?,?,?)`, runID, testID, symbolID, observation.SemanticHash, string(lines), observation.ExecutedCount,
			observation.ExecutableCount, observation.LinePercent)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SaveCoverageDetails(ctx context.Context, runID string, details []CoverageDetail) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, detail := range details {
		var symbolID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM symbols WHERE symbol_key=?`, detail.SymbolKey).Scan(&symbolID); err != nil {
			continue
		}
		lines, _ := json.Marshal(detail.MissingLines)
		branches, _ := json.Marshal(detail.MissingBranches)
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO symbol_coverage_details(run_id,symbol_id,semantic_hash,missing_lines_json,missing_branches_json) VALUES(?,?,?,?,?)`, runID, symbolID, detail.SemanticHash, string(lines), string(branches)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) CoverageDetailForSymbol(ctx context.Context, symbolKey, semanticHash string) (CoverageDetail, error) {
	var detail CoverageDetail
	var lines, branches string
	err := s.db.QueryRowContext(ctx, `SELECT d.missing_lines_json,d.missing_branches_json
FROM symbol_coverage_details d JOIN symbols s ON s.id=d.symbol_id
JOIN test_runs tr ON tr.id=d.run_id
WHERE s.symbol_key=? AND d.semantic_hash=? ORDER BY tr.started_at DESC LIMIT 1`, symbolKey, semanticHash).Scan(&lines, &branches)
	if err == sql.ErrNoRows {
		return detail, nil
	}
	if err != nil {
		return detail, err
	}
	detail.SymbolKey, detail.SemanticHash = symbolKey, semanticHash
	_ = json.Unmarshal([]byte(lines), &detail.MissingLines)
	_ = json.Unmarshal([]byte(branches), &detail.MissingBranches)
	return detail, nil
}

func (s *Store) CoveringTestsForVersion(ctx context.Context, symbolKey, semanticHash string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT t.test_key
FROM coverage_observations co JOIN symbols s ON s.id=co.symbol_id
JOIN tests t ON t.id=co.test_id
JOIN test_case_results r ON r.run_id=co.run_id AND r.test_id=co.test_id
WHERE s.symbol_key=? AND co.semantic_hash=? AND r.outcome='passed' ORDER BY t.test_key`, symbolKey, semanticHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		result = append(result, key)
	}
	return result, rows.Err()
}

func (s *Store) SaveMutationRun(ctx context.Context, run model.MutationRun, command []string) error {
	commandJSON, _ := json.Marshal(command)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO mutation_runs(id,language,started_at,finished_at,status,exit_code,command_json,artifact_directory,infrastructure_error) VALUES(?,?,?,?,?,?,?,?,?)`, run.RunID, run.Language, run.StartedAt.UTC().Format(time.RFC3339Nano), run.FinishedAt.UTC().Format(time.RFC3339Nano), run.Status, run.ExitCode, string(commandJSON), run.ArtifactDirectory, nullString(run.InfrastructureErr)); err != nil {
		return err
	}
	for _, result := range run.Results {
		if _, err := tx.ExecContext(ctx, `INSERT INTO mutation_results(run_id,symbol_key,operator,status,test_key,detail) VALUES(?,?,?,?,?,?)`, run.RunID, result.SymbolKey, result.Operator, result.Status, nullString(result.TestKey), nullString(result.Detail)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) AddArtifact(ctx context.Context, runID, kind, path, hash string, bytes int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO artifacts(run_id, kind, path, sha256, byte_count) VALUES(?,?,?,?,?)`,
		runID, kind, path, hash, bytes)
	return err
}

func (s *Store) LatestTestRun(ctx context.Context) (*model.TestRunResult, error) {
	var r model.TestRunResult
	var started, finished string
	err := s.db.QueryRowContext(ctx, `SELECT id, status, started_at, COALESCE(finished_at,''), COALESCE(exit_code,-1),
artifact_directory, COALESCE(infrastructure_error,'') FROM test_runs ORDER BY started_at DESC LIMIT 1`).Scan(
		&r.RunID, &r.Status, &started, &finished, &r.ExitCode, &r.ArtifactDirectory, &r.InfrastructureErr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
	r.FinishedAt, _ = time.Parse(time.RFC3339Nano, finished)
	rows, err := s.db.QueryContext(ctx, `SELECT outcome, COUNT(*) FROM test_case_results WHERE run_id=? GROUP BY outcome`, r.RunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var outcome string
		var count int
		if err := rows.Scan(&outcome, &count); err != nil {
			return nil, err
		}
		switch outcome {
		case "passed":
			r.Passed = count
		case "failed":
			r.Failed = count
		case "skipped":
			r.Skipped = count
		default:
			r.Errors += count
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM coverage_observations WHERE run_id=?`, r.RunID).Scan(&r.CoverageMappings); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) CountActiveSkips(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dispositions WHERE expires_at IS NULL OR expires_at > ?`,
		time.Now().UTC().Format(time.RFC3339Nano)).Scan(&count)
	return count, err
}
