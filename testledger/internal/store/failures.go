// Failure context, and the append-only agent diagnoses recorded beside it.
// A diagnosis never overwrites runner evidence.
package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/danielcsee/sciterm/testledger/internal/model"
	_ "modernc.org/sqlite"
)

func (s *Store) FailureContext(ctx context.Context, runID, testKey string) (model.FailureContext, error) {
	var result model.TestCaseResult
	err := s.db.QueryRowContext(ctx, `SELECT t.test_key,tr.outcome,tr.phase,tr.duration_seconds,COALESCE(tr.failure_category,''),COALESCE(tr.message,''),COALESCE(tr.traceback,''),COALESCE(tr.stdout_excerpt,''),COALESCE(tr.stderr_excerpt,'')
FROM test_case_results tr JOIN tests t ON t.id=tr.test_id WHERE tr.run_id=? AND t.test_key=? ORDER BY tr.id DESC LIMIT 1`, runID, testKey).Scan(&result.TestKey, &result.Outcome, &result.Phase, &result.DurationSeconds, &result.FailureCategory, &result.Message, &result.Traceback, &result.Stdout, &result.Stderr)
	if err != nil {
		return model.FailureContext{}, err
	}
	contextResult := model.FailureContext{Result: result}
	var diagnosis model.FailureDiagnosis
	err = s.db.QueryRowContext(ctx, `SELECT fd.run_id,t.test_key,fd.category,fd.explanation,fd.created_by,fd.created_at
FROM failure_diagnoses fd JOIN tests t ON t.id=fd.test_id WHERE fd.run_id=? AND t.test_key=? ORDER BY fd.id DESC LIMIT 1`, runID, testKey).Scan(&diagnosis.RunID, &diagnosis.TestKey, &diagnosis.Category, &diagnosis.Explanation, &diagnosis.CreatedBy, &diagnosis.CreatedAt)
	if err == nil {
		contextResult.Diagnosis = &diagnosis
	} else if err != sql.ErrNoRows {
		return contextResult, err
	}
	return contextResult, nil
}

func (s *Store) AddFailureDiagnosis(ctx context.Context, diagnosis model.FailureDiagnosis) error {
	result, err := s.FailureContext(ctx, diagnosis.RunID, diagnosis.TestKey)
	if err != nil {
		return err
	}
	if result.Result.Outcome == "passed" || result.Result.Outcome == "skipped" {
		return fmt.Errorf("test %s is not a failure", diagnosis.TestKey)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO failure_diagnoses(run_id,test_id,category,explanation,created_by,created_at)
SELECT ?,id,?,?,?,? FROM tests WHERE test_key=?`, diagnosis.RunID, diagnosis.Category, diagnosis.Explanation, diagnosis.CreatedBy, diagnosis.CreatedAt, diagnosis.TestKey)
	return err
}
