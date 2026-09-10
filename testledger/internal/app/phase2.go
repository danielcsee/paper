package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/model"
	"github.com/danielcsee/sciterm/testledger/internal/store"
)

func normalizePage(limit, cursor int) (int, int) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if cursor < 0 {
		cursor = 0
	}
	return limit, cursor
}

func (a *App) ListCoverageGaps(ctx context.Context, changedOnly bool, limit, cursor int) (model.Page[model.Gap], error) {
	limit, cursor = normalizePage(limit, cursor)
	symbols, err := a.Store.CurrentSymbols(ctx)
	if err != nil {
		return model.Page[model.Gap]{}, err
	}
	ids, err := a.Store.RecentInventoryIDs(ctx, 2)
	if err != nil {
		return model.Page[model.Gap]{}, err
	}
	previous := map[string]store.PreviousSymbol{}
	if len(ids) > 1 {
		previous, err = a.Store.SymbolsForInventory(ctx, ids[1])
		if err != nil {
			return model.Page[model.Gap]{}, err
		}
	}
	gaps := []model.Gap{}
	for _, symbol := range symbols {
		old, existed := previous[symbol.Key()]
		changed := !existed || old.SemanticHash != symbol.SemanticHash
		if changedOnly && !changed {
			continue
		}
		skipped, err := a.Store.ActiveDisposition(ctx, symbol.Key(), symbol.SemanticHash, time.Now())
		if err != nil {
			return model.Page[model.Gap]{}, err
		}
		if skipped {
			continue
		}
		if coverable(symbol) {
			continue
		}
		minimum := a.minimumCoverage(symbol.Language)
		coverage, err := a.Store.CoverageForSymbol(ctx, symbol.Key(), symbol.SemanticHash, minimum)
		if err != nil {
			return model.Page[model.Gap]{}, err
		}
		if coverage.Count > 0 {
			continue
		}
		reason := "no passing test meets the function coverage policy"
		if coverage.Best > 0 {
			reason = fmt.Sprintf("best observed coverage %.1f%% is below required %.1f%%", coverage.Best, minimum)
		}
		gap := model.Gap{SymbolKey: symbol.Key(), Path: symbol.Path, QualifiedName: symbol.QualifiedName, Kind: symbol.Kind,
			StartLine: symbol.StartLine, EndLine: symbol.EndLine, SemanticHash: symbol.SemanticHash, Changed: changed, Reason: reason,
			BestCoverage: coverage.Best, CoveringTestCount: coverage.Count}
		if detail, detailErr := a.Store.CoverageDetailForSymbol(ctx, symbol.Key(), symbol.SemanticHash); detailErr == nil {
			gap.MissingLines, gap.MissingBranches = detail.MissingLines, detail.MissingBranches
		}
		gaps = append(gaps, gap)
	}
	page := model.Page[model.Gap]{Items: []model.Gap{}, Total: len(gaps)}
	if cursor >= len(gaps) {
		return page, nil
	}
	end := cursor + limit
	if end > len(gaps) {
		end = len(gaps)
	}
	page.Items = append(page.Items, gaps[cursor:end]...)
	if end < len(gaps) {
		page.NextCursor = end
	}
	return page, nil
}

func (a *App) GetSymbolContext(ctx context.Context, symbolKey string, includeSource bool) (model.SymbolContext, error) {
	symbols, err := a.Store.CurrentSymbols(ctx)
	if err != nil {
		return model.SymbolContext{}, err
	}
	var found *model.Symbol
	for i := range symbols {
		if symbols[i].Key() == symbolKey {
			found = &symbols[i]
			break
		}
	}
	if found == nil {
		return model.SymbolContext{}, fmt.Errorf("unknown current symbol %s", symbolKey)
	}
	result := model.SymbolContext{Symbol: *found, CoveringTests: []string{}}
	result.CoveringTests, err = a.Store.CoveringTests(ctx, found.Key(), found.SemanticHash)
	if err != nil {
		return result, err
	}
	if includeSource {
		path := filepath.Join(a.Root, filepath.FromSlash(found.Path))
		rel, err := filepath.Rel(a.Root, path)
		if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			return result, errors.New("symbol path escapes project root")
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return result, err
		}
		lines := strings.Split(string(contents), "\n")
		start, end := found.StartLine-1, found.EndLine
		if start < 0 {
			start = 0
		}
		if end > len(lines) {
			end = len(lines)
		}
		if start < end {
			result.Source = strings.Join(lines[start:end], "\n")
		}
	}
	return result, nil
}

