// Normalising each runner's native result format into one shape. Parsing
// lives here; policy does not.
package app

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danielcsee/sciterm/testledger/internal/model"
)

type pytestPayload struct {
	ExitStatus int                    `json:"exit_status"`
	Tests      []model.TestCaseResult `json:"tests"`
}

type coveragePayload struct {
	Files map[string]coverageFile `json:"files"`
}

type coverageFile struct {
	ExecutedLines   []int               `json:"executed_lines"`
	MissingLines    []int               `json:"missing_lines"`
	Contexts        map[string][]string `json:"contexts"`
	MissingBranches [][]int             `json:"missing_branches"`
}

type goEvent struct {
	Action, Package, Test, Output string
	Elapsed                       float64
}

func readPytestResults(path string, maxOutput int64) (pytestPayload, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return pytestPayload{}, err
	}
	var payload pytestPayload
	if err := json.Unmarshal(contents, &payload); err != nil {
		return payload, err
	}
	for i := range payload.Tests {
		payload.Tests[i].Traceback = truncate(payload.Tests[i].Traceback, maxOutput)
		payload.Tests[i].Stdout = truncate(payload.Tests[i].Stdout, maxOutput)
		payload.Tests[i].Stderr = truncate(payload.Tests[i].Stderr, maxOutput)
	}
	return payload, nil
}

func parseGoTestJSON(path string, max int64) ([]model.TestCaseResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	type state struct {
		output  strings.Builder
		elapsed float64
		outcome string
	}
	states := map[string]*state{}
	packageOutput := map[string]*strings.Builder{}
	packageFailures := map[string]goEvent{}
	validEvents := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var event goEvent
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		validEvents++
		if event.Test == "" {
			if event.Package != "" && event.Output != "" {
				builder := packageOutput[event.Package]
				if builder == nil {
					builder = &strings.Builder{}
					packageOutput[event.Package] = builder
				}
				builder.WriteString(event.Output)
			}
			if event.Action == "fail" && event.Package != "" {
				packageFailures[event.Package] = event
			}
			continue
		}
		key := event.Package + "::" + event.Test
		s := states[key]
		if s == nil {
			s = &state{}
			states[key] = s
		}
		s.output.WriteString(event.Output)
		if event.Elapsed > 0 {
			s.elapsed = event.Elapsed
		}
		if event.Action == "pass" || event.Action == "fail" || event.Action == "skip" {
			s.outcome = map[string]string{"pass": "passed", "fail": "failed", "skip": "skipped"}[event.Action]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if validEvents == 0 {
		return nil, errors.New("go test emitted no JSON events")
	}
	for pkg, event := range packageFailures {
		hasTest := false
		for key := range states {
			if strings.HasPrefix(key, pkg+"::") {
				hasTest = true
				break
			}
		}
		if !hasTest {
			states[pkg+"::<package>"] = &state{outcome: "failed", elapsed: event.Elapsed}
		}
	}
	keys := make([]string, 0, len(states))
	for key := range states {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]model.TestCaseResult, 0, len(keys))
	for _, key := range keys {
		s := states[key]
		if strings.HasSuffix(key, "::<package>") {
			pkg := strings.TrimSuffix(key, "::<package>")
			if output := packageOutput[pkg]; output != nil {
				s.output.WriteString(output.String())
			}
		}
		outcome := s.outcome
		if outcome == "" {
			outcome = "error"
		}
		out = append(out, model.TestCaseResult{TestKey: key, Outcome: outcome, Phase: "call", DurationSeconds: s.elapsed, Message: truncate(s.output.String(), max)})
	}
	return out, nil
}

func parseJSTestJSON(path string, max int64) ([]model.TestCaseResult, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var payload struct {
		TestResults []struct {
			Name             string `json:"name"`
			AssertionResults []struct {
				AncestorTitles  []string `json:"ancestorTitles"`
				Title, Status   string
				Duration        float64
				FailureMessages []string `json:"failureMessages"`
			} `json:"assertionResults"`
		} `json:"testResults"`
	}
	if err := json.Unmarshal(contents, &payload); err != nil {
		return nil, err
	}
	if payload.TestResults == nil {
		return nil, errors.New("expected Jest/Vitest JSON testResults")
	}
	result := []model.TestCaseResult{}
	for _, suite := range payload.TestResults {
		for _, test := range suite.AssertionResults {
			parts := append([]string{filepath.ToSlash(suite.Name)}, test.AncestorTitles...)
			parts = append(parts, test.Title)
			outcome := map[string]string{"passed": "passed", "failed": "failed", "pending": "skipped", "todo": "skipped", "skipped": "skipped"}[test.Status]
			if outcome == "" {
				outcome = "error"
			}
			result = append(result, model.TestCaseResult{TestKey: strings.Join(parts, "::"), Outcome: outcome, Phase: "call", DurationSeconds: test.Duration / 1000, Message: truncate(strings.Join(test.FailureMessages, "\n"), max)})
		}
	}
	return result, nil
}

func categorizeCases(cases []model.TestCaseResult) {
	for i := range cases {
		if cases[i].Outcome == "passed" || cases[i].Outcome == "skipped" {
			continue
		}
		text := strings.ToLower(cases[i].Message + "\n" + cases[i].Traceback)
		switch {
		case cases[i].Phase == "collection":
			cases[i].FailureCategory = "collection"
		case strings.Contains(text, "importerror") || strings.Contains(text, "modulenotfounderror"):
			cases[i].FailureCategory = "import"
		case cases[i].Phase == "setup":
			cases[i].FailureCategory = "fixture_setup"
		case cases[i].Phase == "teardown":
			cases[i].FailureCategory = "teardown"
		case strings.Contains(text, "assertionerror"):
			cases[i].FailureCategory = "assertion"
		case strings.Contains(text, "timeout"):
			cases[i].FailureCategory = "timeout"
		default:
			cases[i].FailureCategory = "application_exception"
		}
	}
}

func countCases(result *model.TestRunResult) {
	for _, tc := range result.Cases {
		switch tc.Outcome {
		case "passed":
			result.Passed++
		case "failed":
			result.Failed++
		case "skipped":
			result.Skipped++
		default:
			result.Errors++
		}
	}
}
