// Test runs and their case results, plus the artifacts each run leaves
// behind.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/model"
	_ "modernc.org/sqlite"
)

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

func (s *Store) TestRun(ctx context.Context, runID string, limit, offset int, failuresOnly bool) (model.TestRunResult, model.Page[model.TestCaseResult], error) {
	var result model.TestRunResult
	var started, finished string
	err := s.db.QueryRowContext(ctx, `SELECT id,status,started_at,COALESCE(finished_at,''),COALESCE(exit_code,-1),artifact_directory,COALESCE(infrastructure_error,'')
FROM test_runs WHERE id=?`, runID).Scan(&result.RunID, &result.Status, &started, &finished, &result.ExitCode, &result.ArtifactDirectory, &result.InfrastructureErr)
	if err != nil {
		return result, model.Page[model.TestCaseResult]{}, err
	}
	result.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
	result.FinishedAt, _ = time.Parse(time.RFC3339Nano, finished)
	countRows, err := s.db.QueryContext(ctx, `SELECT outcome,COUNT(*) FROM test_case_results WHERE run_id=? GROUP BY outcome`, runID)
	if err != nil {
		return result, model.Page[model.TestCaseResult]{}, err
	}
	for countRows.Next() {
		var outcome string
		var count int
		if err := countRows.Scan(&outcome, &count); err != nil {
			countRows.Close()
			return result, model.Page[model.TestCaseResult]{}, err
		}
		switch outcome {
		case "passed":
			result.Passed = count
		case "failed":
			result.Failed = count
		case "skipped":
			result.Skipped = count
		default:
			result.Errors += count
		}
	}
	if err := countRows.Close(); err != nil {
		return result, model.Page[model.TestCaseResult]{}, err
	}
	where := " WHERE tr.run_id=?"
	if failuresOnly {
		where += " AND tr.outcome NOT IN ('passed','skipped')"
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM test_case_results tr`+where, runID).Scan(&total); err != nil {
		return result, model.Page[model.TestCaseResult]{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT t.test_key,tr.outcome,tr.phase,tr.duration_seconds,COALESCE(tr.failure_category,''),COALESCE(tr.message,''),COALESCE(tr.traceback,''),COALESCE(tr.stdout_excerpt,''),COALESCE(tr.stderr_excerpt,'')
FROM test_case_results tr JOIN tests t ON t.id=tr.test_id`+where+` ORDER BY t.test_key LIMIT ? OFFSET ?`, runID, limit, offset)
	if err != nil {
		return result, model.Page[model.TestCaseResult]{}, err
	}
	page := model.Page[model.TestCaseResult]{Items: []model.TestCaseResult{}, Total: total}
	for rows.Next() {
		var tc model.TestCaseResult
		if err := rows.Scan(&tc.TestKey, &tc.Outcome, &tc.Phase, &tc.DurationSeconds, &tc.FailureCategory, &tc.Message, &tc.Traceback, &tc.Stdout, &tc.Stderr); err != nil {
			rows.Close()
			return result, page, err
		}
		page.Items = append(page.Items, tc)
	}
	if err := rows.Close(); err != nil {
		return result, page, err
	}
	if offset+len(page.Items) < total {
		page.NextCursor = offset + len(page.Items)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM coverage_observations WHERE run_id=?`, runID).Scan(&result.CoverageMappings); err != nil {
		return result, page, err
	}
	return result, page, nil
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
