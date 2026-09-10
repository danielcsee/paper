// Human decisions to leave a symbol untested, scoped to a symbol version
// unless deliberately made durable.
package store

import (
	"context"
	"time"

	_ "modernc.org/sqlite"
)

func (s *Store) ActiveDisposition(ctx context.Context, symbolKey, semanticHash string, now time.Time) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dispositions
WHERE symbol_key=? AND (semantic_hash IS NULL OR semantic_hash=?)
AND (expires_at IS NULL OR expires_at > ?)`, symbolKey, semanticHash, now.UTC().Format(time.RFC3339Nano)).Scan(&count)
	return count > 0, err
}

func (s *Store) AddSkip(ctx context.Context, symbolKey, semanticHash, reason, approvedBy string, expiresAt *time.Time) error {
	var expiry any
	if expiresAt != nil {
		expiry = expiresAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO dispositions(
symbol_key, semantic_hash, disposition, reason, approved_by, created_at, expires_at) VALUES(?,?,?,?,?,?,?)`,
		symbolKey, nullString(semanticHash), "skip", reason, approvedBy,
		time.Now().UTC().Format(time.RFC3339Nano), expiry)
	return err
}

func (s *Store) CountActiveSkips(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dispositions WHERE expires_at IS NULL OR expires_at > ?`,
		time.Now().UTC().Format(time.RFC3339Nano)).Scan(&count)
	return count, err
}
