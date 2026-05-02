package repo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// PRD §4.7 / §5.3: a second AcceptNDA call on the same natural key
// returns the existing row — no error, no new write, original
// accepted_at preserved.
func TestNDAAcceptIdempotent_ReplayReturnsOriginal(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "nda-replay")
	store := repo.NewPgNDAAcceptanceStore(testPool)
	ctx := context.Background()

	userID := uuid.New()
	hash := "v1hash"

	first, replayed, err := store.AcceptIdempotent(ctx, &repo.NDAAcceptance{
		UserID:         userID,
		PlaytestID:     pt.ID,
		NDAVersionHash: hash,
	})
	if err != nil {
		t.Fatalf("first accept: %v", err)
	}
	if replayed {
		t.Error("first accept marked replayed; want false")
	}
	if first.AcceptedAt.IsZero() {
		t.Error("first accept did not populate accepted_at")
	}

	second, replayed, err := store.AcceptIdempotent(ctx, &repo.NDAAcceptance{
		UserID:         userID,
		PlaytestID:     pt.ID,
		NDAVersionHash: hash,
	})
	if err != nil {
		t.Fatalf("second accept: %v", err)
	}
	if !replayed {
		t.Error("second accept marked not-replayed; want true")
	}
	if !second.AcceptedAt.Equal(first.AcceptedAt) {
		t.Errorf("replay accepted_at drifted: first=%v, second=%v", first.AcceptedAt, second.AcceptedAt)
	}
}

// New version hash on the same (user, playtest) is a fresh row, not a
// replay — the §5.3 re-accept after admin NDA-text edit case.
func TestNDAAcceptIdempotent_NewHashIsFreshAcceptance(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "nda-newhash")
	store := repo.NewPgNDAAcceptanceStore(testPool)
	ctx := context.Background()

	userID := uuid.New()

	if _, _, err := store.AcceptIdempotent(ctx, &repo.NDAAcceptance{
		UserID:         userID,
		PlaytestID:     pt.ID,
		NDAVersionHash: "v1",
	}); err != nil {
		t.Fatalf("v1 accept: %v", err)
	}

	got, replayed, err := store.AcceptIdempotent(ctx, &repo.NDAAcceptance{
		UserID:         userID,
		PlaytestID:     pt.ID,
		NDAVersionHash: "v2",
	})
	if err != nil {
		t.Fatalf("v2 accept: %v", err)
	}
	if replayed {
		t.Error("v2 accept marked replayed; want false")
	}
	if got.NDAVersionHash != "v2" {
		t.Errorf("hash = %q, want v2", got.NDAVersionHash)
	}
}

func TestNDAGet_NotFound(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "nda-miss")
	store := repo.NewPgNDAAcceptanceStore(testPool)

	_, err := store.Get(context.Background(), uuid.New(), pt.ID, "any")
	if !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("missing acceptance: got %v, want ErrNotFound", err)
	}
}

// LatestForApplicant returns the newest hash for a given (user,
// playtest) regardless of which version they accepted last; powers the
// §5.3 NdaReacceptRequired derived-state check.
func TestNDALatestForApplicant_ReturnsMostRecentHash(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "nda-latest")
	store := repo.NewPgNDAAcceptanceStore(testPool)
	ctx := context.Background()

	userID := uuid.New()
	hashes := []string{"v1", "v2", "v3"}
	for _, h := range hashes {
		if _, _, err := store.AcceptIdempotent(ctx, &repo.NDAAcceptance{
			UserID:         userID,
			PlaytestID:     pt.ID,
			NDAVersionHash: h,
		}); err != nil {
			t.Fatalf("accept %s: %v", h, err)
		}
	}

	latest, err := store.LatestForApplicant(ctx, userID, pt.ID)
	if err != nil {
		t.Fatalf("LatestForApplicant: %v", err)
	}
	if latest.NDAVersionHash != "v3" {
		t.Errorf("latest hash = %q, want v3", latest.NDAVersionHash)
	}
}

func TestNDALatestForApplicant_NotFound(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "nda-latest-miss")
	store := repo.NewPgNDAAcceptanceStore(testPool)

	_, err := store.LatestForApplicant(context.Background(), uuid.New(), pt.ID)
	if !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("missing latest: got %v, want ErrNotFound", err)
	}
}
