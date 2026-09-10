package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/danielcsee/sciterm/testledger/internal/model"
)

func TestCoverageDetailsIncludePythonBranchGaps(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "coverage.json")
	payload := `{"files":{"pkg/a.py":{"executed_lines":[1,2],"missing_lines":[3],"contexts":{},"missing_branches":[[2,3],[2,5]]}}}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	symbol := model.Symbol{Language: "python", Path: "pkg/a.py", QualifiedName: "f", StartLine: 1, EndLine: 4, ExecutableLines: []int{2, 3}, SemanticHash: "h"}
	details, err := coverageDetails(path, root, []model.Symbol{symbol})
	if err != nil {
		t.Fatal(err)
	}
	if len(details) != 1 || len(details[0].MissingLines) != 1 || len(details[0].MissingBranches) != 2 {
		t.Fatalf("unexpected details: %+v", details)
	}
}

func TestAttributeCoverageUsesPerTestContextsAndOwnLines(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "coverage.json")
	contents := `{
  "files": {
    "pkg/example.py": {
      "executed_lines": [1, 2, 3, 5],
      "missing_lines": [4, 6],
      "contexts": {
        "1": [""],
        "2": ["tests/test_example.py::test_one"],
        "3": ["tests/test_example.py::test_one"],
        "5": ["tests/test_example.py::test_nested"]
      }
    }
  }
}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	symbols := []model.Symbol{{
		Language: "python", Path: "pkg/example.py", QualifiedName: "outer", SemanticHash: "abc",
		StartLine: 1, EndLine: 6, ExecutableLines: []int{2, 3, 4},
	}}

	observations, err := attributeCoverage(path, root, symbols)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 {
		t.Fatalf("got %d observations, want 1", len(observations))
	}
	got := observations[0]
	if got.TestKey != "tests/test_example.py::test_one" {
		t.Fatalf("unexpected test key %q", got.TestKey)
	}
	if got.ExecutedCount != 2 || got.ExecutableCount != 3 {
		t.Fatalf("unexpected counts: %+v", got)
	}
	if got.LinePercent < 66.6 || got.LinePercent > 66.7 {
		t.Fatalf("unexpected percent %f", got.LinePercent)
	}
}
