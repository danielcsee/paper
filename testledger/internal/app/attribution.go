// Turning a native coverage report into per-symbol evidence. Python
// contexts give exact test-to-symbol attribution; Go and Istanbul give
// aggregate line and branch detail only.
package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/danielcsee/sciterm/testledger/internal/model"
	"github.com/danielcsee/sciterm/testledger/internal/store"
)

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
