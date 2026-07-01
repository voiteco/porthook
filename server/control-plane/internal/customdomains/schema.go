// SPDX-License-Identifier: AGPL-3.0-only

package customdomains

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var postgresMigrationFiles embed.FS

type PostgresMigration struct {
	Version int
	Name    string
	SQL     string
}

func PostgresMigrations() []PostgresMigration {
	entries, err := fs.ReadDir(postgresMigrationFiles, "migrations")
	if err != nil {
		panic(fmt.Sprintf("read postgres custom domain migrations: %v", err))
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	migrations := make([]PostgresMigration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		version, name, err := parsePostgresMigrationName(entry.Name())
		if err != nil {
			panic(err)
		}
		data, err := postgresMigrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			panic(fmt.Sprintf("read postgres custom domain migration %s: %v", entry.Name(), err))
		}
		migrations = append(migrations, PostgresMigration{
			Version: version,
			Name:    name,
			SQL:     strings.TrimSpace(string(data)),
		})
	}
	return migrations
}

func parsePostgresMigrationName(filename string) (int, string, error) {
	if !strings.HasSuffix(filename, ".sql") {
		return 0, "", fmt.Errorf("postgres migration %q must end with .sql", filename)
	}
	base := strings.TrimSuffix(filename, ".sql")
	versionText, name, ok := strings.Cut(base, "_")
	if !ok || versionText == "" || name == "" {
		return 0, "", fmt.Errorf("postgres migration %q must use NNNN_name.sql", filename)
	}
	version, err := strconv.Atoi(versionText)
	if err != nil || version < 1 {
		return 0, "", fmt.Errorf("postgres migration %q has invalid version", filename)
	}
	return version, name, nil
}
