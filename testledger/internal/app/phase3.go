package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/config"
	"github.com/danielcsee/sciterm/testledger/internal/model"
	"github.com/danielcsee/sciterm/testledger/internal/store"
)

func (a *App) language(name string) (config.Language, error) {
	if name == "" && len(a.Config.Languages) == 1 {
		return a.Config.Languages[0], nil
	}
	for _, language := range a.Config.Languages {
		if language.Name == name {
			return language, nil
		}
	}
	return config.Language{}, fmt.Errorf("language %q is not configured", name)
}

// SelectAffectedTests uses only the immediately previous inventory and its
// persisted dynamic coverage. An unmapped changed/new symbol deliberately
// selects the full suite, favoring correctness over an unsafe empty selection.
func (a *App) SelectAffectedTests(ctx context.Context) (model.AffectedTests, error) {
	current, err := a.discover(ctx)
	if err != nil {
		return model.AffectedTests{}, err
	}
	ids, err := a.Store.RecentInventoryIDs(ctx, 2)
	if err != nil {
		return model.AffectedTests{}, err
	}
	if len(ids) == 0 {
		if _, _, err := a.Scan(ctx); err != nil {
			return model.AffectedTests{}, err
		}
		ids, err = a.Store.RecentInventoryIDs(ctx, 2)
		if err != nil {
			return model.AffectedTests{}, err
		}
	} else {
		latest, loadErr := a.Store.SymbolsForInventory(ctx, ids[0])
		if loadErr != nil {
			return model.AffectedTests{}, loadErr
		}
		if !sameInventory(current, latest) {
			if _, _, err := a.Scan(ctx); err != nil {
				return model.AffectedTests{}, err
			}
			ids, err = a.Store.RecentInventoryIDs(ctx, 2)
			if err != nil {
				return model.AffectedTests{}, err
			}
		}
	}
	selection := model.AffectedTests{InventoryRunID: ids[0], Symbols: []model.AffectedSymbol{}, TestKeys: []string{}}
	if len(ids) < 2 {
		selection.Fallback, selection.Reason = true, "no prior inventory exists"
		return selection, nil
	}
	previous, err := a.Store.SymbolsForInventory(ctx, ids[1])
	if err != nil {
		return selection, err
	}
	tests := map[string]bool{}
	seen := map[string]bool{}
	for _, symbol := range current {
		seen[symbol.Key()] = true
		old, existed := previous[symbol.Key()]
		if existed && old.SemanticHash == symbol.SemanticHash {
			continue
		}
		affected := model.AffectedSymbol{SymbolKey: symbol.Key(), CurrentHash: symbol.SemanticHash, Tests: []string{}}
		if existed {
			affected.PreviousHash = old.SemanticHash
			affected.Tests, err = a.Store.CoveringTestsForVersion(ctx, symbol.Key(), old.SemanticHash)
			if err != nil {
				return selection, err
			}
		}
		if len(affected.Tests) == 0 {
			affected.SelectionReason = "no historical coverage mapping; full-suite fallback required"
			selection.Fallback = true
		} else {
			affected.SelectionReason = "selected from passing tests that covered the previous function version"
			for _, key := range affected.Tests {
				tests[key] = true
			}
		}
		selection.Symbols = append(selection.Symbols, affected)
	}
	for key, old := range previous {
		if seen[key] {
			continue
		}
		affected := model.AffectedSymbol{SymbolKey: key, PreviousHash: old.SemanticHash, Tests: []string{}}
		affected.Tests, err = a.Store.CoveringTestsForVersion(ctx, key, old.SemanticHash)
		if err != nil {
			return selection, err
		}
		if len(affected.Tests) == 0 {
			affected.SelectionReason = "deleted symbol has no historical coverage mapping; full-suite fallback required"
			selection.Fallback = true
		} else {
			affected.SelectionReason = "selected because this test covered a deleted function"
			for _, test := range affected.Tests {
				tests[test] = true
			}
		}
		selection.Symbols = append(selection.Symbols, affected)
	}
	for key := range tests {
		selection.TestKeys = append(selection.TestKeys, key)
	}
	sort.Slice(selection.Symbols, func(i, j int) bool { return selection.Symbols[i].SymbolKey < selection.Symbols[j].SymbolKey })
	sort.Strings(selection.TestKeys)
	if selection.Fallback {
		selection.Reason = "at least one changed symbol has no safe historical test mapping"
	}
	return selection, nil
}

