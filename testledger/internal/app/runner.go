package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/config"
	"github.com/danielcsee/sciterm/testledger/internal/model"
	"github.com/danielcsee/sciterm/testledger/internal/pythonadapter"
	"github.com/danielcsee/sciterm/testledger/internal/store"
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

func (a *App) Test(ctx context.Context, extraArgs []string) (model.TestRunResult, error) {
	check, symbols, err := a.Scan(ctx)
	if err != nil {
		return model.TestRunResult{}, err
	}
	python, err := a.pythonLanguage()
	if err != nil {
		return model.TestRunResult{}, err
	}
	runID := newID("test")
	artifactDir := filepath.Join(config.Resolve(a.Root, a.Config.ArtifactDirectory), runID)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return model.TestRunResult{}, err
	}
	result := model.TestRunResult{
		RunID: runID, Status: "running", StartedAt: time.Now().UTC(), ExitCode: -1,
		ArtifactDirectory: artifactDir,
	}

	pluginDir, cleanup, err := pythonadapter.ExtractTestPlugin()
	if err != nil {
		return result, err
	}
	defer cleanup()
	pytestJSON := filepath.Join(artifactDir, "pytest.json")
	coverageData := filepath.Join(artifactDir, ".coverage")
	coverageJSON := filepath.Join(artifactDir, "coverage.json")
	stdoutPath := filepath.Join(artifactDir, "stdout.log")
	stderrPath := filepath.Join(artifactDir, "stderr.log")

	command, err := coverageCommand(python, pluginDir, pytestJSON, coverageData, extraArgs)
	if err != nil {
		return result, err
	}
	if err := a.Store.BeginTestRun(ctx, result, check.InventoryRunID, command); err != nil {
		return result, err
	}

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
	cmd.Dir = a.Root
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	existingPythonPath := os.Getenv("PYTHONPATH")
	pythonPath := pluginDir
	if existingPythonPath != "" {
		pythonPath += string(os.PathListSeparator) + existingPythonPath
	}
	cmd.Env = environment(a.Config.Execution.EnvironmentAllowlist, map[string]string{
		"PYTHONPATH":             pythonPath,
		"TESTLEDGER_PYTEST_JSON": pytestJSON,
		"COVERAGE_FILE":          coverageData,
	})
	runErr := cmd.Run()
	_ = stdoutFile.Close()
	_ = stderrFile.Close()
	result.ExitCode = commandExitCode(runErr)
	result.FinishedAt = time.Now().UTC()
	if runCtx.Err() == context.DeadlineExceeded {
		result.Status = "timeout"
		result.InfrastructureErr = "test command exceeded configured timeout"
	} else if runErr != nil && result.ExitCode == -1 {
		result.Status = "infrastructure_error"
		result.InfrastructureErr = runErr.Error()
	}

	if payload, parseErr := readPytestResults(pytestJSON, a.Config.Execution.MaxOutputBytes); parseErr == nil {
		result.Cases = payload.Tests
		categorizeCases(result.Cases)
		countCases(&result)
		if result.Status == "running" {
			if result.Failed > 0 || result.Errors > 0 || result.ExitCode != 0 {
				result.Status = "failed"
			} else {
				result.Status = "passed"
			}
		}
	} else {
		if result.Status == "running" {
			result.Status = "infrastructure_error"
		}
		if result.InfrastructureErr == "" {
			result.InfrastructureErr = fmt.Sprintf("pytest did not produce structured results: %v", parseErr)
		}
		stderr, _ := os.ReadFile(stderrPath)
		result.Cases = []model.TestCaseResult{{
			TestKey: "<infrastructure>", Outcome: "error", Phase: "startup",
			FailureCategory: "infrastructure", Message: result.InfrastructureErr,
			Stderr: truncate(string(stderr), a.Config.Execution.MaxOutputBytes),
		}}
		result.Errors = 1
	}
	if err := a.Store.FinishTestRun(ctx, result); err != nil {
		return result, err
	}

	if fileExists(coverageData) {
		exportErr := exportCoverage(ctx, a.Root, python, coverageData, coverageJSON, cmd.Env)
		if exportErr == nil {
			observations, err := attributeCoverage(coverageJSON, a.Root, symbols)
			if err == nil {
				if err = a.Store.SaveCoverage(ctx, runID, observations); err == nil {
					result.CoverageMappings = len(observations)
					if details, detailErr := coverageDetails(coverageJSON, a.Root, symbols); detailErr == nil {
						_ = a.Store.SaveCoverageDetails(ctx, runID, details)
					}
					_ = a.Store.VerifyIntentions(ctx, runID, python.Coverage.MinimumLinePercent)
				}
			}
		}
	}
	for _, artifact := range []struct{ kind, path string }{
		{"stdout", stdoutPath}, {"stderr", stderrPath}, {"pytest_json", pytestJSON}, {"coverage_json", coverageJSON},
	} {
		if !fileExists(artifact.path) {
			continue
		}
		hash, bytes, err := hashFile(artifact.path)
		if err == nil {
			_ = a.Store.AddArtifact(ctx, runID, artifact.kind, artifact.path, hash, bytes)
		}
	}
	return result, nil
}

