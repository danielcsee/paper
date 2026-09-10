// The proposal lifecycle: an agent proposes tests, a human decides, the
// agent records what it wrote. Targets are pinned to a semantic hash so a
// source change makes an unfulfilled proposal obsolete.
package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/model"
)

func (a *App) CreateProposal(ctx context.Context, targets []string, cases []model.ProposalCase, rationale, createdBy string) (model.TestProposal, error) {
	if strings.TrimSpace(rationale) == "" {
		return model.TestProposal{}, errors.New("proposal rationale is required")
	}
	if strings.TrimSpace(createdBy) == "" {
		return model.TestProposal{}, errors.New("created_by is required")
	}
	if len(targets) == 0 || len(cases) == 0 {
		return model.TestProposal{}, errors.New("proposal requires at least one target and one test case")
	}
	for _, testCase := range cases {
		if strings.TrimSpace(testCase.Name) == "" || strings.TrimSpace(testCase.Description) == "" || len(testCase.Assertions) == 0 {
			return model.TestProposal{}, errors.New("each proposed case requires name, description, and assertions")
		}
		if !a.validTestSelector(testCase.TestFile) {
			return model.TestProposal{}, fmt.Errorf("proposed test file is outside configured test roots: %s", testCase.TestFile)
		}
	}
	_, current, err := a.Scan(ctx)
	if err != nil {
		return model.TestProposal{}, err
	}
	byKey := map[string]model.Symbol{}
	for _, symbol := range current {
		byKey[symbol.Key()] = symbol
	}
	proposal := model.TestProposal{ID: newID("proposal"), Status: "proposed", Rationale: rationale, Cases: cases, Targets: []model.ProposalTarget{}, CreatedBy: createdBy}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	proposal.CreatedAt, proposal.UpdatedAt = now, now
	seen := map[string]bool{}
	for _, key := range targets {
		if seen[key] {
			continue
		}
		symbol, ok := byKey[key]
		if !ok {
			return model.TestProposal{}, fmt.Errorf("unknown current target %s", key)
		}
		seen[key] = true
		skipped, err := a.Store.ActiveDisposition(ctx, key, symbol.SemanticHash, time.Now())
		if err != nil {
			return model.TestProposal{}, err
		}
		if skipped {
			return model.TestProposal{}, fmt.Errorf("target %s has an active disposition", key)
		}
		coverage, err := a.Store.CoverageForSymbol(ctx, key, symbol.SemanticHash, a.minimumCoverage(symbol.Language))
		if err != nil {
			return model.TestProposal{}, err
		}
		if coverage.Count > 0 {
			return model.TestProposal{}, fmt.Errorf("target %s already meets coverage policy", key)
		}
		proposal.Targets = append(proposal.Targets, model.ProposalTarget{SymbolKey: key, SemanticHash: symbol.SemanticHash})
	}
	sort.Slice(proposal.Targets, func(i, j int) bool { return proposal.Targets[i].SymbolKey < proposal.Targets[j].SymbolKey })
	if err := a.Store.CreateProposal(ctx, proposal); err != nil {
		return proposal, err
	}
	return proposal, nil
}

func (a *App) DecideProposal(ctx context.Context, id, decision, decidedBy, reason string) (model.TestProposal, error) {
	if strings.TrimSpace(decidedBy) == "" || strings.TrimSpace(reason) == "" {
		return model.TestProposal{}, errors.New("decided_by and reason are required")
	}
	if decision == "approved" {
		if err := a.ensureProposalCurrent(ctx, id); err != nil {
			return model.TestProposal{}, err
		}
	}
	if err := a.Store.DecideProposal(ctx, id, decision, decidedBy, reason); err != nil {
		return model.TestProposal{}, err
	}
	return a.Store.GetProposal(ctx, id)
}

func (a *App) MarkProposalImplemented(ctx context.Context, id string, links []model.IntendedTestLink) (model.TestProposal, error) {
	if len(links) == 0 {
		return model.TestProposal{}, errors.New("at least one intended test link is required")
	}
	for _, link := range links {
		if strings.TrimSpace(link.SymbolKey) == "" || strings.TrimSpace(link.TestKey) == "" {
			return model.TestProposal{}, errors.New("each link requires symbol_key and test_key")
		}
	}
	if err := a.ensureProposalCurrent(ctx, id); err != nil {
		return model.TestProposal{}, err
	}
	proposal, err := a.Store.GetProposal(ctx, id)
	if err != nil {
		return model.TestProposal{}, err
	}
	targeted := map[string]bool{}
	for _, target := range proposal.Targets {
		targeted[target.SymbolKey] = true
	}
	linked := map[string]bool{}
	for _, link := range links {
		if !targeted[link.SymbolKey] {
			return model.TestProposal{}, fmt.Errorf("symbol %s is not targeted by proposal", link.SymbolKey)
		}
		linked[link.SymbolKey] = true
	}
	for key := range targeted {
		if !linked[key] {
			return model.TestProposal{}, fmt.Errorf("proposal target %s has no intended test link", key)
		}
	}
	if err := a.Store.MarkProposalImplemented(ctx, id, links); err != nil {
		return model.TestProposal{}, err
	}
	return a.Store.GetProposal(ctx, id)
}

func (a *App) ensureProposalCurrent(ctx context.Context, id string) error {
	proposal, err := a.Store.GetProposal(ctx, id)
	if err != nil {
		return err
	}
	_, symbols, err := a.Scan(ctx)
	if err != nil {
		return err
	}
	current := map[string]string{}
	for _, symbol := range symbols {
		current[symbol.Key()] = symbol.SemanticHash
	}
	for _, target := range proposal.Targets {
		if current[target.SymbolKey] != target.SemanticHash {
			reason := "target function version changed after proposal creation"
			_ = a.Store.MarkProposalObsolete(ctx, id, reason)
			return fmt.Errorf("proposal %s is obsolete: %s", id, reason)
		}
	}
	return nil
}
