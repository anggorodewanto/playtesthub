package adt_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/anggorodewanto/playtesthub/pkg/adt"
)

func TestMemClient_ListBuilds_HappyPath(t *testing.T) {
	c := adt.NewMemClient()
	c.RecordLinkage("studio-A", "adt-ns-1")
	older := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	c.SeedBuilds("adt-ns-1", "game-x", []adt.Build{
		{ID: "b1", Name: "Alpha", Version: "0.1", UploadedAt: older},
		{ID: "b2", Name: "Beta", Version: "0.2", UploadedAt: newer},
	})

	got, err := c.ListBuilds(context.Background(), "studio-A", "adt-ns-1", "game-x")
	if err != nil {
		t.Fatalf("ListBuilds: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ID != "b2" {
		t.Fatalf("first build ID = %q, want b2 (newest-first sort)", got[0].ID)
	}
}

func TestMemClient_ListBuilds_LinkageMissing(t *testing.T) {
	c := adt.NewMemClient()
	c.SeedBuilds("adt-ns-1", "game-x", []adt.Build{{ID: "b1"}})

	_, err := c.ListBuilds(context.Background(), "studio-A", "adt-ns-1", "game-x")
	if !errors.Is(err, adt.ErrLinkageMissing) {
		t.Fatalf("err = %v, want ErrLinkageMissing", err)
	}
	if !adt.IsLinkageMissing(err) {
		t.Fatal("IsLinkageMissing(err) = false, want true")
	}
}

func TestMemClient_ListBuilds_LinkageClearedAfterUnlink(t *testing.T) {
	c := adt.NewMemClient()
	c.RecordLinkage("studio-A", "adt-ns-1")
	c.ClearLinkage("studio-A", "adt-ns-1")

	if c.IsLinked("studio-A", "adt-ns-1") {
		t.Fatal("expected linkage cleared")
	}
	_, err := c.ListBuilds(context.Background(), "studio-A", "adt-ns-1", "game-x")
	if !errors.Is(err, adt.ErrLinkageMissing) {
		t.Fatalf("err = %v, want ErrLinkageMissing after ClearLinkage", err)
	}
}

func TestMemClient_IssueDownloadURL_PerApplicantUnique(t *testing.T) {
	c := adt.NewMemClient()
	c.RecordLinkage("studio-A", "adt-ns-1")
	params := adt.IssueDownloadURLParams{
		StudioNamespace: "studio-A",
		ADTNamespace:    "adt-ns-1",
		ADTGameID:       "game-x",
		ADTBuildID:      "b1",
		ApplicantIdent:  "user-1",
	}

	first, err := c.IssueDownloadURL(context.Background(), params)
	if err != nil {
		t.Fatalf("first IssueDownloadURL: %v", err)
	}
	if !strings.Contains(first.URL, "applicant=user-1") {
		t.Fatalf("URL missing applicant ident: %q", first.URL)
	}

	params.ApplicantIdent = "user-2"
	second, err := c.IssueDownloadURL(context.Background(), params)
	if err != nil {
		t.Fatalf("second IssueDownloadURL: %v", err)
	}
	if first.URL == second.URL {
		t.Fatal("expected per-applicant URL uniqueness")
	}

	log := c.IssuedURLs()
	if len(log) != 2 {
		t.Fatalf("issued log len = %d, want 2", len(log))
	}
	if log[0].ApplicantIdent != "user-1" || log[1].ApplicantIdent != "user-2" {
		t.Fatalf("issued log order wrong: %+v", log)
	}
}

func TestMemClient_IssueDownloadURL_TTL(t *testing.T) {
	c := adt.NewMemClient()
	c.URLTTL = 30 * time.Minute
	c.RecordLinkage("studio-A", "adt-ns-1")

	got, err := c.IssueDownloadURL(context.Background(), adt.IssueDownloadURLParams{
		StudioNamespace: "studio-A",
		ADTNamespace:    "adt-ns-1",
		ADTGameID:       "g",
		ADTBuildID:      "b",
		ApplicantIdent:  "u",
	})
	if err != nil {
		t.Fatalf("IssueDownloadURL: %v", err)
	}
	if got.ExpiresAt.IsZero() {
		t.Fatal("expected non-zero ExpiresAt when URLTTL is set")
	}
}

func TestMemClient_IssueDownloadURL_LinkageMissing(t *testing.T) {
	c := adt.NewMemClient()

	_, err := c.IssueDownloadURL(context.Background(), adt.IssueDownloadURLParams{
		StudioNamespace: "studio-A",
		ADTNamespace:    "adt-ns-1",
		ADTGameID:       "g",
		ADTBuildID:      "b",
		ApplicantIdent:  "u",
	})
	if !errors.Is(err, adt.ErrLinkageMissing) {
		t.Fatalf("err = %v, want ErrLinkageMissing", err)
	}
}

func TestMemClient_InjectedFailures(t *testing.T) {
	c := adt.NewMemClient()
	c.RecordLinkage("studio-A", "adt-ns-1")
	boom := errors.New("boom")
	c.ListBuildsErr = []error{boom}
	c.IssueDownloadURLErr = []error{boom}

	if _, err := c.ListBuilds(context.Background(), "studio-A", "adt-ns-1", "game-x"); !errors.Is(err, boom) {
		t.Fatalf("ListBuilds err = %v, want boom", err)
	}
	if _, err := c.ListBuilds(context.Background(), "studio-A", "adt-ns-1", "game-x"); err != nil {
		t.Fatalf("ListBuilds after slot consumed: %v", err)
	}
	if _, err := c.IssueDownloadURL(context.Background(), adt.IssueDownloadURLParams{
		StudioNamespace: "studio-A",
		ADTNamespace:    "adt-ns-1",
	}); !errors.Is(err, boom) {
		t.Fatalf("IssueDownloadURL err = %v, want boom", err)
	}
}