func (a *App) SelectAffectedTestsForLanguage(ctx context.Context, language string) (model.AffectedTests, error) {
	selection, err := a.SelectAffectedTests(ctx)
	if err != nil || language == "" {
		return selection, err
	}
	if _, err := a.language(language); err != nil {
		return model.AffectedTests{}, err
	}
	filtered := model.AffectedTests{InventoryRunID: selection.InventoryRunID, Symbols: []model.AffectedSymbol{}, TestKeys: []string{}}
	if selection.Fallback && len(selection.Symbols) == 0 {
		filtered.Fallback = true
		filtered.Reason = selection.Reason
	}
	tests := map[string]bool{}
	for _, symbol := range selection.Symbols {
		if !strings.HasPrefix(symbol.SymbolKey, language+":") {
			continue
		}
		filtered.Symbols = append(filtered.Symbols, symbol)
		if strings.Contains(symbol.SelectionReason, "fallback required") {
			filtered.Fallback = true
		}
		for _, key := range symbol.Tests {
			tests[key] = true
		}
	}
	for key := range tests {
		filtered.TestKeys = append(filtered.TestKeys, key)
	}
	sort.Strings(filtered.TestKeys)
	if filtered.Fallback && filtered.Reason == "" {
		filtered.Reason = "at least one changed " + language + " symbol has no safe historical test mapping"
	}
	return filtered, nil
}

func sameInventory(current []model.Symbol, stored map[string]store.PreviousSymbol) bool {
	if len(current) != len(stored) {
		return false
	}
	for _, symbol := range current {
		prior, ok := stored[symbol.Key()]
		if !ok || prior.SemanticHash != symbol.SemanticHash {
			return false
		}
	}
	return true
}

func (a *App) TestLanguage(ctx context.Context, language string, selectors []string) (model.TestRunResult, error) {
	lang, err := a.language(language)
	if err != nil {
		return model.TestRunResult{}, err
	}
	switch lang.Test.Runner {
	case "pytest":
		return a.Test(ctx, selectors)
	case "go-test-json", "vitest-json", "jest-json":
		return a.testStructured(ctx, lang, selectors)
	default:
		return model.TestRunResult{}, fmt.Errorf("unsupported test runner %q", lang.Test.Runner)
	}
}

func replaceCommand(command []string, values map[string]string) []string {
	result := make([]string, len(command))
	for i, arg := range command {
		for key, value := range values {
			arg = strings.ReplaceAll(arg, "{"+key+"}", value)
		}
		result[i] = arg
	}
	return result
}

