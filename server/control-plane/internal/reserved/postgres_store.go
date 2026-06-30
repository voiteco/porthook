// SPDX-License-Identifier: AGPL-3.0-only

package reserved

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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
		return fmt.Errorf("apply reserved subdomain migrations: %w", err)
	}
	if err := s.verifySchema(ctx); err != nil {
		return fmt.Errorf("verify reserved subdomain schema: %w", err)
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
			AND table_name = 'reserved_subdomains'
			AND column_name IN ('id', 'name', 'token_id', 'created_at')`,
	).Scan(&count); err != nil {
		return err
	}
	if count != requiredPostgresReservationColumns {
		return fmt.Errorf("reserved_subdomains schema is missing required columns: found %d of %d", count, requiredPostgresReservationColumns)
	}
	return nil
}

func (s *PostgresStore) Create(ctx context.Context, record ReservationRecord) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO reserved_subdomains (id, name, token_id, created_at)
		VALUES ($1, $2, $3, $4)`,
		record.ID,
		record.Name,
		record.TokenID,
		record.CreatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "reserved_subdomains_name_key") {
			return ErrReservationAlreadyExists
		}
		return fmt.Errorf("insert reserved subdomain: %w", err)
	}
	return nil
}

func (s *PostgresStore) List(ctx context.Context) ([]ReservationRecord, error) {
	rows, err := s.db.QueryContext(ctx, selectReservationRecordSQL+` ORDER BY name ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list reserved subdomains: %w", err)
	}
	defer rows.Close()

	var records []ReservationRecord
	for rows.Next() {
		record, err := scanReservationRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list reserved subdomains: %w", err)
	}
	return records, nil
}

func (s *PostgresStore) LookupByID(ctx context.Context, id string) (ReservationRecord, bool, error) {
	return s.lookup(ctx, selectReservationRecordSQL+` WHERE id = $1`, id)
}

func (s *PostgresStore) LookupByName(ctx context.Context, name string) (ReservationRecord, bool, error) {
	return s.lookup(ctx, selectReservationRecordSQL+` WHERE name = $1`, name)
}

func (s *PostgresStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM reserved_subdomains WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete reserved subdomain: %w", err)
	}
	return nil
}

func (s *PostgresStore) lookup(ctx context.Context, query, value string) (ReservationRecord, bool, error) {
	record, err := scanReservationRecord(s.db.QueryRowContext(ctx, query, value))
	if errors.Is(err, sql.ErrNoRows) {
		return ReservationRecord{}, false, nil
	}
	if err != nil {
		return ReservationRecord{}, false, fmt.Errorf("lookup reserved subdomain: %w", err)
	}
	return record, true, nil
}

type reservationRecordScanner interface {
	Scan(dest ...any) error
}

const selectReservationRecordSQL = `SELECT id, name, token_id, created_at FROM reserved_subdomains`

const postgresMigrationStateTableSQL = `CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	applied_at TIMESTAMPTZ NOT NULL
)`

const requiredPostgresReservationColumns = 4

func scanReservationRecord(scanner reservationRecordScanner) (ReservationRecord, error) {
	var record ReservationRecord
	err := scanner.Scan(
		&record.ID,
		&record.Name,
		&record.TokenID,
		&record.CreatedAt,
	)
	if err != nil {
		return ReservationRecord{}, err
	}
	return record, nil
}
