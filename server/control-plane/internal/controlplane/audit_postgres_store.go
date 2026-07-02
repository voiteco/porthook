// SPDX-License-Identifier: AGPL-3.0-only

package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type PostgresAuditEventStore struct {
	db *sql.DB
}

func NewPostgresAuditEventStore(db *sql.DB) (*PostgresAuditEventStore, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &PostgresAuditEventStore{db: db}, nil
}

func (s *PostgresAuditEventStore) Migrate(ctx context.Context) error {
	if err := s.applyMigrations(ctx, AuditPostgresMigrations()); err != nil {
		return fmt.Errorf("apply audit event migrations: %w", err)
	}
	if err := s.verifySchema(ctx); err != nil {
		return fmt.Errorf("verify audit event schema: %w", err)
	}
	return nil
}

func (s *PostgresAuditEventStore) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

func (s *PostgresAuditEventStore) Add(ctx context.Context, event AuditEvent) error {
	fieldsJSON, err := encodeAuditEventFields(event.Fields)
	if err != nil {
		return err
	}
	requestID := sql.NullString{String: event.RequestID, Valid: strings.TrimSpace(event.RequestID) != ""}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO audit_events (time, level, message, event, method, path, remote_ip, request_id, fields_json)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)`,
		event.Time,
		event.Level,
		event.Message,
		event.Event,
		event.Method,
		event.Path,
		event.RemoteIP,
		requestID,
		fieldsJSON,
	)
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

func (s *PostgresAuditEventStore) List(ctx context.Context, limit int) ([]AuditEvent, error) {
	if limit <= 0 {
		limit = defaultAuditEventLimit
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT time, level, message, event, method, path, remote_ip, request_id, fields_json::text
		FROM audit_events
		ORDER BY time DESC, id DESC
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()

	var events []AuditEvent
	for rows.Next() {
		event, err := scanAuditEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	return events, nil
}

func (s *PostgresAuditEventStore) applyMigrations(ctx context.Context, migrations []AuditPostgresMigration) error {
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

	if _, err := tx.ExecContext(ctx, auditPostgresMigrationStateTableSQL); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	applied, err := appliedAuditPostgresMigrations(ctx, tx)
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

func appliedAuditPostgresMigrations(ctx context.Context, tx *sql.Tx) (map[int]bool, error) {
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

func (s *PostgresAuditEventStore) verifySchema(ctx context.Context) error {
	var count int
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = current_schema()
			AND table_name = 'audit_events'
			AND column_name IN ('id', 'time', 'level', 'message', 'event', 'method', 'path', 'remote_ip', 'request_id', 'fields_json')`,
	).Scan(&count); err != nil {
		return err
	}
	if count != requiredAuditEventColumns {
		return fmt.Errorf("audit_events schema is missing required columns: found %d of %d", count, requiredAuditEventColumns)
	}
	return nil
}

type auditEventScanner interface {
	Scan(dest ...any) error
}

func scanAuditEvent(scanner auditEventScanner) (AuditEvent, error) {
	var event AuditEvent
	var requestID sql.NullString
	var fieldsJSON string
	err := scanner.Scan(
		&event.Time,
		&event.Level,
		&event.Message,
		&event.Event,
		&event.Method,
		&event.Path,
		&event.RemoteIP,
		&requestID,
		&fieldsJSON,
	)
	if err != nil {
		return AuditEvent{}, err
	}
	if requestID.Valid {
		event.RequestID = requestID.String
	}
	event.Fields, err = decodeAuditEventFields(fieldsJSON)
	if err != nil {
		return AuditEvent{}, err
	}
	return event, nil
}

func encodeAuditEventFields(fields map[string]string) (string, error) {
	if fields == nil {
		fields = map[string]string{}
	}
	data, err := json.Marshal(fields)
	if err != nil {
		return "", fmt.Errorf("encode audit event fields: %w", err)
	}
	return string(data), nil
}

func decodeAuditEventFields(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var fields map[string]string
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return nil, fmt.Errorf("decode audit event fields: %w", err)
	}
	if len(fields) == 0 {
		return nil, nil
	}
	return fields, nil
}

const auditPostgresMigrationStateTableSQL = `CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	applied_at TIMESTAMPTZ NOT NULL
)`

const requiredAuditEventColumns = 10
