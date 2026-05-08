package reclaim

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// fakeLeaseStore is a stand-in for repo.LeaderStore. It tracks
// holder/expiry/release counts so tests can assert on the lifecycle.
type fakeLeaseStore struct {
	mu              sync.Mutex
	currentHolder   string
	expiresAt       time.Time
	tryAcquireCalls int
	refreshCalls    int
	releaseCalls    int
	tryAcquireErr   error
	refreshErr      error
}

func (f *fakeLeaseStore) TryAcquire(_ context.Context, _, holder string, ttl time.Duration) (*repo.LeaderLease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tryAcquireCalls++
	if f.tryAcquireErr != nil {
		return nil, f.tryAcquireErr
	}
	if f.currentHolder != "" && f.currentHolder != holder && time.Now().Before(f.expiresAt) {
		return nil, repo.ErrLeaseHeld
	}
	f.currentHolder = holder
	f.expiresAt = time.Now().Add(ttl)
	return &repo.LeaderLease{Holder: holder, ExpiresAt: f.expiresAt}, nil
}

func (f *fakeLeaseStore) Refresh(_ context.Context, _, holder string, ttl time.Duration) (*repo.LeaderLease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshCalls++
	if f.refreshErr != nil {
		return nil, f.refreshErr
	}
	if f.currentHolder != holder {
		return nil, repo.ErrLeaseHeld
	}
	f.expiresAt = time.Now().Add(ttl)
	return &repo.LeaderLease{Holder: holder, ExpiresAt: f.expiresAt}, nil
}

func (f *fakeLeaseStore) Release(_ context.Context, _, holder string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releaseCalls++
	if f.currentHolder == holder {
		f.currentHolder = ""
	}
	return nil
}

func (f *fakeLeaseStore) Get(_ context.Context, _ string) (*repo.LeaderLease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.currentHolder == "" {
		return nil, repo.ErrNotFound
	}
	return &repo.LeaderLease{Holder: f.currentHolder, ExpiresAt: f.expiresAt}, nil
}

// fakeReclaimer satisfies the narrow Reclaimer interface. Tests assert
// on call counts + the ttl propagated from the worker config.
type fakeReclaimer struct {
	mu             sync.Mutex
	released       int64
	reclaimErr     error
	reclaimCalls   int
	reclaimedSlots []time.Duration
}

func (f *fakeReclaimer) Reclaim(_ context.Context, ttl time.Duration) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reclaimCalls++
	f.reclaimedSlots = append(f.reclaimedSlots, ttl)
	if f.reclaimErr != nil {
		return 0, f.reclaimErr
	}
	return f.released, nil
}

var _ Reclaimer = (*fakeReclaimer)(nil)

// manualTicker drives Worker.tick deterministically: tests push a
// time.Time onto the channel to fire one iteration.
type manualTicker struct {
	ch chan time.Time
}

func newManualTicker() *manualTicker {
	return &manualTicker{ch: make(chan time.Time, 8)}
}

func (m *manualTicker) factory() func(time.Duration) (<-chan time.Time, func()) {
	return func(_ time.Duration) (<-chan time.Time, func()) {
		return m.ch, func() {}
	}
}

func (m *manualTicker) tick() {
	m.ch <- time.Now()
}

// virtualClock lets tests advance simulated time so the
// ReclaimInterval gate inside Worker.tick can be exercised
// deterministically (the heartbeat-only-this-tick branch fires only
// when (clock - lastReclaimAt) < ReclaimInterval).
type virtualClock struct {
	mu  sync.Mutex
	now time.Time
}

func newVirtualClock() *virtualClock {
	return &virtualClock{now: time.Now()}
}

func (c *virtualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *virtualClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newWorker(t *testing.T, cfg Config, leases repo.LeaderStore, codes Reclaimer, mt *manualTicker, buf *bytes.Buffer) *Worker {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	w := New(cfg, leases, codes, logger)
	w.ticker = mt.factory()
	return w
}

// newWorkerWithClock is a variant that wires a virtualClock so the
// test can drive Worker.lastReclaimAt deterministically.
func newWorkerWithClock(t *testing.T, cfg Config, leases repo.LeaderStore, codes Reclaimer, mt *manualTicker, buf *bytes.Buffer, vc *virtualClock) *Worker {
	t.Helper()
	w := newWorker(t, cfg, leases, codes, mt, buf)
	w.clock = vc.Now
	return w
}

func defaultCfg() Config {
	return Config{
		HolderID:          "pod-a",
		LeaseTTL:          30 * time.Second,
		HeartbeatInterval: 10 * time.Second,
		ReclaimInterval:   30 * time.Second,
		ReservationTTL:    60 * time.Second,
	}
}

// runWorkerInBackground starts Run in a goroutine and returns a stop
// helper. Tests drive ticks via manualTicker, then call stop to cancel
// the loop.
func runWorkerInBackground(t *testing.T, w *Worker) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = w.Run(ctx)
		close(done)
	}()
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("worker did not exit after cancel")
		}
	}
}