func coverageDetails(path, root string, symbols []model.Symbol) ([]store.CoverageDetail, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var payload coveragePayload
	if err := json.Unmarshal(contents, &payload); err != nil {
		return nil, err
	}
	byPath := map[string][]model.Symbol{}
	for _, symbol := range symbols {
		byPath[filepath.ToSlash(symbol.Path)] = append(byPath[filepath.ToSlash(symbol.Path)], symbol)
	}
	details := []store.CoverageDetail{}
	for rawPath, file := range payload.Files {
		relative := rawPath
		if filepath.IsAbs(relative) {
			if rel, relErr := filepath.Rel(root, relative); relErr == nil {
				relative = rel
			}
		}
		relative = filepath.ToSlash(filepath.Clean(relative))
		missingSet := map[int]bool{}
		for _, line := range file.MissingLines {
			missingSet[line] = true
		}
		for _, symbol := range byPath[relative] {
			detail := store.CoverageDetail{SymbolKey: symbol.Key(), SemanticHash: symbol.SemanticHash, MissingLines: []int{}, MissingBranches: []model.Branch{}}
			for _, line := range symbol.ExecutableLines {
				if missingSet[line] {
					detail.MissingLines = append(detail.MissingLines, line)
				}
			}
			for _, pair := range file.MissingBranches {
				if len(pair) == 2 && pair[0] >= symbol.StartLine && pair[0] <= symbol.EndLine {
					description := fmt.Sprintf("branch from line %d to line %d was not executed", pair[0], pair[1])
					if pair[1] < 0 {
						description = fmt.Sprintf("exit branch from line %d was not executed", pair[0])
					}
					detail.MissingBranches = append(detail.MissingBranches, model.Branch{From: pair[0], To: pair[1], Description: description})
				}
			}
			details = append(details, detail)
		}
	}
	return details, nil
}

func (a *App) pythonLanguage() (config.Language, error) {
	for _, language := range a.Config.Languages {
		if language.Name == "python" {
			return language, nil
		}
	}
	return config.Language{}, fmt.Errorf("no Python language configured")
}

