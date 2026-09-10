// What an agent or operator should look at next. Status answers "where do
// things stand"; NextActions answers "what should I do".
package app

import (
	"context"
	"strings"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/model"
)

func (a *App) Status(ctx context.Context) (model.Status, error) {
	latestID, completedAt, err := a.Store.LatestInventory(ctx)
	if err != nil {
		return model.Status{}, err
	}
	status := model.Status{LatestInventoryID: latestID, LatestInventoryAt: completedAt}
	if latestID != "" {
		symbols, err := a.Store.SymbolsForInventory(ctx, latestID)
		if err != nil {
			return status, err
		}
		status.SymbolCount = len(symbols)
		// A status scan is intentionally read-only. Count unresolved current versions.
		for key, symbol := range symbols {
			skipped, err := a.Store.ActiveDisposition(ctx, key, symbol.SemanticHash, time.Now())
			if err != nil {
				return status, err
			}
			if skipped {
				continue
			}
			coverage, err := a.Store.CoverageForSymbol(ctx, key, symbol.SemanticHash, a.minimumCoverage(strings.SplitN(key, ":", 2)[0]))
			if err != nil {
				return status, err
			}
			if coverage.Count == 0 {
				status.OutstandingGaps++
			}
		}
	}
	status.ActiveSkips, err = a.Store.CountActiveSkips(ctx)
	if err != nil {
		return status, err
	}
	status.LatestTestRun, err = a.Store.LatestTestRun(ctx)
	return status, err
}

func (a *App) NextActions(ctx context.Context, limit int) (model.NextActions, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	result := model.NextActions{Summary: map[string]int{}, Actions: []model.NextAction{}}
	latestInventory, _, err := a.Store.LatestInventory(ctx)
	if err != nil {
		return result, err
	}
	if latestInventory == "" {
		result.State = "scan_required"
		result.Actions = append(result.Actions, model.NextAction{Type: "scan_changes", Description: "Create the first deterministic function inventory."})
		return result, nil
	}
	jobs, err := a.Store.ActiveJobs(ctx)
	if err != nil {
		return result, err
	}
	if len(jobs) > 0 {
		result.State = "tests_running"
		result.Summary["active_jobs"] = len(jobs)
		for _, job := range jobs {
			if len(result.Actions) >= limit {
				break
			}
			result.Actions = append(result.Actions, model.NextAction{Type: "wait_test_run", ID: job.ID, Description: "Wait for the asynchronous test run to finish."})
		}
		return result, nil
	}
	for _, status := range []string{"proposed", "approved"} {
		page, err := a.Store.ListProposals(ctx, status, limit, 0)
		if err != nil {
			return result, err
		}
		if page.Total == 0 {
			continue
		}
		result.Summary[status] = page.Total
		if status == "proposed" {
			result.State = "awaiting_human_decision"
			for _, proposal := range page.Items {
				result.Actions = append(result.Actions, model.NextAction{Type: "request_human_decision", ID: proposal.ID, Description: "Present the proposal to the human and record only their explicit decision."})
			}
		}
		if status == "approved" {
			result.State = "tests_to_implement"
			for _, proposal := range page.Items {
				result.Actions = append(result.Actions, model.NextAction{Type: "implement_approved_tests", ID: proposal.ID, Description: "Write the approved tests, then record their exact pytest test keys."})
			}
		}
		return result, nil
	}
	latest, err := a.Store.LatestTestRun(ctx)
	if err != nil {
		return result, err
	}
	if latest != nil && (latest.Status == "failed" || latest.Status == "infrastructure_error" || latest.Status == "timeout") {
		result.State = "tests_failing"
		result.Summary["failed"] = latest.Failed
		result.Summary["errors"] = latest.Errors
		result.Actions = append(result.Actions, model.NextAction{Type: "diagnose_test_run", ID: latest.RunID, Description: "Read the paginated failures, categorize root causes, fix them, and rerun."})
		return result, nil
	}
	implemented, err := a.Store.ListProposals(ctx, "implemented", limit, 0)
	if err != nil {
		return result, err
	}
	if implemented.Total > 0 {
		// An implemented proposal needs a run to verify its links. But a link
		// can also be permanently unverifiable -- the named test never reaches
		// the symbol, or the symbol is one a run will never exercise. Asking
		// for another run in that case yields the identical answer forever, so
		// separate "no run has judged this yet" from "a run judged it and these
		// links did not resolve".
		toRun, blocked := []model.TestProposal{}, []model.NextAction{}
		for _, proposal := range implemented.Items {
			links, err := a.Store.ProposalLinks(ctx, proposal.ID)
			if err != nil {
				return result, err
			}
			judged := latest != nil && latest.FinishedAt.Format(time.RFC3339Nano) >= proposal.UpdatedAt
			for _, link := range links {
				if link.Resolution != model.LinkUnresolved {
					continue
				}
				if !judged {
					continue
				}
				blocked = append(blocked, model.NextAction{
					Type: "review_unverified_link", ID: proposal.ID, SymbolKey: link.SymbolKey,
					Description: "The last run did not show " + link.TestKey + " covering this symbol to the required percentage. Fix the test, relink it, or record a disposition -- rerunning alone will not resolve it.",
				})
			}
			if !judged {
				toRun = append(toRun, proposal)
			}
		}
		if len(toRun) > 0 {
			result.State = "tests_to_verify"
			result.Summary["implemented"] = len(toRun)
			for _, proposal := range toRun {
				if len(result.Actions) >= limit {
					break
				}
				result.Actions = append(result.Actions, model.NextAction{Type: "run_implemented_tests", ID: proposal.ID, Description: "Run tests to verify the intended symbol-to-test coverage links."})
			}
			return result, nil
		}
		if len(blocked) > 0 {
			result.State = "links_unverified"
			result.Summary["unresolved_links"] = len(blocked)
			if len(blocked) > limit {
				blocked = blocked[:limit]
			}
			result.Actions = append(result.Actions, blocked...)
			return result, nil
		}
	}
	gaps, err := a.ListCoverageGaps(ctx, false, limit, 0)
	if err != nil {
		return result, err
	}
	result.Summary["coverage_gaps"] = gaps.Total
	if gaps.Total > 0 {
		result.State = "proposal_required"
		for _, gap := range gaps.Items {
			result.Actions = append(result.Actions, model.NextAction{Type: "propose_test", SymbolKey: gap.SymbolKey, Description: "Propose a focused test for this uncovered function version."})
		}
		return result, nil
	}
	result.State = "complete"
	return result, nil
}
