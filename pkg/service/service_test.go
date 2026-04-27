package service

import (
	"testing"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
)

// TestServiceDescriptorMethods guards against accidental RPC removal
// during future proto edits. If the M1 surface in PRD §4.7 shrinks
// silently, this test fails.
func TestServiceDescriptorMethods(t *testing.T) {
	want := map[string]bool{
		"GetPublicPlaytest":        true,
		"GetPlaytestForPlayer":     true,
		"AdminGetPlaytest":         true,
		"ListPlaytests":            true,
		"CreatePlaytest":           true,
		"EditPlaytest":             true,
		"SoftDeletePlaytest":       true,
		"TransitionPlaytestStatus": true,
		"Signup":                   true,
		"GetApplicantStatus":       true,
		"GetDiscordLoginUrl":       true,
	}

	methods := pb.PlaytesthubService_ServiceDesc.Methods
	if len(methods) != len(want) {
		t.Fatalf("method count mismatch: got %d, want %d", len(methods), len(want))
	}

	for _, m := range methods {
		if !want[m.MethodName] {
			t.Errorf("unexpected method in service descriptor: %q", m.MethodName)
		}
		delete(want, m.MethodName)
	}
	for missing := range want {
		t.Errorf("missing method in service descriptor: %q", missing)
	}
}
