package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/model"
)

func TestVersionScopedSkipDoesNotApplyToChangedFunction(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	symbol := model.Symbol{Language: "python", Path: "a.py", QualifiedName: "f", Kind: "function", SemanticHash: "v1", SignatureHash: "s", BodyHash: "b"}
	if err := db.SaveInventory(ctx, "run1", t.TempDir(), time.Now(), []model.Symbol{symbol}); err != nil {
		t.Fatal(err)
	}
	if err := db.AddSkip(ctx, symbol.Key(), "v1", "generated", "human", nil); err != nil {
		t.Fatal(err)
	}
	active, err := db.ActiveDisposition(ctx, symbol.Key(), "v1", time.Now())
	if err != nil || !active {
		t.Fatalf("v1 skip not active: %v", err)
	}
	active, err = db.ActiveDisposition(ctx, symbol.Key(), "v2", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if active {
		t.Fatal("version-scoped skip leaked to changed function")
	}
}

func TestFailureDiagnosisDoesNotReplaceRunnerEvidence(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	run := model.TestRunResult{RunID: "test1", StartedAt: now, ArtifactDirectory: t.TempDir()}
	if err := db.BeginTestRun(ctx, run, "", []string{"pytest"}); err != nil {
		t.Fatal(err)
	}
	run.Status = "failed"
	run.ExitCode = 1
	run.FinishedAt = now.Add(time.Second)
	run.Cases = []model.TestCaseResult{{TestKey: "tests/test_a.py::test_a", Outcome: "failed", Phase: "call", FailureCategory: "assertion", Message: "expected 2"}}
	if err := db.FinishTestRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	diagnosis := model.FailureDiagnosis{RunID: "test1", TestKey: "tests/test_a.py::test_a", Category: "product_bug", Explanation: "implementation returned one", CreatedBy: "agent", CreatedAt: now.Format(time.RFC3339Nano)}
	if err := db.AddFailureDiagnosis(ctx, diagnosis); err != nil {
		t.Fatal(err)
	}
	result, err := db.FailureContext(ctx, diagnosis.RunID, diagnosis.TestKey)
	if err != nil {
		t.Fatal(err)
	}
	if result.Result.FailureCategory != "assertion" {
		t.Fatalf("runner category changed to %q", result.Result.FailureCategory)
	}
	if result.Diagnosis == nil || result.Diagnosis.Category != "product_bug" {
		t.Fatalf("missing diagnosis: %+v", result)
	}
}
