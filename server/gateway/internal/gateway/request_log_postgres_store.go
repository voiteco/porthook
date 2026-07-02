// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type PostgresRequestLogStore struct {
	db *sql.DB
}

func NewPostgresRequestLogStore(db *sql.DB) (*PostgresRequestLogStore, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &PostgresRequestLogStore{db: db}, nil
}

func (s *PostgresRequestLogStore) Migrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("database is required")
	}
	if err := s.applyMigrations(ctx, RequestLogPostgresMigrations()); err != nil {
		return fmt.Errorf("apply request log migrations: %w", err)
	}
	if err := s.verifySchema(ctx); err != nil {
		return fmt.Errorf("verify request log schema: %w", err)
	}
	return nil
}

func (s *PostgresRequestLogStore) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("database is required")
	}
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

func (s *PostgresRequestLogStore) Add(ctx context.Context, entry requestLogEntry) error {
	if s == nil || s.db == nil {
		return errors.New("database is required")
	}
	if entry.Time.IsZero() {
		entry.Time = time.Now().UTC()
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO gateway_request_logs (
			time, method, host, path, query_present, remote_ip, request_id, subdomain,
			custom_domain, tunnel_id, stream_id, status, outcome, request_bytes,
			response_bytes, duration_ms, error
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17
		)`,
		entry.Time,
		entry.Method,
		entry.Host,
		entry.Path,
		entry.QueryPresent,
		entry.RemoteIP,
		nullableString(entry.RequestID),
		nullableString(entry.Subdomain),
		nullableString(entry.CustomDomain),
		nullableString(entry.TunnelID),
		nullableString(entry.StreamID),
		entry.Status,
		entry.Outcome,
		entry.RequestBytes,
		entry.ResponseBytes,
		entry.DurationMS,
		nullableString(entry.Error),
	)
	if err != nil {
		return fmt.Errorf("insert gateway request log: %w", err)
	}
	return nil
}

func (s *PostgresRequestLogStore) applyMigrations(ctx context.Context, migrations []RequestLogPostgresMigration) error {
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

	if _, err := tx.ExecContext(ctx, requestLogPostgresMigrationStateTableSQL); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	applied, err := appliedRequestLogPostgresMigrations(ctx, tx)
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

func appliedRequestLogPostgresMigrations(ctx context.Context, tx *sql.Tx) (map[int]bool, error) {
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

func (s *PostgresRequestLogStore) verifySchema(ctx context.Context) error {
	var count int
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = current_schema()
			AND table_name = 'gateway_request_logs'
			AND column_name IN (
				'id', 'time', 'method', 'host', 'path', 'query_present', 'remote_ip',
				'request_id', 'subdomain', 'custom_domain', 'tunnel_id', 'stream_id',
				'status', 'outcome', 'request_bytes', 'response_bytes', 'duration_ms', 'error'
			)`,
	).Scan(&count); err != nil {
		return err
	}
	if count != requiredPostgresRequestLogColumns {
		return fmt.Errorf("gateway_request_logs schema is missing required columns: found %d of %d", count, requiredPostgresRequestLogColumns)
	}
	return nil
}

func nullableString(value string) sql.NullString {
	value = strings.TrimSpace(value)
	return sql.NullString{String: value, Valid: value != ""}
}

const requestLogPostgresMigrationStateTableSQL = `CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	applied_at TIMESTAMPTZ NOT NULL
)`

const requiredPostgresRequestLogColumns = 18
