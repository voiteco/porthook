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

// Stats returns the underlying connection pool's current statistics.
func (s *PostgresRequestLogStore) Stats() sql.DBStats {
	return s.db.Stats()
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

func (s *PostgresRequestLogStore) List(ctx context.Context, opts requestLogListOptions) (requestLogListPage, error) {
	if s == nil || s.db == nil {
		return requestLogListPage{}, errors.New("database is required")
	}
	limit := normalizedRequestLogLimit(opts.Limit)
	if limit <= 0 {
		return requestLogListPage{}, nil
	}
	where, args := requestLogSQLFilters(opts)
	args = append(args, limit+1)
	query := `SELECT id, time, method, host, path, query_present, remote_ip, request_id,
			subdomain, custom_domain, tunnel_id, stream_id, status, outcome,
			request_bytes, response_bytes, duration_ms, error
		FROM gateway_request_logs`
	if where != "" {
		query += " WHERE " + where
	}
	query += fmt.Sprintf(" ORDER BY time DESC, id DESC LIMIT $%d", len(args))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return requestLogListPage{}, fmt.Errorf("list gateway request logs: %w", err)
	}
	defer rows.Close()

	var entries []requestLogEntry
	for rows.Next() {
		entry, err := scanRequestLogEntry(rows)
		if err != nil {
			return requestLogListPage{}, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return requestLogListPage{}, fmt.Errorf("list gateway request logs: %w", err)
	}
	return requestLogPage(entries, limit), nil
}

func (s *PostgresRequestLogStore) PruneBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("database is required")
	}
	if cutoff.IsZero() {
		return 0, nil
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM gateway_request_logs WHERE time < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune gateway request logs: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune gateway request logs rows affected: %w", err)
	}
	return count, nil
}

func requestLogSQLFilters(opts requestLogListOptions) (string, []any) {
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

	if strings.TrimSpace(opts.Filter.Subdomain) != "" {
		add("LOWER(subdomain) = $%d", strings.ToLower(strings.TrimSpace(opts.Filter.Subdomain)))
	}
	if opts.Filter.Status != 0 {
		add("status = $%d", opts.Filter.Status)
	}
	addLike("outcome", opts.Filter.Outcome)
	addLike("request_id", opts.Filter.RequestID)
	addLike("host", opts.Filter.Host)
	if strings.TrimSpace(opts.Filter.Method) != "" {
		add("method = $%d", strings.ToUpper(strings.TrimSpace(opts.Filter.Method)))
	}
	addLike("path", opts.Filter.Path)
	addLike("tunnel_id", opts.Filter.TunnelID)
	if !opts.Filter.Since.IsZero() {
		add("time >= $%d", opts.Filter.Since)
	}
	if !opts.Filter.Until.IsZero() {
		add("time <= $%d", opts.Filter.Until)
	}
	if opts.Cursor.ID > 0 && !opts.Cursor.Time.IsZero() {
		args = append(args, opts.Cursor.Time, opts.Cursor.ID)
		where = append(where, fmt.Sprintf("(time < $%d OR (time = $%d AND id < $%d))", len(args)-1, len(args)-1, len(args)))
	}

	return strings.Join(where, " AND "), args
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

type requestLogScanner interface {
	Scan(dest ...any) error
}

func scanRequestLogEntry(scanner requestLogScanner) (requestLogEntry, error) {
	var entry requestLogEntry
	var requestID sql.NullString
	var subdomain sql.NullString
	var customDomain sql.NullString
	var tunnelID sql.NullString
	var streamID sql.NullString
	var entryError sql.NullString
	err := scanner.Scan(
		&entry.ID,
		&entry.Time,
		&entry.Method,
		&entry.Host,
		&entry.Path,
		&entry.QueryPresent,
		&entry.RemoteIP,
		&requestID,
		&subdomain,
		&customDomain,
		&tunnelID,
		&streamID,
		&entry.Status,
		&entry.Outcome,
		&entry.RequestBytes,
		&entry.ResponseBytes,
		&entry.DurationMS,
		&entryError,
	)
	if err != nil {
		return requestLogEntry{}, err
	}
	if requestID.Valid {
		entry.RequestID = requestID.String
	}
	if subdomain.Valid {
		entry.Subdomain = subdomain.String
	}
	if customDomain.Valid {
		entry.CustomDomain = customDomain.String
	}
	if tunnelID.Valid {
		entry.TunnelID = tunnelID.String
	}
	if streamID.Valid {
		entry.StreamID = streamID.String
	}
	if entryError.Valid {
		entry.Error = entryError.String
	}
	return entry, nil
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
