// SPDX-License-Identifier: AGPL-3.0-only

package access

import (
	"context"
	"database/sql"
	"encoding/json"
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
		return fmt.Errorf("apply access policy migrations: %w", err)
	}
	if err := s.verifySchema(ctx); err != nil {
		return fmt.Errorf("verify access policy schema: %w", err)
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
			AND table_name = 'access_policies'
			AND column_name IN ('id', 'reserved_subdomain_id', 'mode', 'basic_username', 'secret_hash', 'ip_allowlist_json', 'created_at', 'updated_at')`,
	).Scan(&count); err != nil {
		return err
	}
	if count != requiredPostgresAccessPolicyColumns {
		return fmt.Errorf("access_policies schema is missing required columns: found %d of %d", count, requiredPostgresAccessPolicyColumns)
	}
	return nil
}

func (s *PostgresStore) Create(ctx context.Context, record PolicyRecord) error {
	ipAllowlistJSON, err := encodeIPAllowlist(record.IPAllowlist)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO access_policies (id, reserved_subdomain_id, mode, basic_username, secret_hash, ip_allowlist_json, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		record.ID,
		record.ReservedSubdomainID,
		record.Mode,
		record.BasicUsername,
		record.SecretHash,
		ipAllowlistJSON,
		record.CreatedAt,
		record.UpdatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "access_policies_reserved_subdomain_id_key") || strings.Contains(err.Error(), "access_policies_pkey") {
			return ErrPolicyAlreadyExists
		}
		return fmt.Errorf("insert access policy: %w", err)
	}
	return nil
}

func (s *PostgresStore) Update(ctx context.Context, record PolicyRecord) error {
	ipAllowlistJSON, err := encodeIPAllowlist(record.IPAllowlist)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE access_policies
		SET mode = $1, basic_username = $2, secret_hash = $3, ip_allowlist_json = $4, updated_at = $5
		WHERE id = $6 AND reserved_subdomain_id = $7`,
		record.Mode,
		record.BasicUsername,
		record.SecretHash,
		ipAllowlistJSON,
		record.UpdatedAt,
		record.ID,
		record.ReservedSubdomainID,
	)
	if err != nil {
		return fmt.Errorf("update access policy: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update access policy rows affected: %w", err)
	}
	if affected == 0 {
		return ErrPolicyNotFound
	}
	return nil
}

func (s *PostgresStore) List(ctx context.Context) ([]PolicyRecord, error) {
	rows, err := s.db.QueryContext(ctx, selectPolicyRecordSQL+` ORDER BY reserved_subdomain_id ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list access policies: %w", err)
	}
	defer rows.Close()

	var records []PolicyRecord
	for rows.Next() {
		record, err := scanPolicyRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list access policies: %w", err)
	}
	return records, nil
}

func (s *PostgresStore) LookupByID(ctx context.Context, id string) (PolicyRecord, bool, error) {
	return s.lookup(ctx, selectPolicyRecordSQL+` WHERE id = $1`, id)
}

func (s *PostgresStore) LookupByReservedSubdomainID(ctx context.Context, reservedSubdomainID string) (PolicyRecord, bool, error) {
	return s.lookup(ctx, selectPolicyRecordSQL+` WHERE reserved_subdomain_id = $1`, reservedSubdomainID)
}

func (s *PostgresStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM access_policies WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete access policy: %w", err)
	}
	return nil
}

func (s *PostgresStore) lookup(ctx context.Context, query, value string) (PolicyRecord, bool, error) {
	record, err := scanPolicyRecord(s.db.QueryRowContext(ctx, query, value))
	if errors.Is(err, sql.ErrNoRows) {
		return PolicyRecord{}, false, nil
	}
	if err != nil {
		return PolicyRecord{}, false, fmt.Errorf("lookup access policy: %w", err)
	}
	return record, true, nil
}

type policyRecordScanner interface {
	Scan(dest ...any) error
}

const selectPolicyRecordSQL = `SELECT id, reserved_subdomain_id, mode, basic_username, secret_hash, ip_allowlist_json, created_at, updated_at FROM access_policies`

const postgresMigrationStateTableSQL = `CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	applied_at TIMESTAMPTZ NOT NULL
)`

const requiredPostgresAccessPolicyColumns = 8

func scanPolicyRecord(scanner policyRecordScanner) (PolicyRecord, error) {
	var record PolicyRecord
	var mode string
	var ipAllowlistJSON string
	err := scanner.Scan(
		&record.ID,
		&record.ReservedSubdomainID,
		&mode,
		&record.BasicUsername,
		&record.SecretHash,
		&ipAllowlistJSON,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return PolicyRecord{}, err
	}
	record.Mode = PolicyMode(mode)
	record.IPAllowlist, err = decodeIPAllowlist(ipAllowlistJSON)
	if err != nil {
		return PolicyRecord{}, err
	}
	return record, nil
}

func encodeIPAllowlist(values []string) (string, error) {
	data, err := json.Marshal(cloneStrings(values))
	if err != nil {
		return "", fmt.Errorf("encode ip allowlist: %w", err)
	}
	return string(data), nil
}

func decodeIPAllowlist(raw string) ([]string, error) {
	var values []string
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, fmt.Errorf("decode ip allowlist: %w", err)
	}
	return cloneStrings(values), nil
}
