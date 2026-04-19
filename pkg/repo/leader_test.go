package repo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

func TestLeaderTryAcquire_EmptyRowSucceeds(t *testing.T) {
	truncateAll(t)
	store := repo.NewPgLeaderStore(testPool)

	lease, err := store.TryAcquire(context.Background(), "reclaim-job", "pod-a", 30*time.Second)
	if err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	if lease.Holder != "pod-a" {
		t.Errorf("holder = %q, want pod-a", lease.Holder)
	}
	if !lease.ExpiresAt.After(time.Now()) {
		t.Errorf("expires_at not in future: %v", lease.ExpiresAt)
	}
}

// Key M1-phase-4 invariant: a held-and-unexpired lease blocks a
// different holder.
func TestLeaderTryAcquire_HeldByOtherFails(t *testing.T) {
	truncateAll(t)
	store := repo.NewPgLeaderStore(testPool)
	ctx := context.Background()

	if _, err := store.TryAcquire(ctx, "reclaim-job", "pod-a", 30*time.Second); err != nil {
		t.Fatalf("first TryAcquire: %v", err)
	}
	_, err := store.TryAcquire(ctx, "reclaim-job", "pod-b", 30*time.Second)
	if !errors.Is(err, repo.ErrLeaseHeld) {
		t.Errorf("second holder: got %v, want ErrLeaseHeld", err)
	}
}

// Same holder reclaiming is a no-op / re-entrant acquisition, not a
// block. Used by a restarting leader that crashes mid-heartbeat.
func TestLeaderTryAcquire_SameHolderReentrant(t *testing.T) {
	truncateAll(t)
	store := repo.NewPgLeaderStore(testPool)
	ctx := context.Background()

	if _, err := store.TryAcquire(ctx, "reclaim-job", "pod-a", 30*time.Second); err != nil {
		t.Fatalf("first TryAcquire: %v", err)
	}
	lease2, err := store.TryAcquire(ctx, "reclaim-job", "pod-a", 60*time.Second)
	if err != nil {
		t.Fatalf("re-entrant TryAcquire: %v", err)
	}
	if lease2.Holder != "pod-a" {
		t.Errorf("holder after re-acquire = %q, want pod-a", lease2.Holder)
	}
}

// Key M1-phase-4 invariant: an expired lease can be stolen by a new
// holder — the reclaim job's crash-recovery path.
func TestLeaderTryAcquire_ExpiredLeaseStealable(t *testing.T) {
	truncateAll(t)
	store := repo.NewPgLeaderStore(testPool)
	ctx := context.Background()

	// 1 ms TTL, then sleep long enough for it to expire.
	if _, err := store.TryAcquire(ctx, "reclaim-job", "pod-a", 1*time.Millisecond); err != nil {
		t.Fatalf("first TryAcquire: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	stolen, err := store.TryAcquire(ctx, "reclaim-job", "pod-b", 30*time.Second)
	if err != nil {
		t.Fatalf("steal expired lease: %v", err)
	}
	if stolen.Holder != "pod-b" {
		t.Errorf("holder after steal = %q, want pod-b", stolen.Holder)
	}
}

func TestLeaderRefresh_ExtendsExpiry(t *testing.T) {
	truncateAll(t)
	store := repo.NewPgLeaderStore(testPool)
	ctx := context.Background()

	initial, err := store.TryAcquire(ctx, "reclaim-job", "pod-a", 5*time.Second)
	if err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	refreshed, err := store.Refresh(ctx, "reclaim-job", "pod-a", 30*time.Second)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if !refreshed.ExpiresAt.After(initial.ExpiresAt) {
		t.Errorf("refresh did not advance expires_at: %v !> %v",
			refreshed.ExpiresAt, initial.ExpiresAt)
	}
}

func TestLeaderRefresh_WrongHolderFails(t *testing.T) {
	truncateAll(t)
	store := repo.NewPgLeaderStore(testPool)
	ctx := context.Background()

	if _, err := store.TryAcquire(ctx, "reclaim-job", "pod-a", 30*time.Second); err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	_, err := store.Refresh(ctx, "reclaim-job", "pod-b", 30*time.Second)
	if !errors.Is(err, repo.ErrLeaseHeld) {
		t.Errorf("wrong-holder Refresh: got %v, want ErrLeaseHeld", err)
	}
}

func TestLeaderRelease_ClearsLease(t *testing.T) {
	truncateAll(t)
	store := repo.NewPgLeaderStore(testPool)
	ctx := context.Background()

	if _, err := store.TryAcquire(ctx, "reclaim-job", "pod-a", 30*time.Second); err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	if err := store.Release(ctx, "reclaim-job", "pod-a"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := store.Get(ctx, "reclaim-job"); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("after Release: got %v, want ErrNotFound", err)
	}

	// And a new holder can now claim cleanly.
	if _, err := store.TryAcquire(ctx, "reclaim-job", "pod-b", 30*time.Second); err != nil {
		t.Errorf("post-release TryAcquire: %v", err)
	}
}
