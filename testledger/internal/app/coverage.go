// Coverage policy: which symbols count as gaps, and the context an
// agent needs to decide what to do about one.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/model"
	"github.com/danielcsee/sciterm/testledger/internal/store"
)

func (a *App) ListCoverageGaps(ctx context.Context, changedOnly bool, limit, cursor int) (model.Page[model.Gap], error) {
	limit, cursor = normalizePage(limit, cursor)
	symbols, err := a.Store.CurrentSymbols(ctx)
	if err != nil {
		return model.Page[model.Gap]{}, err
	}
	ids, err := a.Store.RecentInventoryIDs(ctx, 2)
	if err != nil {
		return model.Page[model.Gap]{}, err
	}
	previous := map[string]store.PreviousSymbol{}
	if len(ids) > 1 {
		previous, err = a.Store.SymbolsForInventory(ctx, ids[1])
		if err != nil {
			return model.Page[model.Gap]{}, err
		}
	}
	gaps := []model.Gap{}
	for _, symbol := range symbols {
		old, existed := previous[symbol.Key()]
		changed := !existed || old.SemanticHash != symbol.SemanticHash
		if changedOnly && !changed {
			continue
		}
		skipped, err := a.Store.ActiveDisposition(ctx, symbol.Key(), symbol.SemanticHash, time.Now())
		if err != nil {
			return model.Page[model.Gap]{}, err
		}
		if skipped {
			continue
		}
		if coverable(symbol) {
			continue
		}
		minimum := a.minimumCoverage(symbol.Language)
		coverage, err := a.Store.CoverageForSymbol(ctx, symbol.Key(), symbol.SemanticHash, minimum)
		if err != nil {
			return model.Page[model.Gap]{}, err
		}
		if coverage.Count > 0 {
			continue
		}
		reason := "no passing test meets the function coverage policy"
		if coverage.Best > 0 {
			reason = fmt.Sprintf("best observed coverage %.1f%% is below required %.1f%%", coverage.Best, minimum)
		}
		gap := model.Gap{SymbolKey: symbol.Key(), Path: symbol.Path, QualifiedName: symbol.QualifiedName, Kind: symbol.Kind,
			StartLine: symbol.StartLine, EndLine: symbol.EndLine, SemanticHash: symbol.SemanticHash, Changed: changed, Reason: reason,
			BestCoverage: coverage.Best, CoveringTestCount: coverage.Count}
		if detail, detailErr := a.Store.CoverageDetailForSymbol(ctx, symbol.Key(), symbol.SemanticHash); detailErr == nil {
			gap.MissingLines, gap.MissingBranches = detail.MissingLines, detail.MissingBranches
		}
		gaps = append(gaps, gap)
	}
	page := model.Page[model.Gap]{Items: []model.Gap{}, Total: len(gaps)}
	if cursor >= len(gaps) {
		return page, nil
	}
	end := cursor + limit
	if end > len(gaps) {
		end = len(gaps)
	}
	page.Items = append(page.Items, gaps[cursor:end]...)
	if end < len(gaps) {
		page.NextCursor = end
	}
	return page, nil
}

func (a *App) GetSymbolContext(ctx context.Context, symbolKey string, includeSource bool) (model.SymbolContext, error) {
	symbols, err := a.Store.CurrentSymbols(ctx)
	if err != nil {
		return model.SymbolContext{}, err
	}
	var found *model.Symbol
	for i := range symbols {
		if symbols[i].Key() == symbolKey {
			found = &symbols[i]
			break
		}
	}
	if found == nil {
		return model.SymbolContext{}, fmt.Errorf("unknown current symbol %s", symbolKey)
	}
	result := model.SymbolContext{Symbol: *found, CoveringTests: []string{}}
	result.CoveringTests, err = a.Store.CoveringTests(ctx, found.Key(), found.SemanticHash)
	if err != nil {
		return result, err
	}
	if includeSource {
		path := filepath.Join(a.Root, filepath.FromSlash(found.Path))
		rel, err := filepath.Rel(a.Root, path)
		if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			return result, errors.New("symbol path escapes project root")
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return result, err
		}
		lines := strings.Split(string(contents), "\n")
		start, end := found.StartLine-1, found.EndLine
		if start < 0 {
			start = 0
		}
		if end > len(lines) {
			end = len(lines)
		}
		if start < end {
			result.Source = strings.Join(lines[start:end], "\n")
		}
	}
	return result, nil
}

// coverable reports whether a symbol has no executable body -- an overload
// stub, or a body that is only a docstring. No run can ever produce coverage
// for one, so reporting it as a gap would create a backlog entry nobody can
// ever clear.
func coverable(symbol model.Symbol) bool {
	return len(symbol.ExecutableLines) == 0
}
