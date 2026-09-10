// Asynchronous test runs, and reading their results. MCP hosts impose tool
// timeouts, so a run returns a job id rather than blocking.
package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/model"
)

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
