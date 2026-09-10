package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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
