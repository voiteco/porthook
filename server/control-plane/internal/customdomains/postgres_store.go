// SPDX-License-Identifier: AGPL-3.0-only

package customdomains

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
		return fmt.Errorf("apply custom domain migrations: %w", err)
	}
	if err := s.verifySchema(ctx); err != nil {
		return fmt.Errorf("verify custom domain schema: %w", err)
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
			AND table_name = 'custom_domains'
			AND column_name IN ('id', 'hostname', 'reserved_subdomain_id', 'status', 'created_at', 'updated_at')`,
	).Scan(&count); err != nil {
		return err
	}
	if count != requiredPostgresCustomDomainColumns {
		return fmt.Errorf("custom_domains schema is missing required columns: found %d of %d", count, requiredPostgresCustomDomainColumns)
	}
	return nil
}

func (s *PostgresStore) Create(ctx context.Context, record DomainRecord) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO custom_domains (id, hostname, reserved_subdomain_id, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		record.ID,
		record.Hostname,
		record.ReservedSubdomainID,
		record.Status,
		record.CreatedAt,
		record.UpdatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "custom_domains_hostname_key") || strings.Contains(err.Error(), "custom_domains_pkey") {
			return ErrDomainAlreadyExists
		}
		return fmt.Errorf("insert custom domain: %w", err)
	}
	return nil
}

func (s *PostgresStore) List(ctx context.Context) ([]DomainRecord, error) {
	rows, err := s.db.QueryContext(ctx, selectDomainRecordSQL+` ORDER BY hostname ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list custom domains: %w", err)
	}
	defer rows.Close()

	var records []DomainRecord
	for rows.Next() {
		record, err := scanDomainRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list custom domains: %w", err)
	}
	return records, nil
}

func (s *PostgresStore) LookupByID(ctx context.Context, id string) (DomainRecord, bool, error) {
	return s.lookup(ctx, selectDomainRecordSQL+` WHERE id = $1`, id)
}

func (s *PostgresStore) LookupByHostname(ctx context.Context, hostname string) (DomainRecord, bool, error) {
	return s.lookup(ctx, selectDomainRecordSQL+` WHERE hostname = $1`, hostname)
}

func (s *PostgresStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM custom_domains WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete custom domain: %w", err)
	}
	return nil
}

func (s *PostgresStore) lookup(ctx context.Context, query, value string) (DomainRecord, bool, error) {
	record, err := scanDomainRecord(s.db.QueryRowContext(ctx, query, value))
	if errors.Is(err, sql.ErrNoRows) {
		return DomainRecord{}, false, nil
	}
	if err != nil {
		return DomainRecord{}, false, fmt.Errorf("lookup custom domain: %w", err)
	}
	return record, true, nil
}

type domainRecordScanner interface {
	Scan(dest ...any) error
}

const selectDomainRecordSQL = `SELECT id, hostname, reserved_subdomain_id, status, created_at, updated_at FROM custom_domains`

const postgresMigrationStateTableSQL = `CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	applied_at TIMESTAMPTZ NOT NULL
)`

const requiredPostgresCustomDomainColumns = 6

func scanDomainRecord(scanner domainRecordScanner) (DomainRecord, error) {
	var record DomainRecord
	var status string
	err := scanner.Scan(
		&record.ID,
		&record.Hostname,
		&record.ReservedSubdomainID,
		&status,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return DomainRecord{}, err
	}
	record.Status = DomainStatus(status)
	return record, nil
}
