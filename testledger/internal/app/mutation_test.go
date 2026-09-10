package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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
