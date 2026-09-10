package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/model"
)

func (s *Store) CurrentSymbols(ctx context.Context) ([]model.Symbol, error) {
	runID, _, err := s.LatestInventory(ctx)
	if err != nil || runID == "" {
		return []model.Symbol{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT s.language, sv.path, s.qualified_name, sv.kind,
sv.start_line, sv.end_line, sv.executable_lines_json, sv.semantic_hash, sv.signature_hash, sv.body_hash
FROM symbol_versions sv JOIN symbols s ON s.id=sv.symbol_id
WHERE sv.inventory_run_id=? ORDER BY s.symbol_key`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []model.Symbol{}
	for rows.Next() {
		var symbol model.Symbol
		var lines string
		if err := rows.Scan(&symbol.Language, &symbol.Path, &symbol.QualifiedName, &symbol.Kind,
			&symbol.StartLine, &symbol.EndLine, &lines, &symbol.SemanticHash, &symbol.SignatureHash, &symbol.BodyHash); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(lines), &symbol.ExecutableLines)
		result = append(result, symbol)
	}
	return result, rows.Err()
}

func (s *Store) RecentInventoryIDs(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM inventory_runs WHERE status='completed' ORDER BY completed_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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

func (s *Store) CreateProposal(ctx context.Context, proposal model.TestProposal) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	cases, _ := json.Marshal(proposal.Cases)
	_, err = tx.ExecContext(ctx, `INSERT INTO test_proposals(id,status,rationale,cases_json,created_by,created_at,updated_at)
VALUES(?,?,?,?,?,?,?)`, proposal.ID, proposal.Status, proposal.Rationale, string(cases), proposal.CreatedBy, proposal.CreatedAt, proposal.UpdatedAt)
	if err != nil {
		return err
	}
	for _, target := range proposal.Targets {
		if _, err := tx.ExecContext(ctx, `INSERT INTO proposal_targets(proposal_id,symbol_key,semantic_hash) VALUES(?,?,?)`,
			proposal.ID, target.SymbolKey, target.SemanticHash); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetProposal(ctx context.Context, id string) (model.TestProposal, error) {
	var proposal model.TestProposal
	var cases string
	err := s.db.QueryRowContext(ctx, `SELECT id,status,rationale,cases_json,created_by,created_at,updated_at
FROM test_proposals WHERE id=?`, id).Scan(&proposal.ID, &proposal.Status, &proposal.Rationale, &cases,
		&proposal.CreatedBy, &proposal.CreatedAt, &proposal.UpdatedAt)
	if err != nil {
		return proposal, err
	}
	if err := json.Unmarshal([]byte(cases), &proposal.Cases); err != nil {
		return proposal, err
	}
	proposal.Targets = []model.ProposalTarget{}
	rows, err := s.db.QueryContext(ctx, `SELECT symbol_key,semantic_hash FROM proposal_targets WHERE proposal_id=? ORDER BY symbol_key`, id)
	if err != nil {
		return proposal, err
	}
	for rows.Next() {
		var target model.ProposalTarget
		if err := rows.Scan(&target.SymbolKey, &target.SemanticHash); err != nil {
			rows.Close()
			return proposal, err
		}
		proposal.Targets = append(proposal.Targets, target)
	}
	if err := rows.Close(); err != nil {
		return proposal, err
	}
	var decision model.ProposalDecision
	err = s.db.QueryRowContext(ctx, `SELECT decision,decided_by,reason,created_at FROM proposal_decisions
WHERE proposal_id=? ORDER BY id DESC LIMIT 1`, id).Scan(&decision.Decision, &decision.DecidedBy, &decision.Reason, &decision.CreatedAt)
	if err == nil {
		proposal.Decision = &decision
	} else if err != sql.ErrNoRows {
		return proposal, err
	}
	return proposal, nil
}

func (s *Store) ListProposals(ctx context.Context, status string, limit, offset int) (model.Page[model.TestProposal], error) {
	where, args := "", []any{}
	if status != "" {
		where, args = " WHERE status=?", append(args, status)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM test_proposals`+where, args...).Scan(&total); err != nil {
		return model.Page[model.TestProposal]{}, err
	}
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM test_proposals`+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return model.Page[model.TestProposal]{}, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return model.Page[model.TestProposal]{}, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return model.Page[model.TestProposal]{}, err
	}
	page := model.Page[model.TestProposal]{Items: []model.TestProposal{}, Total: total}
	for _, id := range ids {
		proposal, err := s.GetProposal(ctx, id)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, proposal)
	}
	if offset+len(page.Items) < total {
		page.NextCursor = offset + len(page.Items)
	}
	return page, nil
}

func (s *Store) DecideProposal(ctx context.Context, id, decision, decidedBy, reason string) error {
	if decision != "approved" && decision != "rejected" {
		return fmt.Errorf("invalid decision %q", decision)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM test_proposals WHERE id=?`, id).Scan(&status); err != nil {
		return err
	}
	if status != "proposed" {
		return fmt.Errorf("proposal %s is %s, expected proposed", id, status)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT INTO proposal_decisions(proposal_id,decision,decided_by,reason,created_at) VALUES(?,?,?,?,?)`,
		id, decision, decidedBy, reason, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE test_proposals SET status=?,updated_at=? WHERE id=?`, decision, now, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MarkProposalObsolete(ctx context.Context, id, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE test_proposals SET status='obsolete',updated_at=? WHERE id=? AND status NOT IN ('verified','rejected')`, now, id)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO proposal_decisions(proposal_id,decision,decided_by,reason,created_at)
VALUES(?, 'rejected', 'testledger', ?, ?)`, id, reason, now)
	return err
}

func (s *Store) MarkProposalImplemented(ctx context.Context, id string, links []model.IntendedTestLink) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM test_proposals WHERE id=?`, id).Scan(&status); err != nil {
		return err
	}
	if status != "approved" {
		return fmt.Errorf("proposal %s is %s, expected approved", id, status)
	}
	for _, link := range links {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM proposal_targets WHERE proposal_id=? AND symbol_key=?`, id, link.SymbolKey).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("symbol %s is not targeted by proposal", link.SymbolKey)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO intended_test_links(proposal_id,symbol_key,test_key) VALUES(?,?,?)`, id, link.SymbolKey, link.TestKey); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE test_proposals SET status='implemented',updated_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ProposalLinks(ctx context.Context, id string) ([]model.IntendedTestLink, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT symbol_key,test_key,verified_run_id IS NOT NULL,COALESCE(verified_run_id,'')
FROM intended_test_links WHERE proposal_id=? ORDER BY symbol_key,test_key`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	links := []model.IntendedTestLink{}
	for rows.Next() {
		var link model.IntendedTestLink
		if err := rows.Scan(&link.SymbolKey, &link.TestKey, &link.Verified, &link.RunID); err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, rows.Err()
}

func (s *Store) VerifyIntentions(ctx context.Context, runID string, minimum float64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `UPDATE intended_test_links SET verified_run_id=?,verified_at=?
WHERE verified_run_id IS NULL AND EXISTS (
 SELECT 1 FROM test_proposals p
 JOIN proposal_targets pt ON pt.proposal_id=p.id AND pt.symbol_key=intended_test_links.symbol_key
 JOIN symbols s ON s.symbol_key=intended_test_links.symbol_key
 JOIN tests t ON t.test_key=intended_test_links.test_key
 JOIN coverage_observations co ON co.run_id=? AND co.symbol_id=s.id AND co.test_id=t.id AND co.semantic_hash=pt.semantic_hash AND co.line_percent>=?
 JOIN test_case_results tr ON tr.run_id=co.run_id AND tr.test_id=t.id AND tr.outcome='passed'
 WHERE p.id=intended_test_links.proposal_id
)`, runID, now, runID, minimum)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE test_proposals SET status='verified',updated_at=?
WHERE status='implemented' AND EXISTS (SELECT 1 FROM intended_test_links l WHERE l.proposal_id=test_proposals.id)
AND NOT EXISTS (SELECT 1 FROM intended_test_links l WHERE l.proposal_id=test_proposals.id AND l.verified_run_id IS NULL)`, now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

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

func (s *Store) CreateJob(ctx context.Context, job model.AsyncJob) error {
	arguments, _ := json.Marshal(job.Arguments)
	_, err := s.db.ExecContext(ctx, `INSERT INTO async_jobs(id,kind,status,arguments_json,created_at) VALUES(?,?,?,?,?)`,
		job.ID, job.Kind, job.Status, string(arguments), job.CreatedAt)
	return err
}

func (s *Store) StartJob(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE async_jobs SET status='running',started_at=? WHERE id=? AND status='queued'`, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (s *Store) FinishJob(ctx context.Context, id, status, runID, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE async_jobs SET status=?,finished_at=?,result_run_id=?,error=? WHERE id=?`,
		status, time.Now().UTC().Format(time.RFC3339Nano), nullString(runID), nullString(message), id)
	return err
}

func (s *Store) GetJob(ctx context.Context, id string) (model.AsyncJob, error) {
	var job model.AsyncJob
	var arguments, started, finished, runID, message string
	err := s.db.QueryRowContext(ctx, `SELECT id,kind,status,arguments_json,created_at,COALESCE(started_at,''),COALESCE(finished_at,''),COALESCE(result_run_id,''),COALESCE(error,'')
FROM async_jobs WHERE id=?`, id).Scan(&job.ID, &job.Kind, &job.Status, &arguments, &job.CreatedAt, &started, &finished, &runID, &message)
	if err != nil {
		return job, err
	}
	_ = json.Unmarshal([]byte(arguments), &job.Arguments)
	job.StartedAt, job.FinishedAt, job.ResultRunID, job.Error = started, finished, runID, message
	return job, nil
}

func (s *Store) ActiveJobs(ctx context.Context) ([]model.AsyncJob, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM async_jobs WHERE status IN ('queued','running') ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	jobs := []model.AsyncJob{}
	for _, id := range ids {
		job, err := s.GetJob(ctx, id)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (s *Store) InterruptActiveJobs(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE async_jobs SET status='interrupted',finished_at=?,error='MCP server restarted before completion'
WHERE status IN ('queued','running')`, time.Now().UTC().Format(time.RFC3339Nano))
	return err
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
