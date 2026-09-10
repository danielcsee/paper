// Symbol inventories: the immutable snapshot each scan writes, and the
// lookups that compare one snapshot against another.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/model"
	_ "modernc.org/sqlite"
)

type PreviousSymbol struct {
	Key          string
	SemanticHash string
}

func (s *Store) LatestInventory(ctx context.Context) (string, string, error) {
	var id, completed string
	err := s.db.QueryRowContext(ctx, `SELECT id, COALESCE(completed_at, '') FROM inventory_runs
WHERE status='completed' ORDER BY completed_at DESC LIMIT 1`).Scan(&id, &completed)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return id, completed, err
}

func (s *Store) SymbolsForInventory(ctx context.Context, runID string) (map[string]PreviousSymbol, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.symbol_key, sv.semantic_hash
FROM symbol_versions sv JOIN symbols s ON s.id=sv.symbol_id WHERE sv.inventory_run_id=?`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]PreviousSymbol{}
	for rows.Next() {
		var p PreviousSymbol
		if err := rows.Scan(&p.Key, &p.SemanticHash); err != nil {
			return nil, err
		}
		out[p.Key] = p
	}
	return out, rows.Err()
}

func (s *Store) SaveInventory(ctx context.Context, runID, root string, started time.Time, symbols []model.Symbol) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_runs(id, started_at, status, root) VALUES(?,?,?,?)`,
		runID, started.UTC().Format(time.RFC3339Nano), "running", root); err != nil {
		return err
	}
	for _, symbol := range symbols {
		if _, err := tx.ExecContext(ctx, `INSERT INTO symbols(symbol_key, language, qualified_name)
VALUES(?,?,?) ON CONFLICT(symbol_key) DO UPDATE SET language=excluded.language, qualified_name=excluded.qualified_name`,
			symbol.Key(), symbol.Language, symbol.QualifiedName); err != nil {
			return err
		}
		var symbolID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM symbols WHERE symbol_key=?`, symbol.Key()).Scan(&symbolID); err != nil {
			return err
		}
		lines, _ := json.Marshal(symbol.ExecutableLines)
		if _, err := tx.ExecContext(ctx, `INSERT INTO symbol_versions(
symbol_id, inventory_run_id, semantic_hash, signature_hash, body_hash, path, kind, start_line, end_line, executable_lines_json)
VALUES(?,?,?,?,?,?,?,?,?,?)`, symbolID, runID, symbol.SemanticHash, symbol.SignatureHash, symbol.BodyHash,
			symbol.Path, symbol.Kind, symbol.StartLine, symbol.EndLine, string(lines)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE inventory_runs SET status='completed', completed_at=? WHERE id=?`,
		time.Now().UTC().Format(time.RFC3339Nano), runID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CurrentSymbols(ctx context.Context) ([]model.Symbol, error) {
	runID, _, err := s.LatestInventory(ctx)
	if err != nil || runID == "" {
		return []model.Symbol{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT s.language, sv.path, s.qualified_name, sv.kind,
sv.start_line, sv.end_line, sv.executable_lines_json, sv.semantic_hash, sv.signature_hash, sv.body_hash
FROM symbol_versions sv JOIN symbols s ON s.id=sv.symbol_id
WHERE sv.inventory_run_id=? ORDER BY s.symbol_key`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []model.Symbol{}
	for rows.Next() {
		var symbol model.Symbol
		var lines string
		if err := rows.Scan(&symbol.Language, &symbol.Path, &symbol.QualifiedName, &symbol.Kind,
			&symbol.StartLine, &symbol.EndLine, &lines, &symbol.SemanticHash, &symbol.SignatureHash, &symbol.BodyHash); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(lines), &symbol.ExecutableLines)
		result = append(result, symbol)
	}
	return result, rows.Err()
}

func (s *Store) RecentInventoryIDs(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM inventory_runs WHERE status='completed' ORDER BY completed_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