func TestWorker_AcquiresLeaseAndReclaimsOnTick(t *testing.T) {
	leases := &fakeLeaseStore{}
	codes := &fakeReclaimer{released: 7}
	mt := newManualTicker()
	buf := &bytes.Buffer{}
	w := newWorker(t, defaultCfg(), leases, codes, mt, buf)

	stop := runWorkerInBackground(t, w)
	defer stop()

	mt.tick()
	// Allow the goroutine to process the tick. A bounded poll keeps
	// the test below the 1s mark even on a slow CI executor.
	for i := 0; i < 50; i++ {
		codes.mu.Lock()
		c := codes.reclaimCalls
		codes.mu.Unlock()
		if c >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	codes.mu.Lock()
	defer codes.mu.Unlock()
	if codes.reclaimCalls < 1 {
		t.Fatalf("Reclaim never called; log so far: %s", buf.String())
	}
	if codes.reclaimedSlots[0] != 60*time.Second {
		t.Errorf("Reclaim ttl = %s, want 60s", codes.reclaimedSlots[0])
	}
	if !strings.Contains(buf.String(), `"event":"reclaim_tick"`) {
		t.Errorf("expected reclaim_tick log line; got %s", buf.String())
	}
	if !strings.Contains(buf.String(), `"released":7`) {
		t.Errorf("expected released=7 in log; got %s", buf.String())
	}
}

func TestWorker_FollowerSkipsReclaim(t *testing.T) {
	leases := &fakeLeaseStore{
		currentHolder: "pod-other",
		expiresAt:     time.Now().Add(5 * time.Minute),
	}
	codes := &fakeReclaimer{released: 99}
	mt := newManualTicker()
	buf := &bytes.Buffer{}
	w := newWorker(t, defaultCfg(), leases, codes, mt, buf)

	stop := runWorkerInBackground(t, w)
	defer stop()

	mt.tick()
	// Give it a moment to process — followers don't call Reclaim, so
	// we wait briefly to make sure they never do.
	time.Sleep(50 * time.Millisecond)

	codes.mu.Lock()
	defer codes.mu.Unlock()
	if codes.reclaimCalls != 0 {
		t.Errorf("follower called Reclaim %d times; want 0", codes.reclaimCalls)
	}
}

func TestWorker_ReleasesLeaseOnContextCancel(t *testing.T) {
	leases := &fakeLeaseStore{}
	codes := &fakeReclaimer{}
	mt := newManualTicker()
	buf := &bytes.Buffer{}
	w := newWorker(t, defaultCfg(), leases, codes, mt, buf)

	stop := runWorkerInBackground(t, w)
	mt.tick() // become leader
	for i := 0; i < 50; i++ {
		codes.mu.Lock()
		c := codes.reclaimCalls
		codes.mu.Unlock()
		if c >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()

	leases.mu.Lock()
	defer leases.mu.Unlock()
	if leases.releaseCalls != 1 {
		t.Errorf("Release calls = %d, want 1", leases.releaseCalls)
	}
}

func TestWorker_ReclaimErrorLoggedButLoopContinues(t *testing.T) {
	leases := &fakeLeaseStore{}
	codes := &fakeReclaimer{reclaimErr: errors.New("connection refused")}
	mt := newManualTicker()
	buf := &bytes.Buffer{}
	vc := newVirtualClock()
	w := newWorkerWithClock(t, defaultCfg(), leases, codes, mt, buf, vc)

	stop := runWorkerInBackground(t, w)
	defer stop()

	mt.tick()
	for i := 0; i < 50; i++ {
		codes.mu.Lock()
		c := codes.reclaimCalls
		codes.mu.Unlock()
		if c >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Advance virtual time past ReclaimInterval so the second tick
	// crosses the sweep gate; without this the heartbeat-only branch
	// would skip Reclaim and the test would hang.
	vc.Advance(defaultCfg().ReclaimInterval + time.Second)
	mt.tick()

	for i := 0; i < 50; i++ {
		codes.mu.Lock()
		c := codes.reclaimCalls
		codes.mu.Unlock()
		if c >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	codes.mu.Lock()
	defer codes.mu.Unlock()
	if codes.reclaimCalls < 2 {
		t.Errorf("loop stopped after error: reclaimCalls=%d", codes.reclaimCalls)
	}
	if !strings.Contains(buf.String(), "reclaim sweep failed") {
		t.Errorf("expected error log; got %s", buf.String())
	}
}

// Heartbeat-only tick: between sweep windows, ticks must refresh the
// lease but skip Reclaim. Advancing the virtual clock by less than
// ReclaimInterval after the first tick guarantees the second tick
// stays in the heartbeat-only branch — proves the new sweep gate
// honours PRD §5.9's 30s cadence even when the underlying ticker
// fires at the 10s heartbeat cadence.
func TestWorker_HeartbeatOnlyTick_SkipsReclaimWithinInterval(t *testing.T) {
	leases := &fakeLeaseStore{}
	codes := &fakeReclaimer{released: 1}
	mt := newManualTicker()
	buf := &bytes.Buffer{}
	vc := newVirtualClock()
	cfg := defaultCfg()
	w := newWorkerWithClock(t, cfg, leases, codes, mt, buf, vc)

	stop := runWorkerInBackground(t, w)
	defer stop()

	mt.tick()
	for i := 0; i < 50; i++ {
		codes.mu.Lock()
		c := codes.reclaimCalls
		codes.mu.Unlock()
		if c >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Heartbeat-only window: advance < ReclaimInterval and tick again.
	vc.Advance(cfg.HeartbeatInterval) // 10s, well under 30s
	mt.tick()
	time.Sleep(50 * time.Millisecond) // give the goroutine time to process

	leases.mu.Lock()
	heartbeats := leases.refreshCalls
	leases.mu.Unlock()
	codes.mu.Lock()
	reclaims := codes.reclaimCalls
	codes.mu.Unlock()

	if reclaims != 1 {
		t.Errorf("reclaim ran inside the heartbeat window: reclaimCalls=%d, want 1", reclaims)
	}
	if heartbeats < 2 {
		t.Errorf("heartbeat did not run on the second tick: refreshCalls=%d, want >=2", heartbeats)
	}
}
