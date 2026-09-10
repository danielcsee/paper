// Coverage evidence: per-test observations, aggregate line and branch
// detail, and the queries that ask whether a symbol version is covered.
package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/danielcsee/sciterm/testledger/internal/model"
	_ "modernc.org/sqlite"
)

type CoverageSummary struct {
	Count int
	Best  float64
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

func (s *Store) CoveringTests(ctx context.Context, symbolKey, semanticHash string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT t.test_key
FROM coverage_observations co
JOIN symbols s ON s.id=co.symbol_id JOIN tests t ON t.id=co.test_id
JOIN test_case_results tr ON tr.run_id=co.run_id AND tr.test_id=co.test_id
WHERE s.symbol_key=? AND co.semantic_hash=? AND tr.outcome='passed'
ORDER BY t.test_key`, symbolKey, semanticHash)
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
