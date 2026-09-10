// Turning source into a deterministic symbol inventory, and comparing
// one inventory against the last.
package app

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/model"
	"github.com/danielcsee/sciterm/testledger/internal/store"
)

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
		adapter, err := discovererFor(language.Name)
		if err != nil {
			return nil, err
		}
		found, err := adapter.Discover(ctx, a.Root, language)
		if err != nil {
			return nil, err
		}
		symbols = append(symbols, found...)
	}
	sort.Slice(symbols, func(i, j int) bool { return symbols[i].Key() < symbols[j].Key() })
	return symbols, nil
}

func (a *App) Check(ctx context.Context) (model.CheckResult, error) {
	result, _, err := a.Scan(ctx)
	return result, err
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
