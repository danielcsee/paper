package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/danielcsee/sciterm/testledger/internal/model"
)

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

func TestCategorizeCases(t *testing.T) {
	cases := []model.TestCaseResult{
		{Outcome: "failed", Phase: "call", Traceback: "AssertionError: mismatch"},
		{Outcome: "error", Phase: "collection", Traceback: "SyntaxError"},
		{Outcome: "error", Phase: "setup", Traceback: "fixture exploded"},
	}
	categorizeCases(cases)
	if cases[0].FailureCategory != "assertion" {
		t.Fatalf("got %q", cases[0].FailureCategory)
	}
	if cases[1].FailureCategory != "collection" {
		t.Fatalf("got %q", cases[1].FailureCategory)
	}
	if cases[2].FailureCategory != "fixture_setup" {
		t.Fatalf("got %q", cases[2].FailureCategory)
	}
}
