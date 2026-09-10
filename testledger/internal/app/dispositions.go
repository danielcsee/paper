// Recorded human decisions to leave a symbol untested. A disposition is
// evidence of a choice, never of coverage.
package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

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