func (a *App) testStructured(ctx context.Context, lang config.Language, selectors []string) (model.TestRunResult, error) {
	check, symbols, err := a.Scan(ctx)
	if err != nil {
		return model.TestRunResult{}, err
	}
	if len(lang.Test.Command) == 0 {
		return model.TestRunResult{}, errors.New("configured test command is empty")
	}
	runID := newID("test")
	artifactDir := filepath.Join(config.Resolve(a.Root, a.Config.ArtifactDirectory), runID)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return model.TestRunResult{}, err
	}
	resultsPath := filepath.Join(artifactDir, "results.json")
	coveragePath := filepath.Join(artifactDir, "coverage.out")
	configuredCoveragePath := ""
	if lang.Coverage.Format == "istanbul-json" {
		coveragePath = filepath.Join(artifactDir, "coverage.json")
	}
	if lang.Coverage.File != "" {
		configuredCoveragePath = filepath.Join(a.Root, lang.WorkingDirectory, lang.Coverage.File)
	}
	command := replaceCommand(lang.Test.Command, map[string]string{"artifact_dir": artifactDir, "results_file": resultsPath, "coverage_file": coveragePath})
	command = append(command, selectors...)
	result := model.TestRunResult{RunID: runID, Status: "running", StartedAt: time.Now().UTC(), ExitCode: -1, ArtifactDirectory: artifactDir}
	if err := a.Store.BeginTestRun(ctx, result, check.InventoryRunID, command); err != nil {
		return result, err
	}
	stdoutPath, stderrPath := filepath.Join(artifactDir, "stdout.log"), filepath.Join(artifactDir, "stderr.log")
	stdoutFile, err := os.Create(stdoutPath)
	if err != nil {
		return result, err
	}
	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		stdoutFile.Close()
		return result, err
	}
	runCtx, cancel := context.WithTimeout(ctx, a.Config.Execution.Timeout())
	defer cancel()
	cmd := exec.CommandContext(runCtx, command[0], command[1:]...)
	cmd.Dir = filepath.Join(a.Root, lang.WorkingDirectory)
	cmd.Stdout, cmd.Stderr = stdoutFile, stderrFile
	cmd.Env = environment(a.Config.Execution.EnvironmentAllowlist, map[string]string{"TESTLEDGER_RESULTS_FILE": resultsPath, "TESTLEDGER_COVERAGE_FILE": coveragePath})
	runErr := cmd.Run()
	_ = stdoutFile.Close()
	_ = stderrFile.Close()
	result.ExitCode, result.FinishedAt = commandExitCode(runErr), time.Now().UTC()
	if runCtx.Err() == context.DeadlineExceeded {
		result.Status = "timeout"
		result.InfrastructureErr = "test command exceeded configured timeout"
	}
	var parseErr error
	switch lang.Test.Runner {
	case "go-test-json":
		result.Cases, parseErr = parseGoTestJSON(stdoutPath, a.Config.Execution.MaxOutputBytes)
	default:
		input := resultsPath
		if !fileExists(input) {
			input = stdoutPath
		}
		result.Cases, parseErr = parseJSTestJSON(input, a.Config.Execution.MaxOutputBytes)
	}
	if parseErr != nil {
		result.Status = "infrastructure_error"
		result.InfrastructureErr = "structured test results unavailable: " + parseErr.Error()
		stderr, _ := os.ReadFile(stderrPath)
		result.Cases = []model.TestCaseResult{{TestKey: "<infrastructure>", Outcome: "error", Phase: "startup", FailureCategory: "infrastructure", Message: result.InfrastructureErr, Stderr: truncate(string(stderr), a.Config.Execution.MaxOutputBytes)}}
	}
	categorizeCases(result.Cases)
	countCases(&result)
	if result.Status == "running" {
		if result.ExitCode == 0 && result.Failed == 0 && result.Errors == 0 {
			result.Status = "passed"
		} else {
			result.Status = "failed"
		}
	}
	if err := a.Store.FinishTestRun(ctx, result); err != nil {
		return result, err
	}
	if !artifactFresh(coveragePath, result.StartedAt) && configuredCoveragePath != "" && artifactFresh(configuredCoveragePath, result.StartedAt) {
		if contents, err := os.ReadFile(configuredCoveragePath); err == nil {
			_ = os.WriteFile(coveragePath, contents, 0o600)
		}
	}
	if lang.Test.Runner == "go-test-json" && artifactFresh(coveragePath, result.StartedAt) {
		if details, err := goCoverageDetails(coveragePath, symbols); err == nil {
			_ = a.Store.SaveCoverageDetails(ctx, runID, details)
		}
	} else if artifactFresh(coveragePath, result.StartedAt) {
		if details, err := istanbulCoverageDetails(coveragePath, a.Root, symbols); err == nil {
			_ = a.Store.SaveCoverageDetails(ctx, runID, details)
		}
	}
	for _, artifact := range []struct{ kind, path string }{{"stdout", stdoutPath}, {"stderr", stderrPath}, {"results_json", resultsPath}, {"coverage", coveragePath}} {
		if fileExists(artifact.path) {
			if hash, n, err := hashFile(artifact.path); err == nil {
				_ = a.Store.AddArtifact(ctx, runID, artifact.kind, artifact.path, hash, n)
			}
		}
	}
	return result, nil
}

