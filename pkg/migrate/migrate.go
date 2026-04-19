// Package migrate is a thin wrapper around golang-migrate that applies
// every migration in a directory to a Postgres database. Referenced
// from main.go at boot; also exercised by pkg/repo tests in phase 4.
package migrate

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// Up applies every pending migration in migrationsDir against the
// Postgres instance identified by databaseURL. A databaseURL already at
// the latest version is treated as success.
//
// databaseURL must be a PG-style URL (postgres://user:pass@host:port/db?...).
// migrationsDir is a filesystem path — relative or absolute.
func Up(databaseURL, migrationsDir string) error {
	m, err := migrate.New("file://"+migrationsDir, databaseURL)
	if err != nil {
		return fmt.Errorf("initializing migrate: %w", err)
	}
	defer m.Close()

	err = m.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("applying migrations: %w", err)
	}
	return nil
}
