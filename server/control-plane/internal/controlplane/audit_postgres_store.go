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

func (s *PostgresAuditEventStore) List(ctx context.Context, opts AuditEventListOptions) (AuditEventListPage, error) {
	limit := normalizedAuditEventLimit(opts.Limit)
	where, args := auditEventSQLFilters(opts)
	args = append(args, limit+1)
	query := `SELECT id, time, level, message, event, method, path, remote_ip, request_id, fields_json::text
		FROM audit_events`
	if where != "" {
		query += " WHERE " + where
	}
	query += fmt.Sprintf(" ORDER BY time DESC, id DESC LIMIT $%d", len(args))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return AuditEventListPage{}, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()

	var events []AuditEvent
	for rows.Next() {
		event, err := scanAuditEvent(rows)
		if err != nil {
			return AuditEventListPage{}, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return AuditEventListPage{}, fmt.Errorf("list audit events: %w", err)
	}
	return auditEventPage(events, limit), nil
}

func (s *PostgresAuditEventStore) PruneBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("database is required")
	}
	if cutoff.IsZero() {
		return 0, nil
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM audit_events WHERE time < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune audit events: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune audit events rows affected: %w", err)
	}
	return count, nil
}

func auditEventSQLFilters(opts AuditEventListOptions) (string, []any) {
	var where []string
	var args []any

	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	addLike := func(column, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		add(column+" ILIKE $%d", "%"+value+"%")
	}

	addLike("event", opts.Event)
	if strings.TrimSpace(opts.Level) != "" {
		add("level = $%d", strings.ToUpper(strings.TrimSpace(opts.Level)))
	}
	addLike("request_id", opts.RequestID)
	addLike("remote_ip", opts.RemoteIP)
	addLike("fields_json::text", opts.Field)
	if !opts.Since.IsZero() {
		add("time >= $%d", opts.Since)
	}
	if !opts.Until.IsZero() {
		add("time <= $%d", opts.Until)
	}
	if opts.Cursor.ID > 0 && !opts.Cursor.Time.IsZero() {
		args = append(args, opts.Cursor.Time, opts.Cursor.ID)
		where = append(where, fmt.Sprintf("(time < $%d OR (time = $%d AND id < $%d))", len(args)-1, len(args)-1, len(args)))
	}

	return strings.Join(where, " AND "), args
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
		&event.ID,
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
