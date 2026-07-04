// SPDX-License-Identifier: AGPL-3.0-only

package admintokens

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) (*PostgresStore, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &PostgresStore{db: db}, nil
}

func (s *PostgresStore) Migrate(ctx context.Context) error {
	if err := s.applyMigrations(ctx, PostgresMigrations()); err != nil {
		return fmt.Errorf("apply admin token migrations: %w", err)
	}
	if err := s.verifySchema(ctx); err != nil {
		return fmt.Errorf("verify admin token schema: %w", err)
	}
	return nil
}

func (s *PostgresStore) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

func (s *PostgresStore) applyMigrations(ctx context.Context, migrations []PostgresMigration) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, postgresMigrationStateTableSQL); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}
	applied, err := appliedPostgresMigrations(ctx, tx)
	if err != nil {
		return err
	}
	for _, migration := range migrations {
		if applied[migration.Version] {
			continue
		}
		if strings.TrimSpace(migration.SQL) == "" {
			return fmt.Errorf("migration %04d_%s is empty", migration.Version, migration.Name)
		}
		if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
			return fmt.Errorf("apply migration %04d_%s: %w", migration.Version, migration.Name, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO schema_migrations (version, name, applied_at) VALUES ($1, $2, NOW())`,
			migration.Version,
			migration.Name,
		); err != nil {
			return fmt.Errorf("record migration %04d_%s: %w", migration.Version, migration.Name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	committed = true
	return nil
}

func appliedPostgresMigrations(ctx context.Context, tx *sql.Tx) (map[int]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("list applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list applied migrations: %w", err)
	}
	return applied, nil
}

func (s *PostgresStore) verifySchema(ctx context.Context) error {
	var count int
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = current_schema()
			AND table_name = 'admin_tokens'
			AND column_name IN ('id', 'name', 'token_hash', 'scopes_json', 'created_at', 'last_used_at', 'revoked_at')`,
	).Scan(&count); err != nil {
		return err
	}
	if count != requiredPostgresTokenColumns {
		return fmt.Errorf("admin_tokens schema is missing required columns: found %d of %d", count, requiredPostgresTokenColumns)
	}
	return nil
}

func (s *PostgresStore) Create(ctx context.Context, record TokenRecord) error {
	scopesJSON, err := encodeScopes(record.Scopes)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO admin_tokens (id, name, token_hash, scopes_json, created_at, last_used_at, revoked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		record.ID,
		record.Name,
		record.TokenHash,
		scopesJSON,
		record.CreatedAt,
		record.LastUsedAt,
		record.RevokedAt,
	)
	if err != nil {
		return fmt.Errorf("insert admin token: %w", err)
	}
	return nil
}

func (s *PostgresStore) List(ctx context.Context) ([]TokenRecord, error) {
	rows, err := s.db.QueryContext(ctx, selectTokenRecordSQL+` ORDER BY created_at DESC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list admin tokens: %w", err)
	}
	defer rows.Close()

	var records []TokenRecord
	for rows.Next() {
		record, err := scanTokenRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list admin tokens: %w", err)
	}
	return records, nil
}

func (s *PostgresStore) LookupByHash(ctx context.Context, tokenHash string) (TokenRecord, bool, error) {
	return s.lookup(ctx, selectTokenRecordSQL+` WHERE token_hash = $1`, tokenHash)
}

func (s *PostgresStore) LookupByID(ctx context.Context, id string) (TokenRecord, bool, error) {
	return s.lookup(ctx, selectTokenRecordSQL+` WHERE id = $1`, id)
}

func (s *PostgresStore) MarkUsed(ctx context.Context, id string, usedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE admin_tokens SET last_used_at = $1 WHERE id = $2 AND revoked_at IS NULL`, usedAt, id)
	if err != nil {
		return fmt.Errorf("mark admin token used: %w", err)
	}
	return nil
}

func (s *PostgresStore) Revoke(ctx context.Context, id string, revokedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE admin_tokens SET revoked_at = $1 WHERE id = $2`, revokedAt, id)
	if err != nil {
		return fmt.Errorf("revoke admin token: %w", err)
	}
	return nil
}

func (s *PostgresStore) lookup(ctx context.Context, query, value string) (TokenRecord, bool, error) {
	record, err := scanTokenRecord(s.db.QueryRowContext(ctx, query, value))
	if errors.Is(err, sql.ErrNoRows) {
		return TokenRecord{}, false, nil
	}
	if err != nil {
		return TokenRecord{}, false, fmt.Errorf("lookup admin token: %w", err)
	}
	return record, true, nil
}

type tokenRecordScanner interface {
	Scan(dest ...any) error
}

const selectTokenRecordSQL = `SELECT id, name, token_hash, scopes_json, created_at, last_used_at, revoked_at FROM admin_tokens`

const postgresMigrationStateTableSQL = `CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	applied_at TIMESTAMPTZ NOT NULL
)`

const requiredPostgresTokenColumns = 7

func scanTokenRecord(scanner tokenRecordScanner) (TokenRecord, error) {
	var record TokenRecord
	var scopesJSON string
	var lastUsedAt sql.NullTime
	var revokedAt sql.NullTime
	err := scanner.Scan(
		&record.ID,
		&record.Name,
		&record.TokenHash,
		&scopesJSON,
		&record.CreatedAt,
		&lastUsedAt,
		&revokedAt,
	)
	if err != nil {
		return TokenRecord{}, err
	}
	record.Scopes, err = decodeScopes(scopesJSON)
	if err != nil {
		return TokenRecord{}, err
	}
	if lastUsedAt.Valid {
		record.LastUsedAt = &lastUsedAt.Time
	}
	if revokedAt.Valid {
		record.RevokedAt = &revokedAt.Time
	}
	return record, nil
}

func encodeScopes(scopes []string) (string, error) {
	data, err := json.Marshal(cloneScopes(scopes))
	if err != nil {
		return "", fmt.Errorf("encode scopes: %w", err)
	}
	return string(data), nil
}

func decodeScopes(raw string) ([]string, error) {
	var scopes []string
	if err := json.Unmarshal([]byte(raw), &scopes); err != nil {
		return nil, fmt.Errorf("decode scopes: %w", err)
	}
	return scopes, nil
}
