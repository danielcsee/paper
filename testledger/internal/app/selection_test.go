package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/model"
	"github.com/danielcsee/sciterm/testledger/internal/store"
)

func TestAffectedSelectionUsesPreviousCoverage(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	configText := `schema_version=1
database=".testledger/db.sqlite"
artifact_directory=".testledger/artifacts"
[[languages]]
name="python"
python="python3"
include=["src/**/*.py"]
[languages.test]
runner="pytest"
command=["python3","-m","pytest"]
test_roots=["tests"]
[languages.coverage]
source=["src"]
minimum_line_percent=80
[execution]
max_parallel_runs=1
`
	if err := os.WriteFile(filepath.Join(root, "testledger.toml"), []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(root, "src", "mathy.py")
	if err := os.WriteFile(sourcePath, []byte("def add(a, b):\n    return a + b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := Open(root, "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	_, symbols, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	run := model.TestRunResult{RunID: "baseline", StartedAt: now, FinishedAt: now, Status: "passed", ExitCode: 0, ArtifactDirectory: root, Cases: []model.TestCaseResult{{TestKey: "tests/test_mathy.py::test_add", Outcome: "passed", Phase: "call"}}}
	if err := a.Store.BeginTestRun(context.Background(), run, "", []string{"pytest"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.FinishTestRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.SaveCoverage(context.Background(), run.RunID, []store.CoverageObservation{{TestKey: run.Cases[0].TestKey, SymbolKey: symbols[0].Key(), SemanticHash: symbols[0].SemanticHash, ExecutedLines: []int{2}, ExecutedCount: 1, ExecutableCount: 1, LinePercent: 100}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("def add(a, b):\n    return a + b + 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	selection, err := a.SelectAffectedTests(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if selection.Fallback || len(selection.TestKeys) != 1 || selection.TestKeys[0] != run.Cases[0].TestKey {
		t.Fatalf("unexpected selection: %+v", selection)
	}
}
