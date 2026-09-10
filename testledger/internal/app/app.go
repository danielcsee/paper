package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/config"
	"github.com/danielcsee/sciterm/testledger/internal/goadapter"
	"github.com/danielcsee/sciterm/testledger/internal/model"
	"github.com/danielcsee/sciterm/testledger/internal/pythonadapter"
	"github.com/danielcsee/sciterm/testledger/internal/store"
	"github.com/danielcsee/sciterm/testledger/internal/typescriptadapter"
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

func (a *App) Scan(ctx context.Context) (model.CheckResult, []model.Symbol, error) {
	started := time.Now().UTC()
	previousID, _, err := a.Store.LatestInventory(ctx)
	if err != nil {
		return model.CheckResult{}, nil, err
	}
	previous := map[string]store.PreviousSymbol{}
	if previousID != "" {
		previous, err = a.Store.SymbolsForInventory(ctx, previousID)
		if err != nil {
			return model.CheckResult{}, nil, err
		}
	}

	symbols, err := a.discover(ctx)
	if err != nil {
		return model.CheckResult{}, nil, err
	}
	sort.Slice(symbols, func(i, j int) bool { return symbols[i].Key() < symbols[j].Key() })
	runID := newID("inv")
	if err := a.Store.SaveInventory(ctx, runID, a.Root, started, symbols); err != nil {
		return model.CheckResult{}, nil, err
	}

	result := model.CheckResult{
		InventoryRunID: runID,
		Scanned:        len(symbols),
		Deleted:        []string{},
		ChangedGaps:    []model.Gap{},
		AllGaps:        []model.Gap{},
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
	seen := make(map[string]bool, len(symbols))
	for _, symbol := range symbols {
		seen[symbol.Key()] = true
		old, existed := previous[symbol.Key()]
		changed := !existed || old.SemanticHash != symbol.SemanticHash
		if changed {
			result.Changed++
		}
		skipped, err := a.Store.ActiveDisposition(ctx, symbol.Key(), symbol.SemanticHash, time.Now())
		if err != nil {
			return model.CheckResult{}, nil, err
		}
		if skipped {
			result.Skipped++
			continue
		}
		if coverable(symbol) {
			result.Skipped++
			continue
		}
		minimum := a.minimumCoverage(symbol.Language)
		coverage, err := a.Store.CoverageForSymbol(ctx, symbol.Key(), symbol.SemanticHash, minimum)
		if err != nil {
			return model.CheckResult{}, nil, err
		}
		if coverage.Count == 0 {
			reason := "no passing test meets the function coverage policy"
			if coverage.Best > 0 {
				reason = fmt.Sprintf("best observed coverage %.1f%% is below required %.1f%%", coverage.Best, minimum)
			}
			gap := model.Gap{
				SymbolKey: symbol.Key(), Path: symbol.Path, QualifiedName: symbol.QualifiedName,
				Kind: symbol.Kind, StartLine: symbol.StartLine, EndLine: symbol.EndLine,
				SemanticHash: symbol.SemanticHash, Changed: changed, Reason: reason,
				BestCoverage: coverage.Best, CoveringTestCount: coverage.Count,
			}
			if detail, detailErr := a.Store.CoverageDetailForSymbol(ctx, symbol.Key(), symbol.SemanticHash); detailErr == nil {
				gap.MissingLines, gap.MissingBranches = detail.MissingLines, detail.MissingBranches
			}
			result.AllGaps = append(result.AllGaps, gap)
			if changed {
				result.ChangedGaps = append(result.ChangedGaps, gap)
			}
		}
	}
	for key := range previous {
		if !seen[key] {
			result.Deleted = append(result.Deleted, key)
		}
	}
	sort.Strings(result.Deleted)
	return result, symbols, nil
}

func (a *App) discover(ctx context.Context) ([]model.Symbol, error) {
	var symbols []model.Symbol
	for _, language := range a.Config.Languages {
		switch language.Name {
		case "python":
			found, err := (pythonadapter.Adapter{}).Discover(ctx, a.Root, language)
			if err != nil {
				return nil, err
			}
			symbols = append(symbols, found...)
		case "go":
			found, err := (goadapter.Adapter{}).Discover(ctx, a.Root, language)
			if err != nil {
				return nil, err
			}
			symbols = append(symbols, found...)
		case "typescript":
			found, err := (typescriptadapter.Adapter{}).Discover(ctx, a.Root, language)
			if err != nil {
				return nil, err
			}
			symbols = append(symbols, found...)
		default:
			return nil, fmt.Errorf("unsupported language adapter %q", language.Name)
		}
	}
	sort.Slice(symbols, func(i, j int) bool { return symbols[i].Key() < symbols[j].Key() })
	return symbols, nil
}

func (a *App) Check(ctx context.Context) (model.CheckResult, error) {
	result, _, err := a.Scan(ctx)
	return result, err
}

func (a *App) minimumCoverage(language string) float64 {
	for _, candidate := range a.Config.Languages {
		if candidate.Name == language {
			return candidate.Coverage.MinimumLinePercent
		}
	}
	return 80
}

func (a *App) AddSkip(ctx context.Context, symbolKey, reason string, durable bool, expiresAt *time.Time) error {
	return a.AddSkipBy(ctx, symbolKey, reason, durable, expiresAt, "human")
}

func (a *App) AddSkipBy(ctx context.Context, symbolKey, reason string, durable bool, expiresAt *time.Time, approvedBy string) error {
	if strings.TrimSpace(reason) == "" {
		return errors.New("skip reason is required")
	}
	if strings.TrimSpace(approvedBy) == "" {
		return errors.New("approved_by is required")
	}
	latestID, _, err := a.Store.LatestInventory(ctx)
	if err != nil {
		return err
	}
	if latestID == "" {
		return errors.New("run check before recording a skip")
	}
	symbols, err := a.Store.SymbolsForInventory(ctx, latestID)
	if err != nil {
		return err
	}
	symbol, ok := symbols[symbolKey]
	if !ok {
		return fmt.Errorf("symbol not found in latest inventory: %s", symbolKey)
	}
	semanticHash := symbol.SemanticHash
	if durable {
		semanticHash = ""
	}
	return a.Store.AddSkip(ctx, symbolKey, semanticHash, reason, approvedBy, expiresAt)
}

func (a *App) Status(ctx context.Context) (model.Status, error) {
	latestID, completedAt, err := a.Store.LatestInventory(ctx)
	if err != nil {
		return model.Status{}, err
	}
	status := model.Status{LatestInventoryID: latestID, LatestInventoryAt: completedAt}
	if latestID != "" {
		symbols, err := a.Store.SymbolsForInventory(ctx, latestID)
		if err != nil {
			return status, err
		}
		status.SymbolCount = len(symbols)
		// A status scan is intentionally read-only. Count unresolved current versions.
		for key, symbol := range symbols {
			skipped, err := a.Store.ActiveDisposition(ctx, key, symbol.SemanticHash, time.Now())
			if err != nil {
				return status, err
			}
			if skipped {
				continue
			}
			coverage, err := a.Store.CoverageForSymbol(ctx, key, symbol.SemanticHash, a.minimumCoverage(strings.SplitN(key, ":", 2)[0]))
			if err != nil {
				return status, err
			}
			if coverage.Count == 0 {
				status.OutstandingGaps++
			}
		}
	}
	status.ActiveSkips, err = a.Store.CountActiveSkips(ctx)
	if err != nil {
		return status, err
	}
	status.LatestTestRun, err = a.Store.LatestTestRun(ctx)
	return status, err
}

func newID(prefix string) string {
	bytes := make([]byte, 8)
	_, _ = rand.Read(bytes)
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UTC().UnixMilli(), hex.EncodeToString(bytes))
}

func hashFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	count, err := io.Copy(hash, file)
	return hex.EncodeToString(hash.Sum(nil)), count, err
}

func environment(allowlist []string, additions map[string]string) []string {
	allowed := make(map[string]bool, len(allowlist))
	for _, key := range allowlist {
		allowed[key] = true
	}
	values := map[string]string{}
	for _, pair := range os.Environ() {
		key, value, ok := strings.Cut(pair, "=")
		if ok && allowed[key] {
			values[key] = value
		}
	}
	for key, value := range additions {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func commandExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func truncate(value string, limit int64) string {
	if limit <= 0 || int64(len(value)) <= limit {
		return value
	}
	return value[:limit] + "\n<testledger: output truncated>"
}

func writeJSON(path string, value any) error {
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(contents, '\n'), 0o600)
}

// coverable reports whether a symbol has no executable body -- an overload
// stub, or a body that is only a docstring. No run can ever produce coverage
// for one, so reporting it as a gap would create a backlog entry nobody can
// ever clear.
func coverable(symbol model.Symbol) bool {
	return len(symbol.ExecutableLines) == 0
}
