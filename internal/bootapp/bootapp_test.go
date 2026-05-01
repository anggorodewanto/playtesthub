package bootapp_test

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/anggorodewanto/playtesthub/internal/bootapp"
	"github.com/anggorodewanto/playtesthub/pkg/config"
)

// TestNew_RequiredOptions locks the constructor's argument contract.
// Catching a missing required field at New time keeps callers from
// chasing nil-pointer panics deep in the request path. The full
// serve-and-dial path is exercised by e2e/golden_m1_test.go where
// testcontainers gives us a real Postgres pool for free.
func TestNew_RequiredOptions(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot bind 127.0.0.1: %v", err)
	}
	defer listener.Close()

	cases := []struct {
		name string
		opts bootapp.Options
		want string
	}{
		{name: "missing config", opts: bootapp.Options{}, want: "Config"},
		{name: "missing dbpool", opts: bootapp.Options{Config: &config.Config{}}, want: "DBPool"},
		// Listener is checked after Config + DBPool. We can't easily
		// prove the listener-required branch without satisfying the
		// earlier checks; the e2e suite covers the happy path.
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := bootapp.New(context.Background(), tc.opts)
			if err == nil {
				t.Fatalf("New should reject %s; got nil error", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q should mention %q", err.Error(), tc.want)
			}
		})
	}
}
