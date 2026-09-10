package ops

import (
	"reflect"
	"testing"
)

// The drift this package exists to prevent: a capability on one transport and
// missing from the other. Both surfaces are generated from one registry, so
// parity is structural -- this test states it anyway, because the Only field
// can silently reintroduce the split.
func TestEveryOperationReachesBothSurfaces(t *testing.T) {
	cli := map[string]bool{}
	for _, op := range For(SurfaceCLI) {
		cli[op.Name] = true
	}
	for _, op := range For(SurfaceMCP) {
		if !cli[op.Name] && op.Only == "" {
			t.Errorf("%s reaches MCP but not the CLI", op.Name)
		}
	}
	for _, op := range All() {
		if op.Only != "" && op.Only != SurfaceCLI && op.Only != SurfaceMCP {
			t.Errorf("%s has an unknown Only value %q", op.Name, op.Only)
		}
	}
}

func TestEveryOperationIsWellFormed(t *testing.T) {
	verbs := map[string]string{}
	for _, op := range All() {
		if op.Verb == "" || op.Name == "" || op.Summary == "" || op.Title == "" {
			t.Errorf("%s is missing a name, verb, title or summary", op.Name)
		}
		if previous, seen := verbs[op.Verb]; seen {
			t.Errorf("verb %q is claimed by both %s and %s", op.Verb, previous, op.Name)
		}
		verbs[op.Verb] = op.Name
		if op.New == nil || op.Run == nil {
			t.Fatalf("%s has no input constructor or handler", op.Name)
		}
		for _, field := range Fields(op.New()) {
			if field.Desc == "" {
				t.Errorf("%s.%s has no description, so neither surface can document it", op.Name, field.Name)
			}
		}
		schema := InputSchema(op)
		if schema["type"] != "object" {
			t.Errorf("%s produced a non-object schema", op.Name)
		}
	}
}

// Embedded Page and Confirm fields must appear as ordinary arguments, not as a
// nested object, or paging would exist on one surface only.
func TestEmbeddedFieldsAreFlattened(t *testing.T) {
	op, ok := ByName("list_coverage_gaps")
	if !ok {
		t.Fatal("list_coverage_gaps is not registered")
	}
	names := map[string]bool{}
	for _, field := range Fields(op.New()) {
		names[field.Name] = true
	}
	for _, want := range []string{"changed_only", "limit", "cursor"} {
		if !names[want] {
			t.Errorf("missing flattened field %q", want)
		}
	}
	properties := InputSchema(op)["properties"].(map[string]any)
	if _, nested := properties["Page"]; nested {
		t.Error("Page leaked into the schema as a nested object")
	}
}

// The standard flag package stops at the first non-flag argument, which made
// `decide ID --json` silently drop the flag. Order must not matter.
func TestFlagsParseAfterPositionals(t *testing.T) {
	op, _ := ByName("record_disposition")
	input, err := ParseArgs(op, []string{"python:api/x.py:f", "--reason", "needs a database", "--durable"})
	if err != nil {
		t.Fatal(err)
	}
	skip := input.(*skipInput)
	if skip.SymbolKey != "python:api/x.py:f" || skip.Reason != "needs a database" || !skip.Durable {
		t.Fatalf("parsed %+v", skip)
	}
}

func TestRequiredArgumentsAreEnforced(t *testing.T) {
	op, _ := ByName("record_disposition")
	if _, err := ParseArgs(op, []string{"python:api/x.py:f"}); err == nil {
		t.Fatal("expected a missing --reason error")
	}
}

func TestEnumsAreRejectedAtTheCommandLine(t *testing.T) {
	op, _ := ByName("record_proposal_decision")
	_, err := ParseArgs(op, []string{"p1", "--decision", "maybe", "--decided-by", "x", "--reason", "y"})
	if err == nil {
		t.Fatal("expected an enum validation error")
	}
}

// Hyphen and underscore spellings must both work: the wire name uses
// underscores, but a flag reads better hyphenated.
func TestFlagNameSpellings(t *testing.T) {
	op, _ := ByName("get_test_run")
	input, err := ParseArgs(op, []string{"--run-id", "r1", "--failures_only"})
	if err != nil {
		t.Fatal(err)
	}
	run := input.(*runInput)
	if run.RunID != "r1" || !run.FailuresOnly {
		t.Fatalf("parsed %+v", run)
	}
}

func TestUnknownFlagIsAnError(t *testing.T) {
	op, _ := ByName("get_status")
	if _, err := ParseArgs(op, []string{"--nope"}); err == nil {
		t.Fatal("expected an unknown flag error")
	}
}

// MCP must never block on a test run, whatever the caller asked for.
func TestForceAsyncIsAvailableToTheMCPTransport(t *testing.T) {
	op, _ := ByName("run_tests")
	input := op.New()
	forcer, ok := input.(interface{ ForceAsync() })
	if !ok {
		t.Fatal("run_tests input cannot be forced async")
	}
	forcer.ForceAsync()
	if !reflect.ValueOf(input).Elem().FieldByName("Async").Bool() {
		t.Fatal("ForceAsync did not set Async")
	}
}