func (a *App) CreateProposal(ctx context.Context, targets []string, cases []model.ProposalCase, rationale, createdBy string) (model.TestProposal, error) {
	if strings.TrimSpace(rationale) == "" {
		return model.TestProposal{}, errors.New("proposal rationale is required")
	}
	if strings.TrimSpace(createdBy) == "" {
		return model.TestProposal{}, errors.New("created_by is required")
	}
	if len(targets) == 0 || len(cases) == 0 {
		return model.TestProposal{}, errors.New("proposal requires at least one target and one test case")
	}
	for _, testCase := range cases {
		if strings.TrimSpace(testCase.Name) == "" || strings.TrimSpace(testCase.Description) == "" || len(testCase.Assertions) == 0 {
			return model.TestProposal{}, errors.New("each proposed case requires name, description, and assertions")
		}
		if !a.validTestSelector(testCase.TestFile) {
			return model.TestProposal{}, fmt.Errorf("proposed test file is outside configured test roots: %s", testCase.TestFile)
		}
	}
	_, current, err := a.Scan(ctx)
	if err != nil {
		return model.TestProposal{}, err
	}
	byKey := map[string]model.Symbol{}
	for _, symbol := range current {
		byKey[symbol.Key()] = symbol
	}
	proposal := model.TestProposal{ID: newID("proposal"), Status: "proposed", Rationale: rationale, Cases: cases, Targets: []model.ProposalTarget{}, CreatedBy: createdBy}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	proposal.CreatedAt, proposal.UpdatedAt = now, now
	seen := map[string]bool{}
	for _, key := range targets {
		if seen[key] {
			continue
		}
		symbol, ok := byKey[key]
		if !ok {
			return model.TestProposal{}, fmt.Errorf("unknown current target %s", key)
		}
		seen[key] = true
		skipped, err := a.Store.ActiveDisposition(ctx, key, symbol.SemanticHash, time.Now())
		if err != nil {
			return model.TestProposal{}, err
		}
		if skipped {
			return model.TestProposal{}, fmt.Errorf("target %s has an active disposition", key)
		}
		coverage, err := a.Store.CoverageForSymbol(ctx, key, symbol.SemanticHash, a.minimumCoverage(symbol.Language))
		if err != nil {
			return model.TestProposal{}, err
		}
		if coverage.Count > 0 {
			return model.TestProposal{}, fmt.Errorf("target %s already meets coverage policy", key)
		}
		proposal.Targets = append(proposal.Targets, model.ProposalTarget{SymbolKey: key, SemanticHash: symbol.SemanticHash})
	}
	sort.Slice(proposal.Targets, func(i, j int) bool { return proposal.Targets[i].SymbolKey < proposal.Targets[j].SymbolKey })
	if err := a.Store.CreateProposal(ctx, proposal); err != nil {
		return proposal, err
	}
	return proposal, nil
}

func (a *App) DecideProposal(ctx context.Context, id, decision, decidedBy, reason string) (model.TestProposal, error) {
	if strings.TrimSpace(decidedBy) == "" || strings.TrimSpace(reason) == "" {
		return model.TestProposal{}, errors.New("decided_by and reason are required")
	}
	if decision == "approved" {
		if err := a.ensureProposalCurrent(ctx, id); err != nil {
			return model.TestProposal{}, err
		}
	}
	if err := a.Store.DecideProposal(ctx, id, decision, decidedBy, reason); err != nil {
		return model.TestProposal{}, err
	}
	return a.Store.GetProposal(ctx, id)
}

