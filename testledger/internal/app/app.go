// The application service: the single API both the CLI and the MCP
// server call. Neither one shells out to the other.
package app

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/danielcsee/sciterm/testledger/internal/config"
	"github.com/danielcsee/sciterm/testledger/internal/store"
)

type App struct {
	Root     string
	Config   config.Config
	Store    *store.Store
	jobSlots chan struct{}
	jobs     sync.WaitGroup
}

func Open(root, configPath string) (*App, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(absRoot, configPath)
	if err != nil {
		return nil, err
	}
	if err := config.EnsureDirectories(absRoot, cfg); err != nil {
		return nil, err
	}
	db, err := store.Open(config.Resolve(absRoot, cfg.Database))
	if err != nil {
		return nil, err
	}
	return &App{Root: absRoot, Config: cfg, Store: db, jobSlots: make(chan struct{}, cfg.Execution.MaxParallelRuns)}, nil
}

func (a *App) Close() error { a.jobs.Wait(); return a.Store.Close() }

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

func (a *App) pythonLanguage() (config.Language, error) {
	for _, language := range a.Config.Languages {
		if language.Name == "python" {
			return language, nil
		}
	}
	return config.Language{}, fmt.Errorf("no Python language configured")
}

func (a *App) minimumCoverage(language string) float64 {
	for _, candidate := range a.Config.Languages {
		if candidate.Name == language {
			return candidate.Coverage.MinimumLinePercent
		}
	}
	return 80
}
