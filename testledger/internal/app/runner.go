// Executing a language's native test runner and recording the result.
// Commands are fixed argument arrays with artifact placeholders, never
// shell-evaluated.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/config"
	"github.com/danielcsee/sciterm/testledger/internal/model"
	"github.com/danielcsee/sciterm/testledger/internal/pythonadapter"
)

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

func artifactFresh(path string, started time.Time) bool {
	info, err := os.Stat(path)
	return err == nil && !info.ModTime().Before(started.Add(-time.Second))
}
