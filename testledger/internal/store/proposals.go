// Proposals, the human decisions recorded against them, and the intended
// symbol-to-test links a later run either verifies or leaves unresolved.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/model"
	_ "modernc.org/sqlite"
)

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
	if proposal.Links, err = s.ProposalLinks(ctx, id); err != nil {
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
	// `implemented` is accepted as well as `approved`: a link can name a test
	// that turns out not to reach its symbol, and correcting it is the action
	// NextActions recommends for an unresolved link. Rejecting the correction
	// would leave the proposal permanently stuck on a link nobody can fix.
	if status != "approved" && status != "implemented" {
		return fmt.Errorf("proposal %s is %s, expected approved or implemented", id, status)
	}
	// Re-recording replaces the previous set rather than accumulating, so a
	// corrected link supersedes the one it replaces instead of sitting beside
	// it. Verification timestamps are re-earned by the next run.
	if _, err := tx.ExecContext(ctx, `DELETE FROM intended_test_links WHERE proposal_id=?`, id); err != nil {
		return err
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

// ProposalLinks returns a proposal's intended links, each resolved against the
// dispositions in force now. Resolution is what a caller should read: a link
// the human has dispositioned is settled even though no coverage will ever
// verify it, and only LinkUnresolved still blocks the proposal.

// ProposalLinks returns a proposal's intended links, each resolved against the
// dispositions in force now. Resolution is what a caller should read: a link
// the human has dispositioned is settled even though no coverage will ever
// verify it, and only LinkUnresolved still blocks the proposal.
func (s *Store) ProposalLinks(ctx context.Context, id string) ([]model.IntendedTestLink, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `SELECT l.symbol_key,l.test_key,l.verified_run_id IS NOT NULL,COALESCE(l.verified_run_id,''),
COALESCE((SELECT d.reason FROM dispositions d WHERE d.symbol_key=l.symbol_key
 AND (d.expires_at IS NULL OR d.expires_at > ?) ORDER BY d.id DESC LIMIT 1),'')
FROM intended_test_links l WHERE l.proposal_id=? ORDER BY l.symbol_key,l.test_key`, now, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	links := []model.IntendedTestLink{}
	for rows.Next() {
		var link model.IntendedTestLink
		var dispositionReason string
		if err := rows.Scan(&link.SymbolKey, &link.TestKey, &link.Verified, &link.RunID, &dispositionReason); err != nil {
			return nil, err
		}
		switch {
		case link.Verified:
			link.Resolution = model.LinkVerified
		case dispositionReason != "":
			link.Resolution, link.Reason = model.LinkDispositioned, dispositionReason
		default:
			link.Resolution = model.LinkUnresolved
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
	// A link is resolved when coverage verified it, or when a human recorded an
	// active disposition for its symbol. Without the second clause a proposal
	// containing even one deliberately skipped target could never close, and
	// `next` would return run_implemented_tests forever.
	_, err = tx.ExecContext(ctx, `UPDATE test_proposals SET status='verified',updated_at=?
WHERE status='implemented' AND EXISTS (SELECT 1 FROM intended_test_links l WHERE l.proposal_id=test_proposals.id)
AND NOT EXISTS (
 SELECT 1 FROM intended_test_links l
 WHERE l.proposal_id=test_proposals.id AND l.verified_run_id IS NULL
 AND NOT EXISTS (
  SELECT 1 FROM dispositions d
  WHERE d.symbol_key=l.symbol_key AND (d.expires_at IS NULL OR d.expires_at > ?)
 )
)`, now, now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// UnresolvedLinkCount is the number of links still blocking a proposal.

// UnresolvedLinkCount is the number of links still blocking a proposal.
func (s *Store) UnresolvedLinkCount(ctx context.Context, proposalID string) (int, error) {
	links, err := s.ProposalLinks(ctx, proposalID)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, link := range links {
		if link.Resolution == model.LinkUnresolved {
			count++
		}
	}
	return count, nil
}
