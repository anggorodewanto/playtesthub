package repo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

func newADTPlaytest(slug string) *repo.Playtest {
	ns, game, build := "adt-ns", "game-1", "build-1"
	return &repo.Playtest{
		Namespace:         testNamespace,
		Slug:              slug,
		Title:             "ADT playtest",
		Platforms:         []string{"PC"},
		Status:            "DRAFT",
		DistributionModel: "ADT",
		ADTNamespace:      &ns,
		ADTGameID:         &game,
		ADTBuildID:        &build,
	}
}

// TestMigration0009_PlaytestADTBuildHealthColumns pins the two columns
// added to playtest by migration 0009.
func TestMigration0009_PlaytestADTBuildHealthColumns(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	const sql = `
		SELECT column_name, data_type, is_nullable
		  FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND table_name   = 'playtest'
		   AND column_name IN ('adt_build_status', 'adt_build_checked_at')
		 ORDER BY column_name`

	rows, err := testPool.Query(ctx, sql)
	if err != nil {
		t.Fatalf("query columns: %v", err)
	}
	defer rows.Close()

	type col struct{ name, dataType, isNullable string }
	var got []col
	for rows.Next() {
		var c col
		if scanErr := rows.Scan(&c.name, &c.dataType, &c.isNullable); scanErr != nil {
			t.Fatalf("scan: %v", scanErr)
		}
		got = append(got, c)
	}
	if rows.Err() != nil {
		t.Fatalf("rows.Err: %v", rows.Err())
	}

	want := []col{
		{"adt_build_checked_at", "timestamp with time zone", "YES"},
		{"adt_build_status", "text", "YES"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestMigration0009_BuildStatusEnumCheck pins the CHECK constraint:
// adt_build_status must be NULL, 'OK', or 'UNAVAILABLE'.
func TestMigration0009_BuildStatusEnumCheck(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	_, err := testPool.Exec(ctx,
		`INSERT INTO playtest (namespace, slug, title, distribution_model, adt_namespace, adt_game_id, adt_build_id, adt_build_status)
		 VALUES ($1, $2, $3, 'ADT', $4, $5, $6, 'BOGUS')`,
		"ns", "adt-bad-status", "title", "adt-ns", "g", "b")
	if err == nil {
		t.Fatalf("BOGUS adt_build_status accepted; want CHECK rejection")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != "playtest_adt_build_status_enum" {
		t.Errorf("err = %v; want playtest_adt_build_status_enum violation", err)
	}
}

// TestPlaytestUpdateADTBuild_Persists is the regression guard for the
// ChangeADTBuild persistence bug: the generic Update omits adt_game_id /
// adt_build_id, so a dedicated writer must round-trip them through real
// Postgres (the service-layer fake masked this).
func TestPlaytestUpdateADTBuild_Persists(t *testing.T) {
	truncateAll(t)
	store := repo.NewPgPlaytestStore(testPool)
	ctx := context.Background()

	created, err := store.Create(ctx, newADTPlaytest("adt-update-build"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := store.UpdateADTBuild(ctx, testNamespace, created.ID, "game-2", "build-2")
	if err != nil {
		t.Fatalf("UpdateADTBuild: %v", err)
	}
	if updated.ADTGameID == nil || *updated.ADTGameID != "game-2" {
		t.Errorf("returned adt_game_id = %v, want game-2", updated.ADTGameID)
	}
	if updated.ADTBuildID == nil || *updated.ADTBuildID != "build-2" {
		t.Errorf("returned adt_build_id = %v, want build-2", updated.ADTBuildID)
	}

	// Re-read from the DB — the change must have actually persisted.
	got, err := store.GetByID(ctx, testNamespace, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ADTGameID == nil || *got.ADTGameID != "game-2" {
		t.Errorf("persisted adt_game_id = %v, want game-2", got.ADTGameID)
	}
	if got.ADTBuildID == nil || *got.ADTBuildID != "build-2" {
		t.Errorf("persisted adt_build_id = %v, want build-2", got.ADTBuildID)
	}
}

func TestPlaytestUpdateADTBuild_RefusesSoftDeleted(t *testing.T) {
	truncateAll(t)
	store := repo.NewPgPlaytestStore(testPool)
	ctx := context.Background()

	created, err := store.Create(ctx, newADTPlaytest("adt-update-deleted"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if delErr := store.SoftDelete(ctx, testNamespace, created.ID); delErr != nil {
		t.Fatalf("SoftDelete: %v", delErr)
	}
	if _, err = store.UpdateADTBuild(ctx, testNamespace, created.ID, "g", "b"); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("UpdateADTBuild on soft-deleted: err = %v, want ErrNotFound", err)
	}
}

// TestPlaytestSetADTBuildHealth_Persists round-trips the health columns
// and confirms updated_at is NOT bumped (a health probe is not an edit).
func TestPlaytestSetADTBuildHealth_Persists(t *testing.T) {
	truncateAll(t)
	store := repo.NewPgPlaytestStore(testPool)
	ctx := context.Background()

	created, err := store.Create(ctx, newADTPlaytest("adt-health"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ADTBuildStatus != nil {
		t.Errorf("new row adt_build_status = %v, want nil", created.ADTBuildStatus)
	}

	checkedAt := time.Now().UTC().Truncate(time.Millisecond)
	updated, err := store.SetADTBuildHealth(ctx, testNamespace, created.ID, "UNAVAILABLE", checkedAt)
	if err != nil {
		t.Fatalf("SetADTBuildHealth: %v", err)
	}
	if updated.ADTBuildStatus == nil || *updated.ADTBuildStatus != "UNAVAILABLE" {
		t.Errorf("adt_build_status = %v, want UNAVAILABLE", updated.ADTBuildStatus)
	}
	if updated.ADTBuildCheckedAt == nil || !updated.ADTBuildCheckedAt.Equal(checkedAt) {
		t.Errorf("adt_build_checked_at = %v, want %v", updated.ADTBuildCheckedAt, checkedAt)
	}
	if !updated.UpdatedAt.Equal(created.UpdatedAt) {
		t.Errorf("updated_at changed: got %v, want unchanged %v (health write must not touch updated_at)", updated.UpdatedAt, created.UpdatedAt)
	}

	got, err := store.GetByID(ctx, testNamespace, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ADTBuildStatus == nil || *got.ADTBuildStatus != "UNAVAILABLE" {
		t.Errorf("persisted adt_build_status = %v, want UNAVAILABLE", got.ADTBuildStatus)
	}
}
