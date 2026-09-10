package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/model"
)

func copyFixture(t *testing.T) string {
	t.Helper()
	source := filepath.Join("testdata", "python_project")
	destination := t.TempDir()
	err := filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, contents, info.Mode())
	})
	if err != nil {
		t.Fatal(err)
	}
	return destination
}

func openFixture(t *testing.T) (*App, string) {
	t.Helper()
	root := copyFixture(t)
	application, err := Open(root, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	return application, root
}

func TestProposalLifecycleAndNextActions(t *testing.T) {
	application, _ := openFixture(t)
	ctx := context.Background()
	if _, err := application.Check(ctx); err != nil {
		t.Fatal(err)
	}
	firstPage, err := application.ListCoverageGaps(ctx, false, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if firstPage.Total != 2 || len(firstPage.Items) != 1 || firstPage.NextCursor != 1 {
		t.Fatalf("unexpected first page: %+v", firstPage)
	}
	secondPage, err := application.ListCoverageGaps(ctx, false, 1, firstPage.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPage.Items) != 1 || secondPage.Items[0].SymbolKey == firstPage.Items[0].SymbolKey {
		t.Fatalf("unexpected second page: %+v", secondPage)
	}
	next, err := application.NextActions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if next.State != "proposal_required" {
		t.Fatalf("got state %q", next.State)
	}
	cases := []model.ProposalCase{{Name: "test_add", TestFile: "tests/test_mathy.py", Description: "add two integers", Assertions: []string{"result equals 5"}}}
	proposal, err := application.CreateProposal(ctx, []string{"python:src/mathy.py:add"}, cases, "cover arithmetic behavior", "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	next, err = application.NextActions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if next.State != "awaiting_human_decision" {
		t.Fatalf("got state %q", next.State)
	}
	proposal, err = application.DecideProposal(ctx, proposal.ID, "approved", "developer@example.com", "case is focused")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Status != "approved" {
		t.Fatalf("got status %q", proposal.Status)
	}
	proposal, err = application.MarkProposalImplemented(ctx, proposal.ID, []model.IntendedTestLink{{SymbolKey: "python:src/mathy.py:add", TestKey: "tests/test_m.py::test_add"}})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Status != "implemented" {
		t.Fatalf("got status %q", proposal.Status)
	}
	links, err := application.Store.ProposalLinks(ctx, proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].Verified {
		t.Fatalf("unexpected links: %+v", links)
	}
	next, err = application.NextActions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if next.State != "tests_to_verify" {
		t.Fatalf("got state %q", next.State)
	}
}

func TestChangedTargetMakesProposalObsolete(t *testing.T) {
	application, root := openFixture(t)
	ctx := context.Background()
	if _, err := application.Check(ctx); err != nil {
		t.Fatal(err)
	}
	proposal, err := application.CreateProposal(ctx, []string{"python:src/mathy.py:classify"}, []model.ProposalCase{{Name: "test_classify", TestFile: "tests/test_mathy.py", Description: "classify values", Assertions: []string{"negative is labeled"}}}, "cover branches", "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "src", "mathy.py")
	// Change the target body, not merely formatting.
	updated := "def add(left, right):\n    return left + right\n\n\ndef classify(value):\n    if value == 0:\n        return \"zero\"\n    if value < 0:\n        return \"negative\"\n    return \"nonnegative\"\n"
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := application.DecideProposal(ctx, proposal.ID, "approved", "developer@example.com", "looks good"); err == nil {
		t.Fatal("expected stale proposal error")
	}
	stored, err := application.Store.GetProposal(ctx, proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "obsolete" {
		t.Fatalf("got status %q", stored.Status)
	}
}

func TestImplementationRequiresEveryTargetAndRejectsPytestFlags(t *testing.T) {
	application, _ := openFixture(t)
	ctx := context.Background()
	if _, err := application.Check(ctx); err != nil {
		t.Fatal(err)
	}
	proposal, err := application.CreateProposal(ctx, []string{"python:src/mathy.py:add", "python:src/mathy.py:classify"}, []model.ProposalCase{{Name: "test_math", TestFile: "tests/test_mathy.py", Description: "cover math", Assertions: []string{"results match"}}}, "cover both functions", "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = application.DecideProposal(ctx, proposal.ID, "approved", "human", "approved"); err != nil {
		t.Fatal(err)
	}
	_, err = application.MarkProposalImplemented(ctx, proposal.ID, []model.IntendedTestLink{{SymbolKey: "python:src/mathy.py:add", TestKey: "tests/test_mathy.py::test_add"}})
	if err == nil {
		t.Fatal("implementation without a link for every target was accepted")
	}
	if _, err = application.StartAsyncTest(ctx, []string{"-k", "add"}, ""); err == nil {
		t.Fatal("arbitrary pytest flag was accepted")
	}
}

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