func artifactFresh(path string, started time.Time) bool {
	info, err := os.Stat(path)
	return err == nil && !info.ModTime().Before(started.Add(-time.Second))
}

type goEvent struct {
	Action, Package, Test, Output string
	Elapsed                       float64
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

func goCoverageDetails(path string, symbols []model.Symbol) ([]store.CoverageDetail, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	missing := map[string]map[int]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "mode:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		location := fields[0]
		colon := strings.LastIndex(location, ":")
		if colon < 0 {
			continue
		}
		file := filepath.ToSlash(location[:colon])
		parts := strings.FieldsFunc(location[colon+1:], func(r rune) bool { return r == '.' || r == ',' })
		if len(parts) != 4 {
			continue
		}
		count, _ := strconv.Atoi(fields[2])
		if count != 0 {
			continue
		}
		start, _ := strconv.Atoi(parts[0])
		end, _ := strconv.Atoi(parts[2])
		if missing[file] == nil {
			missing[file] = map[int]bool{}
		}
		for n := start; n <= end; n++ {
			missing[file][n] = true
		}
	}
	details := []store.CoverageDetail{}
	for _, symbol := range symbols {
		if symbol.Language != "go" {
			continue
		}
		set := missing[symbol.Path]
		if set == nil {
			for path, candidate := range missing {
				if strings.HasSuffix(path, "/"+symbol.Path) {
					set = candidate
					break
				}
			}
		}
		d := store.CoverageDetail{SymbolKey: symbol.Key(), SemanticHash: symbol.SemanticHash, MissingLines: []int{}, MissingBranches: []model.Branch{}}
		for _, line := range symbol.ExecutableLines {
			if set[line] {
				d.MissingLines = append(d.MissingLines, line)
			}
		}
		details = append(details, d)
	}
	return details, scanner.Err()
}

func istanbulCoverageDetails(path, root string, symbols []model.Symbol) ([]store.CoverageDetail, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var files map[string]struct {
		StatementMap map[string]struct{ Start struct{ Line int } } `json:"statementMap"`
		Statements   map[string]int                                `json:"s"`
		BranchMap    map[string]struct {
			Locations []struct{ Start struct{ Line int } }
		} `json:"branchMap"`
		Branches map[string][]int `json:"b"`
	}
	if err := json.Unmarshal(contents, &files); err != nil {
		return nil, err
	}
	byPath := map[string]struct {
		lines    map[int]bool
		branches []model.Branch
	}{}
	for raw, file := range files {
		rel := raw
		if filepath.IsAbs(rel) {
			rel, _ = filepath.Rel(root, rel)
		}
		rel = filepath.ToSlash(filepath.Clean(rel))
		item := struct {
			lines    map[int]bool
			branches []model.Branch
		}{lines: map[int]bool{}}
		for id, count := range file.Statements {
			if count == 0 {
				item.lines[file.StatementMap[id].Start.Line] = true
			}
		}
		for id, counts := range file.Branches {
			for i, count := range counts {
				if count == 0 && i < len(file.BranchMap[id].Locations) {
					line := file.BranchMap[id].Locations[i].Start.Line
					item.branches = append(item.branches, model.Branch{From: line, To: line, Description: fmt.Sprintf("branch alternative at line %d was not executed", line)})
				}
			}
		}
		byPath[rel] = item
	}
	details := []store.CoverageDetail{}
	for _, symbol := range symbols {
		if symbol.Language != "typescript" {
			continue
		}
		item := byPath[symbol.Path]
		d := store.CoverageDetail{SymbolKey: symbol.Key(), SemanticHash: symbol.SemanticHash, MissingLines: []int{}, MissingBranches: []model.Branch{}}
		for _, line := range symbol.ExecutableLines {
			if item.lines[line] {
				d.MissingLines = append(d.MissingLines, line)
			}
		}
		for _, branch := range item.branches {
			if branch.From >= symbol.StartLine && branch.From <= symbol.EndLine {
				d.MissingBranches = append(d.MissingBranches, branch)
			}
		}
		details = append(details, d)
	}
	return details, nil
}

func (a *App) RunMutation(ctx context.Context, language string) (model.MutationRun, error) {
	lang, err := a.language(language)
	if err != nil {
		return model.MutationRun{}, err
	}
	if len(lang.Mutation.Command) == 0 {
		return model.MutationRun{}, fmt.Errorf("no mutation command configured for %s", lang.Name)
	}
	if lang.Mutation.Format != "testledger-json" {
		return model.MutationRun{}, fmt.Errorf("unsupported mutation format %q", lang.Mutation.Format)
	}
	run := model.MutationRun{RunID: newID("mutation"), Language: lang.Name, Status: "running", StartedAt: time.Now().UTC(), ExitCode: -1}
	run.ArtifactDirectory = filepath.Join(config.Resolve(a.Root, a.Config.ArtifactDirectory), run.RunID)
	if err := os.MkdirAll(run.ArtifactDirectory, 0o755); err != nil {
		return run, err
	}
	resultPath := filepath.Join(run.ArtifactDirectory, "mutation.json")
	command := replaceCommand(lang.Mutation.Command, map[string]string{"artifact_dir": run.ArtifactDirectory, "results_file": resultPath})
	runCtx, cancel := context.WithTimeout(ctx, a.Config.Execution.Timeout())
	defer cancel()
	cmd := exec.CommandContext(runCtx, command[0], command[1:]...)
	cmd.Dir = filepath.Join(a.Root, lang.WorkingDirectory)
	output, runErr := cmd.CombinedOutput()
	run.ExitCode = commandExitCode(runErr)
	run.FinishedAt = time.Now().UTC()
	if !fileExists(resultPath) {
		_ = os.WriteFile(resultPath, output, 0o600)
	}
	contents, parseErr := os.ReadFile(resultPath)
	if parseErr == nil {
		var payload struct {
			Mutants []model.MutationResult `json:"mutants"`
		}
		parseErr = json.Unmarshal(contents, &payload)
		run.Results = payload.Mutants
		if parseErr == nil {
			for _, result := range run.Results {
				if strings.TrimSpace(result.SymbolKey) == "" || strings.TrimSpace(result.Operator) == "" {
					parseErr = errors.New("mutation entries require symbol_key and operator")
					break
				}
				if !map[string]bool{"killed": true, "survived": true, "timeout": true, "error": true, "skipped": true}[result.Status] {
					parseErr = fmt.Errorf("invalid mutation status %q", result.Status)
					break
				}
			}
		}
	}
	if runCtx.Err() == context.DeadlineExceeded {
		run.Status = "timeout"
		run.InfrastructureErr = "mutation command exceeded configured timeout"
	} else if parseErr != nil {
		run.Status = "infrastructure_error"
		run.InfrastructureErr = "mutation results unavailable: " + parseErr.Error()
	} else if runErr != nil {
		run.Status = "failed"
	} else {
		run.Status = "passed"
	}
	if err := a.Store.SaveMutationRun(ctx, run, command); err != nil {
		return run, err
	}
	return run, nil
}
