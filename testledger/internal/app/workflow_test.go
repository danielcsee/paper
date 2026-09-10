package app

import (
	"context"
	"testing"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/model"
)

func TestNextActionsRequestsInitialScan(t *testing.T) {
	application, _ := openFixture(t)
	next, err := application.NextActions(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if next.State != "scan_required" {
		t.Fatalf("got state %q", next.State)
	}
}

// A proposal whose target is dispositioned used to stall forever: the link
// could never be coverage-verified, so the proposal stayed `implemented` and
// NextActions returned run_implemented_tests on every call. An agent obeying
// it would rerun the suite indefinitely.

// A proposal whose target is dispositioned used to stall forever: the link
// could never be coverage-verified, so the proposal stayed `implemented` and
// NextActions returned run_implemented_tests on every call. An agent obeying
// it would rerun the suite indefinitely.
func TestDispositionedTargetDoesNotStallProposal(t *testing.T) {
	application, _ := openFixture(t)
	ctx := context.Background()
	if _, err := application.Check(ctx); err != nil {
		t.Fatal(err)
	}
	const symbol = "python:src/mathy.py:add"
	cases := []model.ProposalCase{{Name: "test_add", TestFile: "tests/test_mathy.py", Description: "add", Assertions: []string{"5"}}}
	proposal, err := application.CreateProposal(ctx, []string{symbol}, cases, "cover add", "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = application.DecideProposal(ctx, proposal.ID, "approved", "human", "ok"); err != nil {
		t.Fatal(err)
	}
	if _, err = application.MarkProposalImplemented(ctx, proposal.ID, []model.IntendedTestLink{{SymbolKey: symbol, TestKey: "tests/test_mathy.py::test_add"}}); err != nil {
		t.Fatal(err)
	}
	if err = application.AddSkip(ctx, symbol, "needs a live database", false, nil); err != nil {
		t.Fatal(err)
	}
	links, err := application.Store.ProposalLinks(ctx, proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].Resolution != model.LinkDispositioned {
		t.Fatalf("link resolution = %+v, want dispositioned", links)
	}
	if links[0].Verified {
		t.Fatal("a dispositioned link must not claim coverage evidence")
	}
	unresolved, err := application.Store.UnresolvedLinkCount(ctx, proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unresolved != 0 {
		t.Fatalf("unresolved links = %d, want 0", unresolved)
	}
}

// With no run yet judging the proposal, NextActions should ask for one exactly
// until a run has happened -- never in a loop after it.

// With no run yet judging the proposal, NextActions should ask for one exactly
// until a run has happened -- never in a loop after it.
func TestNextActionsStopsAskingForRunsThatCannotHelp(t *testing.T) {
	application, _ := openFixture(t)
	ctx := context.Background()
	if _, err := application.Check(ctx); err != nil {
		t.Fatal(err)
	}
	const symbol = "python:src/mathy.py:add"
	cases := []model.ProposalCase{{Name: "test_add", TestFile: "tests/test_mathy.py", Description: "add", Assertions: []string{"5"}}}
	proposal, err := application.CreateProposal(ctx, []string{symbol}, cases, "cover add", "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = application.DecideProposal(ctx, proposal.ID, "approved", "human", "ok"); err != nil {
		t.Fatal(err)
	}
	// Deliberately link a test key that will never cover the symbol.
	if _, err = application.MarkProposalImplemented(ctx, proposal.ID, []model.IntendedTestLink{{SymbolKey: symbol, TestKey: "tests/test_mathy.py::test_does_not_exist"}}); err != nil {
		t.Fatal(err)
	}
	next, err := application.NextActions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if next.State != "tests_to_verify" {
		t.Fatalf("before any run, state = %q, want tests_to_verify", next.State)
	}
	// Record a completed, passing run rather than shelling out to pytest: the
	// subject here is the state machine, not the runner.
	run := model.TestRunResult{
		RunID: "test_fixture_run", Status: "passed", ExitCode: 0, Passed: 1,
		StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC().Add(time.Second),
		ArtifactDirectory: t.TempDir(),
	}
	if err = application.Store.BeginTestRun(ctx, run, "", []string{"pytest"}); err != nil {
		t.Fatal(err)
	}
	if err = application.Store.FinishTestRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	next, err = application.NextActions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if next.State == "tests_to_verify" {
		t.Fatal("NextActions still asks for a run after one already judged the proposal: livelock")
	}
	if next.State != "links_unverified" {
		t.Fatalf("after a run, state = %q, want links_unverified", next.State)
	}
	if len(next.Actions) == 0 || next.Actions[0].SymbolKey != symbol {
		t.Fatalf("expected the blocking symbol to be named, got %+v", next.Actions)
	}
}
