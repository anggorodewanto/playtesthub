package main

import (
	"testing"
	"time"
)

// emptyEnv returns no env values, so parseGlobals tests run in isolation
// from the host environment.
func emptyEnv(string) (string, bool) { return "", false }

func envFromMap(m map[string]string) envSnapshot {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestParseGlobals_Defaults(t *testing.T) {
	g, rest, err := parseGlobals(nil, emptyEnv)
	if err != nil {
		t.Fatalf("parseGlobals: %v", err)
	}
	if g.Addr != "localhost:6565" {
		t.Errorf("Addr default = %q, want localhost:6565", g.Addr)
	}
	if g.Profile != "default" {
		t.Errorf("Profile default = %q, want default", g.Profile)
	}
	if g.Timeout != 10*time.Second {
		t.Errorf("Timeout default = %v, want 10s", g.Timeout)
	}
	if g.InsecureSet {
		t.Error("InsecureSet should default false")
	}
	if len(rest) != 0 {
		t.Errorf("rest should be empty, got %v", rest)
	}
}

func TestParseGlobals_FlagOverridesEnv(t *testing.T) {
	env := envFromMap(map[string]string{
		"PTH_ADDR":      "from-env:6565",
		"PTH_NAMESPACE": "from-env-ns",
		"PTH_TIMEOUT":   "30s",
	})
	g, rest, err := parseGlobals(
		[]string{"--addr", "from-flag:6565", "--timeout=5s", "playtest", "get-public"},
		env,
	)
	if err != nil {
		t.Fatalf("parseGlobals: %v", err)
	}
	if g.Addr != "from-flag:6565" {
		t.Errorf("flag should win: Addr=%q", g.Addr)
	}
	if g.Namespace != "from-env-ns" {
		t.Errorf("env fallback: Namespace=%q", g.Namespace)
	}
	if g.Timeout != 5*time.Second {
		t.Errorf("flag should win: Timeout=%v", g.Timeout)
	}
	if len(rest) != 2 || rest[0] != "playtest" || rest[1] != "get-public" {
		t.Errorf("rest = %v, want [playtest get-public]", rest)
	}
}

func TestParseGlobals_StopsAtSubcommand(t *testing.T) {
	g, rest, err := parseGlobals(
		[]string{"--addr", "x:1", "playtest", "get-public", "--slug", "demo"},
		emptyEnv,
	)
	if err != nil {
		t.Fatalf("parseGlobals: %v", err)
	}
	if g.Addr != "x:1" {
		t.Errorf("Addr=%q, want x:1", g.Addr)
	}
	want := []string{"playtest", "get-public", "--slug", "demo"}
	if len(rest) != len(want) {
		t.Fatalf("rest=%v, want %v", rest, want)
	}
	for i, v := range want {
		if rest[i] != v {
			t.Errorf("rest[%d]=%q, want %q", i, rest[i], v)
		}
	}
}

func TestParseGlobals_StopsAtUnknownFlagForSubcommand(t *testing.T) {
	// Unknown flags belong to the subcommand parser. The global parser
	// must hand them off untouched, not error out.
	_, rest, err := parseGlobals(
		[]string{"--addr", "x:1", "--slug", "demo"},
		emptyEnv,
	)
	if err != nil {
		t.Fatalf("parseGlobals: %v", err)
	}
	want := []string{"--slug", "demo"}
	if len(rest) != len(want) || rest[0] != want[0] || rest[1] != want[1] {
		t.Errorf("rest=%v, want %v", rest, want)
	}
}

func TestParseGlobals_AnonAndInsecure(t *testing.T) {
	g, _, err := parseGlobals([]string{"--anon", "--insecure"}, emptyEnv)
	if err != nil {
		t.Fatalf("parseGlobals: %v", err)
	}
	if !g.Anon {
		t.Error("Anon should be true")
	}
	if !g.Insecure || !g.InsecureSet {
		t.Errorf("Insecure=%v InsecureSet=%v, want both true", g.Insecure, g.InsecureSet)
	}
}

func TestParseGlobals_InsecureWithExplicitValue(t *testing.T) {
	g, _, err := parseGlobals([]string{"--insecure=false", "--addr", "127.0.0.1:6565"}, emptyEnv)
	if err != nil {
		t.Fatalf("parseGlobals: %v", err)
	}
	if !g.InsecureSet || g.Insecure {
		t.Errorf("expected explicit --insecure=false: set=%v val=%v", g.InsecureSet, g.Insecure)
	}
	if g.effectiveInsecure() {
		t.Error("explicit --insecure=false on loopback must NOT auto-flip back to insecure")
	}
}

func TestEffectiveInsecure_LoopbackDefault(t *testing.T) {
	cases := map[string]bool{
		"localhost:6565":   true,
		"127.0.0.1:6565":   true,
		"[::1]:6565":       true,
		"0.0.0.0:6565":     false, // 0.0.0.0 is unspecified, not loopback
		"public.host:6565": false,
		"":                 true, // empty addr is treated as loopback (defensive)
	}
	for addr, wantInsecure := range cases {
		g := &Globals{Addr: addr}
		got := g.effectiveInsecure()
		if got != wantInsecure {
			t.Errorf("effectiveInsecure(addr=%q) = %v, want %v", addr, got, wantInsecure)
		}
	}
}

func TestParseGlobals_MissingFlagValueErrors(t *testing.T) {
	_, _, err := parseGlobals([]string{"--addr"}, emptyEnv)
	if err == nil {
		t.Fatal("expected error for trailing --addr without value")
	}
}

func TestParseGlobals_BadTimeoutErrors(t *testing.T) {
	_, _, err := parseGlobals([]string{"--timeout", "not-a-duration"}, emptyEnv)
	if err == nil {
		t.Fatal("expected error for bad --timeout")
	}
}

func TestParseGlobals_DoubleDashEndsGlobalParsing(t *testing.T) {
	_, rest, err := parseGlobals([]string{"--addr", "x:1", "--", "--addr", "shouldnt-eat-this"}, emptyEnv)
	if err != nil {
		t.Fatalf("parseGlobals: %v", err)
	}
	want := []string{"--addr", "shouldnt-eat-this"}
	if len(rest) != 2 || rest[0] != want[0] || rest[1] != want[1] {
		t.Errorf("rest=%v, want %v", rest, want)
	}
}
