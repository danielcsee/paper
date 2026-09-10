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

func TestStructuredResultParsers(t *testing.T) {
	dir := t.TempDir()
	goPath := filepath.Join(dir, "go.json")
	if err := os.WriteFile(goPath, []byte("{\"Action\":\"run\",\"Package\":\"pkg\",\"Test\":\"TestOne\"}\n{\"Action\":\"pass\",\"Package\":\"pkg\",\"Test\":\"TestOne\",\"Elapsed\":0.1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases, err := parseGoTestJSON(goPath, 1000)
	if err != nil || len(cases) != 1 || cases[0].Outcome != "passed" {
		t.Fatalf("Go parse: %+v %v", cases, err)
	}
	jsPath := filepath.Join(dir, "js.json")
	if err := os.WriteFile(jsPath, []byte(`{"testResults":[{"name":"src/a.test.ts","assertionResults":[{"ancestorTitles":["math"],"title":"adds","status":"failed","duration":5,"failureMessages":["nope"]}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cases, err = parseJSTestJSON(jsPath, 1000)
	if err != nil || len(cases) != 1 || cases[0].TestKey != "src/a.test.ts::math::adds" {
		t.Fatalf("JS parse: %+v %v", cases, err)
	}
}

func TestGoRunnerPersistsStructuredResults(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod":       "module example.test/sample\n\ngo 1.22\n",
		"math.go":      "package sample\nfunc Add(a,b int) int { return a+b }\n",
		"math_test.go": "package sample\nimport \"testing\"\nfunc TestAdd(t *testing.T){if Add(2,3)!=5{t.Fatal(\"bad\")}}\n",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := `schema_version=1
database=".testledger/db.sqlite"
artifact_directory=".testledger/artifacts"
[[languages]]
name="go"
include=["**/*.go"]
exclude=["**/*_test.go"]
[languages.test]
runner="go-test-json"
command=["go","test","-json","-coverprofile={coverage_file}","./..."]
test_roots=["."]
[languages.coverage]
format="go-coverprofile"
minimum_line_percent=80
[execution]
timeout_seconds=60
max_parallel_runs=1
environment_allowlist=["PATH","HOME","GOCACHE","GOMODCACHE"]
`
	if err := os.WriteFile(filepath.Join(root, "testledger.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := Open(root, "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	result, err := a.TestLanguage(context.Background(), "go", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "passed" || result.Passed != 1 || len(result.Cases) != 1 {
		t.Fatalf("unexpected Go run: %+v", result)
	}
}

func TestMutationAdapterPersistsNormalizedSignals(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "mutate.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' '{\"mutants\":[{\"symbol_key\":\"python:a.py:f\",\"operator\":\"negate\",\"status\":\"killed\"}]}' > \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := `schema_version=1
database=".testledger/db.sqlite"
artifact_directory=".testledger/artifacts"
[[languages]]
name="python"
include=["**/*.py"]
[languages.test]
runner="pytest"
command=["python3","-m","pytest"]
test_roots=["tests"]
[languages.coverage]
minimum_line_percent=80
[languages.mutation]
format="testledger-json"
command=["` + script + `","{results_file}"]
[execution]
max_parallel_runs=1
environment_allowlist=["PATH"]
`
	if err := os.WriteFile(filepath.Join(root, "testledger.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := Open(root, "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	run, err := a.RunMutation(context.Background(), "python")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "passed" || len(run.Results) != 1 || run.Results[0].Status != "killed" {
		t.Fatalf("unexpected mutation run: %+v", run)
	}
}