func (a *App) MarkProposalImplemented(ctx context.Context, id string, links []model.IntendedTestLink) (model.TestProposal, error) {
	if len(links) == 0 {
		return model.TestProposal{}, errors.New("at least one intended test link is required")
	}
	for _, link := range links {
		if strings.TrimSpace(link.SymbolKey) == "" || strings.TrimSpace(link.TestKey) == "" {
			return model.TestProposal{}, errors.New("each link requires symbol_key and test_key")
		}
	}
	if err := a.ensureProposalCurrent(ctx, id); err != nil {
		return model.TestProposal{}, err
	}
	proposal, err := a.Store.GetProposal(ctx, id)
	if err != nil {
		return model.TestProposal{}, err
	}
	targeted := map[string]bool{}
	for _, target := range proposal.Targets {
		targeted[target.SymbolKey] = true
	}
	linked := map[string]bool{}
	for _, link := range links {
		if !targeted[link.SymbolKey] {
			return model.TestProposal{}, fmt.Errorf("symbol %s is not targeted by proposal", link.SymbolKey)
		}
		linked[link.SymbolKey] = true
	}
	for key := range targeted {
		if !linked[key] {
			return model.TestProposal{}, fmt.Errorf("proposal target %s has no intended test link", key)
		}
	}
	if err := a.Store.MarkProposalImplemented(ctx, id, links); err != nil {
		return model.TestProposal{}, err
	}
	return a.Store.GetProposal(ctx, id)
}

func (a *App) ensureProposalCurrent(ctx context.Context, id string) error {
	proposal, err := a.Store.GetProposal(ctx, id)
	if err != nil {
		return err
	}
	_, symbols, err := a.Scan(ctx)
	if err != nil {
		return err
	}
	current := map[string]string{}
	for _, symbol := range symbols {
		current[symbol.Key()] = symbol.SemanticHash
	}
	for _, target := range proposal.Targets {
		if current[target.SymbolKey] != target.SemanticHash {
			reason := "target function version changed after proposal creation"
			_ = a.Store.MarkProposalObsolete(ctx, id, reason)
			return fmt.Errorf("proposal %s is obsolete: %s", id, reason)
		}
	}
	return nil
}

func (a *App) StartAsyncTest(ctx context.Context, extraArgs []string, proposalID string) (model.AsyncJob, error) {
	return a.StartAsyncTestLanguage(ctx, "python", extraArgs, proposalID, false)
}

