package repo_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/anggorodewanto/playtesthub/pkg/migrate"
)

// testPool is shared by every *_test.go in this package. TestMain starts
// a single postgres container for the whole package run; individual
// tests call truncateAll to reset state.
var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
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
		log.Fatalf("start postgres container: %v", err)
	}
	defer func() {
		if termErr := pgC.Terminate(ctx); termErr != nil {
			log.Printf("terminate postgres container: %v", termErr)
		}
	}()

	dbURL, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("connection string: %v", err)
	}

	if upErr := migrate.Up(dbURL, migrationsDir()); upErr != nil {
		log.Fatalf("migrate.Up: %v", upErr)
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("pgxpool.New: %v", err)
	}
	testPool = pool

	code := m.Run()

	pool.Close()
	os.Exit(code)
}

// truncateAll resets every table in scope, restarting identity sequences
// and cascading FKs. Intended to be the first line of every test; it is
// cheap next to container start.
func truncateAll(t *testing.T) {
	t.Helper()
	_, err := testPool.Exec(context.Background(),
		`TRUNCATE playtest, code, applicant, audit_log, leader_lease, nda_acceptance RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// migrationsDir resolves the repo-root migrations/ directory from this
// test file's location so `go test` works from any CWD.
func migrationsDir() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		panic(fmt.Errorf("runtime.Caller(0) returned ok=false"))
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
}
