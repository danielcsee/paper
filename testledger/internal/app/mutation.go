// Optional mutation signals. Kept separate from the deterministic
// pass/fail record: a surviving mutant suggests weak assertions, it does
// not prove a test is missing.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/config"
	"github.com/danielcsee/sciterm/testledger/internal/model"
)

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