func coverageCommand(lang config.Language, pluginDir, pytestJSON, coverageData string, extra []string) ([]string, error) {
	if len(lang.Test.Command) < 3 || lang.Test.Command[1] != "-m" || lang.Test.Command[2] != "pytest" {
		return nil, fmt.Errorf("Python test command must begin with [python, -m, pytest]")
	}
	python := lang.Test.Command[0]
	if python == "" {
		python = lang.Python
	}
	command := []string{python, "-m", "coverage", "run", "--data-file", coverageData}
	if lang.Coverage.Branch {
		command = append(command, "--branch")
	}
	for _, source := range lang.Coverage.Source {
		command = append(command, "--source", source)
	}
	command = append(command, "-m", "pytest", "-p", "testledger_pytest_plugin", "-p", "no:cacheprovider")
	command = append(command, lang.Test.Command[3:]...)
	command = append(command, lang.Test.TestRoots...)
	command = append(command, extra...)
	return command, nil
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

func exportCoverage(ctx context.Context, root string, lang config.Language, dataPath, jsonPath string, env []string) error {
	python := lang.Python
	if len(lang.Test.Command) > 0 {
		python = lang.Test.Command[0]
	}
	cmd := exec.CommandContext(ctx, python, "-m", "coverage", "json", "--data-file", dataPath, "--show-contexts", "-o", jsonPath)
	cmd.Dir = root
	cmd.Env = env
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("export coverage: %w: %s", err, output)
	}
	return nil
}

func attributeCoverage(path, root string, symbols []model.Symbol) ([]store.CoverageObservation, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var payload coveragePayload
	if err := json.Unmarshal(contents, &payload); err != nil {
		return nil, err
	}
	byPath := map[string][]model.Symbol{}
	for _, symbol := range symbols {
		byPath[filepath.ToSlash(symbol.Path)] = append(byPath[filepath.ToSlash(symbol.Path)], symbol)
	}
	type aggregate struct {
		lines      map[int]bool
		executable map[int]bool
		symbol     model.Symbol
		test       string
	}
	aggregates := map[string]*aggregate{}
	for rawPath, file := range payload.Files {
		relative := rawPath
		if filepath.IsAbs(relative) {
			if rel, err := filepath.Rel(root, relative); err == nil {
				relative = rel
			}
		}
		relative = filepath.ToSlash(filepath.Clean(relative))
		fileSymbols := byPath[relative]
		allExecutable := append(append([]int{}, file.ExecutedLines...), file.MissingLines...)
		for _, symbol := range fileSymbols {
			executable := map[int]bool{}
			coverageExecutable := map[int]bool{}
			for _, line := range allExecutable {
				coverageExecutable[line] = true
			}
			for _, line := range symbol.ExecutableLines {
				if coverageExecutable[line] {
					executable[line] = true
				}
			}
			for lineText, contexts := range file.Contexts {
				var line int
				if _, err := fmt.Sscanf(lineText, "%d", &line); err != nil || !executable[line] {
					continue
				}
				for _, testKey := range contexts {
					if testKey == "" {
						continue
					}
					key := testKey + "\x00" + symbol.Key()
					agg := aggregates[key]
					if agg == nil {
						agg = &aggregate{lines: map[int]bool{}, executable: executable, symbol: symbol, test: testKey}
						aggregates[key] = agg
					}
					agg.lines[line] = true
				}
			}
		}
	}
	observations := make([]store.CoverageObservation, 0, len(aggregates))
	for _, aggregate := range aggregates {
		lines := make([]int, 0, len(aggregate.lines))
		for line := range aggregate.lines {
			lines = append(lines, line)
		}
		sort.Ints(lines)
		percent := 0.0
		if len(aggregate.executable) > 0 {
			percent = float64(len(lines)) / float64(len(aggregate.executable)) * 100
		}
		observations = append(observations, store.CoverageObservation{
			TestKey: aggregate.test, SymbolKey: aggregate.symbol.Key(), SemanticHash: aggregate.symbol.SemanticHash,
			ExecutedLines: lines, ExecutedCount: len(lines), ExecutableCount: len(aggregate.executable), LinePercent: percent,
		})
	}
	sort.Slice(observations, func(i, j int) bool {
		if observations[i].SymbolKey == observations[j].SymbolKey {
			return observations[i].TestKey < observations[j].TestKey
		}
		return observations[i].SymbolKey < observations[j].SymbolKey
	})
	return observations, nil
}

func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }
