// SPDX-License-Identifier: AGPL-3.0-only

package tokens

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	for _, statement := range PostgresSchemaStatements() {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply token schema: %w", err)
		}
	}
	return nil
}

func (s *PostgresStore) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
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
		`INSERT INTO api_tokens (id, name, token_hash, scopes_json, created_at, last_used_at, revoked_at)
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
		return fmt.Errorf("insert token: %w", err)
	}
	return nil
}

func (s *PostgresStore) LookupByHash(ctx context.Context, tokenHash string) (TokenRecord, bool, error) {
	return s.lookup(ctx, selectTokenRecordSQL+` WHERE token_hash = $1`, tokenHash)
}

func (s *PostgresStore) List(ctx context.Context) ([]TokenRecord, error) {
	rows, err := s.db.QueryContext(ctx, selectTokenRecordSQL+` ORDER BY created_at DESC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
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
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	return records, nil
}

func (s *PostgresStore) LookupByID(ctx context.Context, id string) (TokenRecord, bool, error) {
	return s.lookup(ctx, selectTokenRecordSQL+` WHERE id = $1`, id)
}

func (s *PostgresStore) MarkUsed(ctx context.Context, id string, usedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = $1 WHERE id = $2 AND revoked_at IS NULL`, usedAt, id)
	if err != nil {
		return fmt.Errorf("mark token used: %w", err)
	}
	return nil
}

func (s *PostgresStore) Revoke(ctx context.Context, id string, revokedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE api_tokens SET revoked_at = $1 WHERE id = $2`, revokedAt, id)
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	return nil
}

func (s *PostgresStore) lookup(ctx context.Context, query, value string) (TokenRecord, bool, error) {
	record, err := scanTokenRecord(s.db.QueryRowContext(ctx, query, value))
	if errors.Is(err, sql.ErrNoRows) {
		return TokenRecord{}, false, nil
	}
	if err != nil {
		return TokenRecord{}, false, fmt.Errorf("lookup token: %w", err)
	}
	return record, true, nil
}

type tokenRecordScanner interface {
	Scan(dest ...any) error
}

const selectTokenRecordSQL = `SELECT id, name, token_hash, scopes_json, created_at, last_used_at, revoked_at FROM api_tokens`

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
