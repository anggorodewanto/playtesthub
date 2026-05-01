// Package e2e_test owns the playtesthub end-to-end suite. Each test
// boots a real backend in-process against a testcontainers-postgres and
// drives it through the `pth` CLI binary, mirroring how operators and
// AI agents exercise the system in production. Tests are gated on a
// real AGS IAM tenant per cli.md §7.4 — when the required env vars
// below are unset the suite skips cleanly so CI without secrets stays
// green.
//
// Required env (any unset → suite skipped):
//   - AGS_BASE_URL          — operator's AGS host (e.g. https://abtestdewa.internal.gamingservices.accelbyte.io)
//   - AGS_NAMESPACE         — operator's AGS game namespace
//   - AGS_IAM_CLIENT_ID     — confidential AGS IAM client (for the in-process backend's token validator + admin endpoints)
//   - AGS_IAM_CLIENT_SECRET — confidential AGS IAM client secret
//   - E2E_USERNAME          — admin username for the ROPC grant
//   - E2E_PASSWORD          — admin password (sent via --password-stdin so it never lands in argv)
//
// Optional env:
//   - PTH_IAM_CLIENT_ID     — distinct ROPC client for `pth auth login --password` (defaults to AGS_IAM_CLIENT_ID)
//   - PTH_IAM_CLIENT_SECRET — paired secret for the above (defaults to AGS_IAM_CLIENT_SECRET)
package e2e_test

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/anggorodewanto/playtesthub/internal/bootapp"
	"github.com/anggorodewanto/playtesthub/pkg/config"
	"github.com/anggorodewanto/playtesthub/pkg/migrate"
)

// e2eEnv carries everything a test needs to drive the CLI. Resolved
// once via loadE2EEnv; an empty Skip field means the suite is good to
// go.
type e2eEnv struct {
	AGSBaseURL         string
	AGSNamespace       string
	AGSIAMClientID     string
	AGSIAMClientSecret string
	PTHIAMClientID     string
	PTHIAMClientSecret string
	AdminUsername      string
	AdminPassword      string
	Skip               string // non-empty → skip the suite with this reason
}

func loadE2EEnv() e2eEnv {
	env := e2eEnv{
		AGSBaseURL:         os.Getenv("AGS_BASE_URL"),
		AGSNamespace:       os.Getenv("AGS_NAMESPACE"),
		AGSIAMClientID:     os.Getenv("AGS_IAM_CLIENT_ID"),
		AGSIAMClientSecret: os.Getenv("AGS_IAM_CLIENT_SECRET"),
		PTHIAMClientID:     os.Getenv("PTH_IAM_CLIENT_ID"),
		PTHIAMClientSecret: os.Getenv("PTH_IAM_CLIENT_SECRET"),
		AdminUsername:      os.Getenv("E2E_USERNAME"),
		AdminPassword:      os.Getenv("E2E_PASSWORD"),
	}
	if env.PTHIAMClientID == "" {
		env.PTHIAMClientID = env.AGSIAMClientID
	}
	if env.PTHIAMClientSecret == "" {
		env.PTHIAMClientSecret = env.AGSIAMClientSecret
	}

	required := map[string]string{
		"AGS_BASE_URL":          env.AGSBaseURL,
		"AGS_NAMESPACE":         env.AGSNamespace,
		"AGS_IAM_CLIENT_ID":     env.AGSIAMClientID,
		"AGS_IAM_CLIENT_SECRET": env.AGSIAMClientSecret,
		"E2E_USERNAME":          env.AdminUsername,
		"E2E_PASSWORD":          env.AdminPassword,
	}
	for k, v := range required {
		if v == "" {
			env.Skip = fmt.Sprintf("e2e suite skipped: %s unset", k)
			return env
		}
	}
	return env
}

// suiteHarness is the shared per-suite bootstrap: testcontainers
// postgres, in-process backend, built pth binary. setupHarness is
// idempotent and lazy — the first test that actually runs pays the
// container-spin cost; if all tests skip (env unset), nothing boots.
type suiteHarness struct {
	once   sync.Once
	err    error
	env    e2eEnv
	addr   string
	pthBin string
	creds  string
	logDir string

	// State for cleanup.
	teardown func()
}

var harness suiteHarness