func (a *App) StartAsyncTestLanguage(ctx context.Context, language string, extraArgs []string, proposalID string, affected bool) (model.AsyncJob, error) {
	configured, err := a.language(language)
	if err != nil {
		return model.AsyncJob{}, err
	}
	language = configured.Name
	if affected {
		if proposalID != "" || len(extraArgs) > 0 {
			return model.AsyncJob{}, errors.New("affected selection cannot be combined with proposal_id or test_keys")
		}
		selection, err := a.SelectAffectedTestsForLanguage(ctx, language)
		if err != nil {
			return model.AsyncJob{}, err
		}
		if !selection.Fallback && len(selection.TestKeys) == 0 {
			return model.AsyncJob{}, errors.New("no changed functions have affected tests")
		}
		if !selection.Fallback {
			extraArgs = selection.TestKeys
		}
	}
	if proposalID != "" {
		proposal, err := a.Store.GetProposal(ctx, proposalID)
		if err != nil {
			return model.AsyncJob{}, err
		}
		if proposal.Status != "implemented" {
			return model.AsyncJob{}, fmt.Errorf("proposal %s is %s, expected implemented", proposalID, proposal.Status)
		}
		if len(extraArgs) == 0 {
			links, err := a.Store.ProposalLinks(ctx, proposalID)
			if err != nil {
				return model.AsyncJob{}, err
			}
			for _, link := range links {
				extraArgs = append(extraArgs, link.TestKey)
			}
		}
	}
	for _, selector := range extraArgs {
		if !a.validTestSelector(selector) {
			return model.AsyncJob{}, fmt.Errorf("invalid test selector %q", selector)
		}
	}
	job := model.AsyncJob{ID: newID("job"), Kind: "test", Status: "queued", Arguments: map[string]any{"language": language, "test_keys": extraArgs, "proposal_id": proposalID, "affected": affected}, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := a.Store.CreateJob(ctx, job); err != nil {
		return job, err
	}
	a.jobs.Add(1)
	go func() {
		defer a.jobs.Done()
		a.jobSlots <- struct{}{}
		defer func() { <-a.jobSlots }()
		background := context.Background()
		if err := a.Store.StartJob(background, job.ID); err != nil {
			_ = a.Store.FinishJob(background, job.ID, "failed", "", err.Error())
			return
		}
		result, err := a.TestLanguage(background, language, extraArgs)
		if err != nil {
			_ = a.Store.FinishJob(background, job.ID, "failed", "", err.Error())
			return
		}
		status := "completed"
		message := ""
		if result.Status != "passed" {
			status = "failed"
			message = result.InfrastructureErr
		}
		_ = a.Store.FinishJob(background, job.ID, status, result.RunID, message)
	}()
	return job, nil
}

func (a *App) validTestSelector(selector string) bool {
	if strings.TrimSpace(selector) == "" || strings.HasPrefix(selector, "-") || strings.ContainsAny(selector, "\x00\r\n") {
		return false
	}
	path := strings.SplitN(selector, "::", 2)[0]
	if filepath.IsAbs(path) {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return false
	}
	for _, language := range a.Config.Languages {
		for _, root := range language.Test.TestRoots {
			root = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(filepath.FromSlash(root))), "/")
			if clean == root || strings.HasPrefix(clean, root+"/") {
				return true
			}
		}
	}
	return false
}

func (a *App) GetAsyncTest(ctx context.Context, jobID string, limit, cursor int, failuresOnly bool) (model.AsyncJob, *model.TestRunResult, model.Page[model.TestCaseResult], error) {
	limit, cursor = normalizePage(limit, cursor)
	job, err := a.Store.GetJob(ctx, jobID)
	if err != nil {
		return job, nil, model.Page[model.TestCaseResult]{}, err
	}
	if job.ResultRunID == "" {
		return job, nil, model.Page[model.TestCaseResult]{Items: []model.TestCaseResult{}}, nil
	}
	run, page, err := a.Store.TestRun(ctx, job.ResultRunID, limit, cursor, failuresOnly)
	if err != nil {
		return job, nil, page, err
	}
	return job, &run, page, nil
}

func (a *App) RecordFailureDiagnosis(ctx context.Context, runID, testKey, category, explanation, createdBy string) (model.FailureDiagnosis, error) {
	allowed := map[string]bool{"product_bug": true, "incorrect_test_expectation": true, "test_setup_defect": true, "environment_problem": true, "flaky": true, "unknown": true}
	if !allowed[category] {
		return model.FailureDiagnosis{}, fmt.Errorf("invalid diagnosis category %q", category)
	}
	if strings.TrimSpace(explanation) == "" || strings.TrimSpace(createdBy) == "" {
		return model.FailureDiagnosis{}, errors.New("explanation and created_by are required")
	}
	diagnosis := model.FailureDiagnosis{RunID: runID, TestKey: testKey, Category: category, Explanation: explanation, CreatedBy: createdBy, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := a.Store.AddFailureDiagnosis(ctx, diagnosis); err != nil {
		return diagnosis, err
	}
	return diagnosis, nil
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
