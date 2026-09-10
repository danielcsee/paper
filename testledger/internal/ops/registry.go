// Every operation Testledger exposes, declared once for both transports.
//
// Each entry owns its input struct, its schema-bearing tags, and its handler.
// Adding a capability here adds it to the CLI and the MCP server together.
package ops

import (
	"context"
	"fmt"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/app"
	"github.com/danielcsee/sciterm/testledger/internal/model"
)

// Page fields are shared by every list-shaped operation so both transports
// page the same way. Embedding them keeps the wire names identical.
type Page struct {
	Limit  int `json:"limit" desc:"Page size; default 20, maximum 100."`
	Cursor int `json:"cursor" desc:"Offset cursor from the previous page."`
}

// Confirm is embedded by operations that record a human decision.
type Confirm struct {
	HumanConfirmed bool `json:"human_confirmed" desc:"Set only after the human stated this exact decision. On the CLI the human typing the command is the confirmation."`
}

// Confirmed reports whether a decision input carries explicit confirmation.
func Confirmed(input any) bool {
	for _, field := range Fields(input) {
		if field.Name == "human_confirmed" {
			return field.get(input).Bool()
		}
	}
	return false
}

func init() {
	register(&Op{
		Name: "get_next_actions", Verb: "next", Order: 1, Title: "Get next testing actions", ReadOnly: true,
		Summary: "Return the smallest actionable workflow state. Call this first, and again after every decision or run.",
		New:     func() any { return &nextInput{} },
		Run: func(ctx context.Context, a *app.App, in any) (Result, error) {
			input := in.(*nextInput)
			result, err := a.NextActions(ctx, input.Limit)
			if err != nil {
				return Result{}, err
			}
			detail := []string{}
			for _, action := range result.Actions {
				target := action.ID
				if action.SymbolKey != "" {
					target = action.SymbolKey
				}
				detail = append(detail, fmt.Sprintf("  %s %s", action.Type, target))
			}
			return Result{Value: result, Summary: "Workflow state: " + result.State, Detail: detail}, nil
		},
	})

	register(&Op{
		Name: "scan_changes", Verb: "check", Order: 3, Title: "Scan changed functions",
		Summary: "Take a deterministic inventory snapshot and report which function versions still need coverage.",
		New:     func() any { return &checkInput{} },
		Run: func(ctx context.Context, a *app.App, in any) (Result, error) {
			input := in.(*checkInput)
			check, err := a.Check(ctx)
			if err != nil {
				return Result{}, err
			}
			limit := clamp(input.Limit, 20, 100)
			gaps := check.ChangedGaps
			if input.All {
				gaps = check.AllGaps
			}
			total := len(gaps)
			if len(gaps) > limit {
				gaps = gaps[:limit]
			}
			detail := []string{}
			for _, gap := range gaps {
				detail = append(detail, fmt.Sprintf("  %s (%s)", gap.SymbolKey, gap.Reason))
			}
			value := map[string]any{
				"inventory_run_id": check.InventoryRunID, "scanned": check.Scanned,
				"changed": check.Changed, "deleted": check.Deleted, "skipped": check.Skipped,
				"gap_total": total, "gaps": gaps, "truncated": total > len(gaps),
			}
			return Result{
				Value:   value,
				Summary: fmt.Sprintf("Scanned %d functions: %d changed, %d gaps shown of %d, %d skipped.", check.Scanned, check.Changed, len(gaps), total, check.Skipped),
				Detail:  detail,
			}, nil
		},
	})

	register(&Op{
		Name: "list_coverage_gaps", Verb: "gaps", Order: 4, Title: "List coverage gaps", ReadOnly: true,
		Summary: "Read uncovered function versions a page at a time, with missing line and branch evidence.",
		New:     func() any { return &gapsInput{} },
		Run: func(ctx context.Context, a *app.App, in any) (Result, error) {
			input := in.(*gapsInput)
			page, err := a.ListCoverageGaps(ctx, input.ChangedOnly, input.Limit, input.Cursor)
			if err != nil {
				return Result{}, err
			}
			detail := []string{}
			for _, gap := range page.Items {
				detail = append(detail, fmt.Sprintf("  %s (%s)", gap.SymbolKey, gap.Reason))
			}
			return Result{Value: page, Summary: fmt.Sprintf("%d coverage gaps; showing %d.", page.Total, len(page.Items)), Detail: detail}, nil
		},
	})

	register(&Op{
		Name: "get_symbol_context", Verb: "symbol", Order: 5, Title: "Get function context", ReadOnly: true,
		Summary: "Read one symbol, its bounded source span, and the tests that previously covered it.",
		New:     func() any { return &symbolInput{} },
		Run: func(ctx context.Context, a *app.App, in any) (Result, error) {
			input := in.(*symbolInput)
			result, err := a.GetSymbolContext(ctx, input.SymbolKey, input.IncludeSource)
			return Result{Value: result, Summary: "Loaded context for " + input.SymbolKey}, err
		},
	})

	register(&Op{
		Name: "select_affected_tests", Verb: "affected", Order: 10, Title: "Select affected tests",
		Summary: "Choose tests for changed functions from the previous inventory's coverage, falling back to the full suite when unsafe.",
		New:     func() any { return &languageInput{} },
		Run: func(ctx context.Context, a *app.App, in any) (Result, error) {
			input := in.(*languageInput)
			result, err := a.SelectAffectedTestsForLanguage(ctx, input.Language)
			if err != nil {
				return Result{}, err
			}
			summary := fmt.Sprintf("Selected %d affected tests.", len(result.TestKeys))
			if result.Fallback {
				summary = "Full-suite fallback: " + result.Reason
			}
			return Result{Value: result, Summary: summary, Detail: indent(result.TestKeys)}, nil
		},
	})

	register(&Op{
		Name: "run_tests", Verb: "test", Order: 11, Title: "Run tests",
		Summary: "Run a configured language's tests and record normalized results and coverage.",
		New:     func() any { return &testInput{} },
		Run: func(ctx context.Context, a *app.App, in any) (Result, error) {
			input := in.(*testInput)
			if input.Async {
				job, err := a.StartAsyncTestLanguage(ctx, input.Language, input.TestKeys, input.ProposalID, input.Affected)
				return Result{Value: job, Summary: "Queued test run " + job.ID}, err
			}
			selectors := input.TestKeys
			if input.Affected {
				if len(selectors) > 0 {
					return Result{}, fmt.Errorf("--affected cannot be combined with explicit test selectors")
				}
				selection, err := a.SelectAffectedTestsForLanguage(ctx, input.Language)
				if err != nil {
					return Result{}, err
				}
				if !selection.Fallback && len(selection.TestKeys) == 0 {
					return Result{}, fmt.Errorf("no changed functions have affected tests")
				}
				if !selection.Fallback {
					selectors = selection.TestKeys
				}
			}
			result, err := a.TestLanguage(ctx, input.Language, selectors)
			if err != nil {
				return Result{}, err
			}
			exit := 0
			if result.Status != "passed" {
				if exit = result.ExitCode; exit <= 0 {
					exit = 1
				}
			}
			detail := []string{
				fmt.Sprintf("  coverage mappings: %d", result.CoverageMappings),
				"  artifacts: " + result.ArtifactDirectory,
			}
			if result.InfrastructureErr != "" {
				detail = append(detail, "  infrastructure: "+result.InfrastructureErr)
			}
			if result.Failed+result.Errors > 0 {
				detail = append(detail, fmt.Sprintf("  read failures: testledger run --run-id %s --failures-only", result.RunID))
			}
			return Result{
				Value:   result,
				Summary: fmt.Sprintf("Run %s: %s (%d passed, %d failed, %d errors, %d skipped)", result.RunID, result.Status, result.Passed, result.Failed, result.Errors, result.Skipped),
				Detail:  detail, ExitCode: exit,
			}, nil
		},
	})

	register(&Op{
		Name: "get_test_run", Verb: "run", Order: 12, Title: "Read a test run", ReadOnly: true,
		Summary: "Read a recorded run's normalized cases a page at a time, optionally failures only.",
		New:     func() any { return &runInput{} },
		Run: func(ctx context.Context, a *app.App, in any) (Result, error) {
			input := in.(*runInput)
			if input.JobID != "" {
				job, run, page, err := a.GetAsyncTest(ctx, input.JobID, input.Limit, input.Cursor, input.FailuresOnly)
				if err != nil {
					return Result{}, err
				}
				return Result{Value: map[string]any{"job": job, "run": run, "cases": page}, Summary: "Job status: " + job.Status, Detail: caseLines(page.Items)}, nil
			}
			if input.RunID == "" {
				return Result{}, fmt.Errorf("run requires --run-id or --job-id")
			}
			run, page, err := a.Store.TestRun(ctx, input.RunID, clamp(input.Limit, 20, 100), input.Cursor, input.FailuresOnly)
			if err != nil {
				return Result{}, err
			}
			return Result{Value: map[string]any{"run": run, "cases": page}, Summary: fmt.Sprintf("Run %s: %s (%d of %d cases shown)", run.RunID, run.Status, len(page.Items), page.Total), Detail: caseLines(page.Items)}, nil
		},
	})

	register(&Op{
		Name: "get_failure_context", Verb: "failure", Order: 13, Title: "Get failure context", ReadOnly: true,
		Summary: "Read one normalized failure and its latest diagnosis, without loading the whole run.",
		New:     func() any { return &failureInput{} },
		Run: func(ctx context.Context, a *app.App, in any) (Result, error) {
			input := in.(*failureInput)
			result, err := a.Store.FailureContext(ctx, input.RunID, input.TestKey)
			if err != nil {
				return Result{}, err
			}
			return Result{Value: result, Summary: "Failure context for " + input.TestKey, Detail: []string{"  " + result.Result.Message}}, nil
		},
	})

	register(&Op{
		Name: "record_failure_diagnosis", Verb: "diagnose", Order: 14, Title: "Record a failure diagnosis",
		Summary: "Append an interpretation of a failure. Never overwrites the deterministic result.",
		New:     func() any { return &diagnosisInput{} },
		Run: func(ctx context.Context, a *app.App, in any) (Result, error) {
			input := in.(*diagnosisInput)
			result, err := a.RecordFailureDiagnosis(ctx, input.RunID, input.TestKey, input.Category, input.Explanation, input.CreatedBy)
			return Result{Value: result, Summary: "Recorded diagnosis for " + input.TestKey}, err
		},
	})

	register(&Op{
		Name: "propose_tests", Verb: "propose", Order: 6, Title: "Propose tests",
		Summary: "Record proposed test cases against exact current function versions. This does not approve them.",
		New:     func() any { return &proposeInput{} },
		Run: func(ctx context.Context, a *app.App, in any) (Result, error) {
			input := in.(*proposeInput)
			result, err := a.CreateProposal(ctx, input.Targets, input.Cases, input.Rationale, input.CreatedBy)
			if err != nil {
				return Result{}, err
			}
			return Result{Value: result, Summary: "Proposal " + result.ID + " recorded; a human decision is required.", Detail: []string{fmt.Sprintf("  %d targets, %d cases", len(result.Targets), len(result.Cases))}}, nil
		},
	})

	register(&Op{
		Name: "list_proposals", Verb: "proposals", Order: 7, Title: "List proposals", ReadOnly: true,
		Summary: "Read proposals with their targets, decisions, and link resolutions.",
		New:     func() any { return &proposalsInput{} },
		Run: func(ctx context.Context, a *app.App, in any) (Result, error) {
			input := in.(*proposalsInput)
			if input.ProposalID != "" {
				proposal, err := a.Store.GetProposal(ctx, input.ProposalID)
				if err != nil {
					return Result{}, err
				}
				return Result{Value: proposal, Summary: "Proposal " + proposal.ID + " is " + proposal.Status, Detail: linkLines(proposal.Links)}, nil
			}
			page, err := a.Store.ListProposals(ctx, input.Status, clamp(input.Limit, 20, 100), input.Cursor)
			if err != nil {
				return Result{}, err
			}
			detail := []string{}
			for _, proposal := range page.Items {
				detail = append(detail, fmt.Sprintf("  %s  %-12s %d targets", proposal.ID, proposal.Status, len(proposal.Targets)))
			}
			return Result{Value: page, Summary: fmt.Sprintf("%d proposals; showing %d.", page.Total, len(page.Items)), Detail: detail}, nil
		},
	})

	register(&Op{
		Name: "record_proposal_implementation", Verb: "implemented", Order: 9, Title: "Record implemented tests",
		Summary: "Record the exact native test IDs written for an approved proposal. Re-recording replaces the previous links.",
		New:     func() any { return &implementedInput{} },
		Run: func(ctx context.Context, a *app.App, in any) (Result, error) {
			input := in.(*implementedInput)
			result, err := a.MarkProposalImplemented(ctx, input.ProposalID, input.Links)
			return Result{Value: result, Summary: "Recorded " + fmt.Sprint(len(input.Links)) + " links; run tests to verify them."}, err
		},
	})

	register(&Op{
		Name: "record_proposal_decision", Verb: "decide", Order: 8, Title: "Record a human proposal decision", Decision: true,
		Summary: "Record a human's explicit approval or rejection of a proposal.",
		New:     func() any { return &decideInput{} },
		Run: func(ctx context.Context, a *app.App, in any) (Result, error) {
			input := in.(*decideInput)
			result, err := a.DecideProposal(ctx, input.ProposalID, input.Decision, input.DecidedBy, input.Reason)
			return Result{Value: result, Summary: "Recorded human decision: " + input.Decision}, err
		},
	})

	register(&Op{
		Name: "record_disposition", Verb: "skip", Order: 15, Title: "Record a human skip", Decision: true,
		Summary: "Record a human-approved decision to leave a function untested.",
		New:     func() any { return &skipInput{} },
		Run: func(ctx context.Context, a *app.App, in any) (Result, error) {
			input := in.(*skipInput)
			var expiry *time.Time
			if input.ExpiresAt != "" {
				parsed, err := time.Parse(time.RFC3339, input.ExpiresAt)
				if err != nil {
					return Result{}, err
				}
				expiry = &parsed
			}
			by := input.ApprovedBy
			if by == "" {
				by = "human"
			}
			if err := a.AddSkipBy(ctx, input.SymbolKey, input.Reason, input.Durable, expiry, by); err != nil {
				return Result{}, err
			}
			return Result{Value: map[string]any{"recorded": true, "symbol_key": input.SymbolKey, "durable": input.Durable}, Summary: "Recorded disposition for " + input.SymbolKey}, nil
		},
	})

	register(&Op{
		Name: "run_mutation", Verb: "mutation", Order: 16, Title: "Run the mutation adapter",
		Summary: "Run the configured mutation command and record normalized killed/survived signals.",
		New:     func() any { return &languageInput{} },
		Run: func(ctx context.Context, a *app.App, in any) (Result, error) {
			input := in.(*languageInput)
			result, err := a.RunMutation(ctx, input.Language)
			if err != nil {
				return Result{}, err
			}
			return Result{Value: result, Summary: fmt.Sprintf("Mutation run recorded %d signals.", len(result.Results))}, nil
		},
	})

	register(&Op{
		Name: "get_status", Verb: "status", Order: 2, Title: "Get ledger status", ReadOnly: true,
		Summary: "Summarize the inventory, outstanding gaps, active skips, and the latest run.",
		New:     func() any { return &emptyInput{} },
		Run: func(ctx context.Context, a *app.App, in any) (Result, error) {
			result, err := a.Status(ctx)
			if err != nil {
				return Result{}, err
			}
			detail := []string{fmt.Sprintf("  %d symbols, %d gaps, %d active skips", result.SymbolCount, result.OutstandingGaps, result.ActiveSkips)}
			if result.LatestTestRun != nil {
				detail = append(detail, fmt.Sprintf("  latest run %s: %s", result.LatestTestRun.RunID, result.LatestTestRun.Status))
			}
			return Result{Value: result, Summary: "Ledger status", Detail: detail}, nil
		},
	})
}

func clamp(value, fallback, max int) int {
	if value <= 0 {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}

func indent(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, "  "+value)
	}
	return out
}

func caseLines(cases []model.TestCaseResult) []string {
	out := []string{}
	for _, item := range cases {
		line := fmt.Sprintf("  %-8s %s", item.Outcome, item.TestKey)
		if item.Message != "" {
			line += "\n           " + item.Message
		}
		out = append(out, line)
	}
	return out
}

func linkLines(links []model.IntendedTestLink) []string {
	out := []string{}
	for _, link := range links {
		out = append(out, fmt.Sprintf("  %-14s %s <- %s", link.Resolution, link.SymbolKey, link.TestKey))
	}
	return out
}
