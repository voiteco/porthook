// SPDX-License-Identifier: AGPL-3.0-only

package controlplane

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var auditPostgresMigrationFiles embed.FS

type AuditPostgresMigration struct {
	Version int
	Name    string
	SQL     string
}

func AuditPostgresMigrations() []AuditPostgresMigration {
	entries, err := fs.ReadDir(auditPostgresMigrationFiles, "migrations")
	if err != nil {
		panic(fmt.Sprintf("read audit postgres migrations: %v", err))
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	migrations := make([]AuditPostgresMigration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		version, name, err := parseAuditPostgresMigrationName(entry.Name())
		if err != nil {
			panic(err)
		}
		data, err := auditPostgresMigrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			panic(fmt.Sprintf("read audit postgres migration %s: %v", entry.Name(), err))
		}
		migrations = append(migrations, AuditPostgresMigration{
			Version: version,
			Name:    name,
			SQL:     strings.TrimSpace(string(data)),
		})
	}
	return migrations
}

func parseAuditPostgresMigrationName(filename string) (int, string, error) {
	if !strings.HasSuffix(filename, ".sql") {
		return 0, "", fmt.Errorf("audit postgres migration %q must end with .sql", filename)
	}
	base := strings.TrimSuffix(filename, ".sql")
	versionText, name, ok := strings.Cut(base, "_")
	if !ok || versionText == "" || name == "" {
		return 0, "", fmt.Errorf("audit postgres migration %q must use NNNN_name.sql", filename)
	}
	version, err := strconv.Atoi(versionText)
	if err != nil || version < 1 {
		return 0, "", fmt.Errorf("audit postgres migration %q has invalid version", filename)
	}
	return version, name, nil
}
