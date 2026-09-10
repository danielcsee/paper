// Choosing which tests a change requires. Falls back to the full suite
// whenever a changed symbol has no safe historical mapping.
package app

import (
	"context"
	"sort"
	"strings"

	"github.com/danielcsee/sciterm/testledger/internal/model"
)

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