func getHarness(t *testing.T) *suiteHarness {
	t.Helper()
	harness.once.Do(func() {
		harness.env = loadE2EEnv()
		if harness.env.Skip != "" {
			return
		}
		harness.err = harness.setup()
	})
	if harness.env.Skip != "" {
		t.Skip(harness.env.Skip)
	}
	if harness.err != nil {
		t.Fatalf("e2e setup: %v", harness.err)
	}
	return &harness
}

func (h *suiteHarness) setup() error {
	ctx := context.Background()

	// 1. Postgres container.
	pgC, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("playtesthub_e2e"),
		postgres.WithUsername("playtesthub"),
		postgres.WithPassword("playtesthub"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		return fmt.Errorf("postgres container: %w", err)
	}
	dbURL, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = pgC.Terminate(ctx)
		return fmt.Errorf("connection string: %w", err)
	}
	if err := migrate.Up(dbURL, migrationsDir()); err != nil {
		_ = pgC.Terminate(ctx)
		return fmt.Errorf("migrate up: %w", err)
	}
	dbPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		_ = pgC.Terminate(ctx)
		return fmt.Errorf("pgxpool new: %w", err)
	}

	// 2. Bootapp on a free port. Auth ENABLED so the backend validates
	// real AGS-issued JWTs — that's the whole point of running against
	// the operator's tenant.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		dbPool.Close()
		_ = pgC.Terminate(ctx)
		return fmt.Errorf("listen: %w", err)
	}
	cfg := &config.Config{
		DatabaseURL:            dbURL,
		DiscordBotToken:        "", // not exercised in M1 e2e
		AGSIAMClientID:         h.env.AGSIAMClientID,
		AGSIAMClientSecret:     h.env.AGSIAMClientSecret,
		AGSBaseURL:             h.env.AGSBaseURL,
		AGSNamespace:           h.env.AGSNamespace,
		BasePath:               "/playtesthub",
		AuthEnabled:            true,
		RefreshIntervalSeconds: 600,
		LogLevel:               "warn", // less noise in test output
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	srv, err := bootapp.New(ctx, bootapp.Options{
		Config:   cfg,
		DBPool:   dbPool,
		Listener: listener,
		Logger:   logger,
	})
	if err != nil {
		_ = listener.Close()
		dbPool.Close()
		_ = pgC.Terminate(ctx)
		return fmt.Errorf("bootapp new: %w", err)
	}
	h.addr = srv.Addr()

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve()
	}()

	// 3. Build pth binary.
	tmpDir, err := os.MkdirTemp("", "pth-e2e-*")
	if err != nil {
		srv.Stop()
		dbPool.Close()
		_ = pgC.Terminate(ctx)
		return fmt.Errorf("tempdir: %w", err)
	}
	pthBin := filepath.Join(tmpDir, "pth")
	build := exec.Command("go", "build", "-o", pthBin, "./cmd/pth")
	build.Dir = repoRoot()
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		srv.Stop()
		dbPool.Close()
		_ = pgC.Terminate(ctx)
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("go build pth: %w", err)
	}
	h.pthBin = pthBin
	h.creds = filepath.Join(tmpDir, "credentials.json")
	h.logDir = tmpDir

	h.teardown = func() {
		srv.Stop()
		// Drain serveErr so a delayed Serve() return doesn't leak.
		select {
		case <-serveErr:
		case <-time.After(2 * time.Second):
		}
		dbPool.Close()
		if termErr := pgC.Terminate(context.Background()); termErr != nil {
			fmt.Fprintf(os.Stderr, "e2e: terminate postgres: %v\n", termErr)
		}
		_ = os.RemoveAll(tmpDir)
	}
	return nil
}

// TestMain runs the suite. Cleanup is wired through the lazy harness so
// no work happens when the suite is skipped.
func TestMain(m *testing.M) {
	code := m.Run()
	if harness.teardown != nil {
		harness.teardown()
	}
	os.Exit(code)
}

// repoRoot resolves the playtesthub repo root from this test file's
// location so `go test ./e2e/...` works from any CWD.
func repoRoot() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("runtime.Caller(0) returned ok=false")
	}
	return filepath.Join(filepath.Dir(thisFile), "..")
}

func migrationsDir() string {
	return filepath.Join(repoRoot(), "migrations")
}
