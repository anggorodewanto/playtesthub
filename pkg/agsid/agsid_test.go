package agsid_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/anggorodewanto/playtesthub/pkg/agsid"
)

func TestFormat_StripsDashes(t *testing.T) {
	u := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	got := agsid.Format(u)
	want := "550e8400e29b41d4a716446655440000"
	if got != want {
		t.Fatalf("Format = %q, want %q", got, want)
	}
	if strings.ContainsRune(got, '-') {
		t.Fatalf("Format = %q still contains '-'", got)
	}
	if len(got) != 32 {
		t.Fatalf("Format = %q, len=%d, want 32", got, len(got))
	}
}

func TestFormat_NilUUID(t *testing.T) {
	got := agsid.Format(uuid.Nil)
	want := "00000000000000000000000000000000"
	if got != want {
		t.Fatalf("Format(uuid.Nil) = %q, want %q", got, want)
	}
}

func TestFormat_RoundTripAcceptedByParse(t *testing.T) {
	// uuid.Parse accepts both dashed and dashless forms — Format's
	// output must remain re-parseable so request inputs and DB writes
	// keep working unchanged.
	orig := uuid.New()
	formatted := agsid.Format(orig)
	parsed, err := uuid.Parse(formatted)
	if err != nil {
		t.Fatalf("uuid.Parse(%q) error: %v", formatted, err)
	}
	if parsed != orig {
		t.Fatalf("round-trip mismatch: orig=%v parsed=%v", orig, parsed)
	}
}

func TestFormatPtr_Nil(t *testing.T) {
	if got := agsid.FormatPtr(nil); got != "" {
		t.Fatalf("FormatPtr(nil) = %q, want \"\"", got)
	}
}

func TestFormatPtr_NonNil(t *testing.T) {
	u := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	got := agsid.FormatPtr(&u)
	want := "550e8400e29b41d4a716446655440000"
	if got != want {
		t.Fatalf("FormatPtr = %q, want %q", got, want)
	}
}
