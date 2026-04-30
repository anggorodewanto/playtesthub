package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func newTestCredStore(t *testing.T) (*credStore, string) {
	t.Helper()
	dir := t.TempDir()
	// MkdirAll inside save() will create the playtesthub subdir; the
	// outer t.TempDir is already 0700.
	return newCredStore(filepath.Join(dir, "playtesthub", "credentials.json")), dir
}

func TestCredStoreLoadMissingFileReturnsEmpty(t *testing.T) {
	s, _ := newTestCredStore(t)
	doc, err := s.load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if doc.Version != credStoreVersion {
		t.Errorf("Version=%d want %d", doc.Version, credStoreVersion)
	}
	if len(doc.Profiles) != 0 {
		t.Errorf("Profiles=%v want empty", doc.Profiles)
	}
}

func TestCredStoreSaveRoundTrip(t *testing.T) {
	s, _ := newTestCredStore(t)
	want := &profileEntry{
		Addr:         "localhost:6565",
		Namespace:    "playtest-ns",
		UserID:       "u1",
		LoginMode:    "password",
		AccessToken:  "access-abc",
		RefreshToken: "refresh-xyz",
		ExpiresAt:    time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
	if err := s.putProfile("default", want); err != nil {
		t.Fatalf("putProfile: %v", err)
	}
	got, err := s.getProfile("default", "localhost:6565", "playtest-ns")
	if err != nil {
		t.Fatalf("getProfile: %v", err)
	}
	if got == nil {
		t.Fatal("got nil profile")
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken || got.UserID != want.UserID || !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestCredStoreGetProfileMismatchedAddrReturnsNil(t *testing.T) {
	s, _ := newTestCredStore(t)
	if err := s.putProfile("default", &profileEntry{Addr: "a:1", Namespace: "ns", AccessToken: "tok"}); err != nil {
		t.Fatalf("putProfile: %v", err)
	}
	got, err := s.getProfile("default", "b:2", "ns")
	if err != nil {
		t.Fatalf("getProfile: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for mismatched addr; got %+v", got)
	}
	got, err = s.getProfile("default", "a:1", "other-ns")
	if err != nil {
		t.Fatalf("getProfile: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for mismatched namespace; got %+v", got)
	}
}

func TestCredStoreDeleteProfile(t *testing.T) {
	s, _ := newTestCredStore(t)
	if err := s.putProfile("default", &profileEntry{Addr: "a:1", Namespace: "ns", AccessToken: "tok"}); err != nil {
		t.Fatalf("putProfile: %v", err)
	}
	removed, err := s.deleteProfile("default")
	if err != nil {
		t.Fatalf("deleteProfile: %v", err)
	}
	if !removed {
		t.Error("removed=false, want true")
	}
	removed, err = s.deleteProfile("default")
	if err != nil {
		t.Fatalf("deleteProfile (second): %v", err)
	}
	if removed {
		t.Error("removed=true on missing profile")
	}
}

func TestCredStoreListProfilesIsSorted(t *testing.T) {
	s, _ := newTestCredStore(t)
	for _, name := range []string{"zeta", "alpha", "mu"} {
		if err := s.putProfile(name, &profileEntry{Addr: "a:1", Namespace: "ns", AccessToken: "t"}); err != nil {
			t.Fatalf("putProfile %s: %v", name, err)
		}
	}
	got, err := s.listProfiles()
	if err != nil {
		t.Fatalf("listProfiles: %v", err)
	}
	want := []string{"alpha", "mu", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d]=%s want %s", i, got[i], want[i])
		}
	}
}

func TestCredStoreSaveSetsTightFilePerms(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("perm bits are best-effort on windows")
	}
	s, _ := newTestCredStore(t)
	if err := s.putProfile("default", &profileEntry{Addr: "a:1", Namespace: "ns", AccessToken: "t"}); err != nil {
		t.Fatalf("putProfile: %v", err)
	}
	info, err := os.Stat(s.path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("file mode %o, want 0600", mode)
	}
	dirInfo, err := os.Stat(filepath.Dir(s.path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if mode := dirInfo.Mode().Perm(); mode != 0o700 {
		t.Errorf("dir mode %o, want 0700", mode)
	}
}

func TestCredStoreCheckPermsRefusesLooseFile(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("perm bits are best-effort on windows")
	}
	s, _ := newTestCredStore(t)
	if err := s.putProfile("default", &profileEntry{Addr: "a:1", Namespace: "ns", AccessToken: "t"}); err != nil {
		t.Fatalf("putProfile: %v", err)
	}
	if err := os.Chmod(s.path, 0o644); err != nil {
		t.Fatalf("chmod loose: %v", err)
	}
	_, err := s.load()
	if err == nil {
		t.Fatal("load: want perms error, got nil")
	}
	if !strings.Contains(err.Error(), "too loose") || !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("error missing remediation: %v", err)
	}
}

func TestCredStoreCheckPermsRefusesLooseDir(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("perm bits are best-effort on windows")
	}
	s, _ := newTestCredStore(t)
	if err := s.putProfile("default", &profileEntry{Addr: "a:1", Namespace: "ns", AccessToken: "t"}); err != nil {
		t.Fatalf("putProfile: %v", err)
	}
	if err := os.Chmod(filepath.Dir(s.path), 0o755); err != nil {
		t.Fatalf("chmod loose dir: %v", err)
	}
	_, err := s.load()
	if err == nil {
		t.Fatal("load: want dir-perms error, got nil")
	}
	if !strings.Contains(err.Error(), "chmod 700") {
		t.Errorf("error missing remediation: %v", err)
	}
}

func TestCredStoreLoadRejectsUnknownVersion(t *testing.T) {
	s, _ := newTestCredStore(t)
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body, _ := json.Marshal(map[string]any{"version": 99, "profiles": map[string]any{}})
	if err := os.WriteFile(s.path, body, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := s.load()
	if err == nil {
		t.Fatal("want unsupported-version error, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported credentials version") {
		t.Errorf("err = %v", err)
	}
}
