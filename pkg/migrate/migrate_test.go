package migrate_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/anggorodewanto/playtesthub/pkg/migrate"
)

func TestUpCreatesAllTables(t *testing.T) {
	ctx := context.Background()

	pgC, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("playtesthub_test"),
		postgres.WithUsername("playtesthub"),
		postgres.WithPassword("playtesthub"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if termErr := pgC.Terminate(ctx); termErr != nil {
			t.Logf("terminate postgres container: %v", termErr)
		}
	})

	dbURL, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	if upErr := migrate.Up(dbURL, migrationsDir(t)); upErr != nil {
		t.Fatalf("migrate.Up: %v", upErr)
	}

	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("pgx.Connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })

	want := []string{"playtest", "code", "applicant", "leader_lease", "audit_log"}
	for _, table := range want {
		var exists bool
		queryErr := conn.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = 'public' AND table_name = $1
			)`, table).Scan(&exists)
		if queryErr != nil {
			t.Errorf("query for table %q: %v", table, queryErr)
			continue
		}
		if !exists {
			t.Errorf("table %q was not created by migration 0001", table)
		}
	}

	// Run Up a second time — should be a no-op, not an error.
	if upErr := migrate.Up(dbURL, migrationsDir(t)); upErr != nil {
		t.Fatalf("second migrate.Up (idempotency): %v", upErr)
	}
}

// migrationsDir returns the absolute path to repo-root migrations/,
// resolved from this test file's location so `go test` works from any CWD.
func migrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
}
