package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"
)

// credStoreVersion is the on-disk schema version per cli.md §7.3. Bumped
// only on a breaking field change; readers that see a newer version refuse
// the file rather than silently corrupting it.
const credStoreVersion = 1

// goosWindows is the runtime.GOOS value for the Windows perm-bit carve-out
// (POSIX mode bits are not authoritative on Windows; ACLs are).
const goosWindows = "windows"

// profileEntry mirrors the cli.md §7.3 schema. addr+namespace are kept
// alongside the credential so a token from `--profile foo --addr X` is
// only returned when X matches the captured addr (the documented
// `(addr, namespace, profile)` lookup key).
type profileEntry struct {
	Addr         string    `json:"addr"`
	Namespace    string    `json:"namespace"`
	UserID       string    `json:"userId"`
	LoginMode    string    `json:"loginMode"`
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

// credentialsFile is the on-disk JSON document.
type credentialsFile struct {
	Version  int                      `json:"version"`
	Profiles map[string]*profileEntry `json:"profiles"`
}

// credStore is the file-backed credentials store. All ops re-read the
// file under a perms check; we don't cache because the CLI is short-lived
// and a stale read would silently use an old token after `pth auth login`.
type credStore struct {
	path string
}

// defaultCredStorePath returns the platform-appropriate path per cli.md
// §7.3. `XDG_CONFIG_HOME` overrides the default on Linux/macOS; Windows
// uses `%APPDATA%`.
func defaultCredStorePath() (string, error) {
	if v := os.Getenv("PTH_CREDENTIALS_FILE"); v != "" {
		return v, nil
	}
	if runtime.GOOS == goosWindows {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", fmt.Errorf("APPDATA not set")
		}
		return filepath.Join(appData, "playtesthub", "credentials.json"), nil
	}
	if cfg := os.Getenv("XDG_CONFIG_HOME"); cfg != "" {
		return filepath.Join(cfg, "playtesthub", "credentials.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home dir: %w", err)
	}
	return filepath.Join(home, ".config", "playtesthub", "credentials.json"), nil
}

func newCredStore(path string) *credStore {
	return &credStore{path: path}
}

// load reads + validates the file. A missing file returns an empty
// document so callers can write a first profile without an explicit init
// step. Refuses to read a file with looser-than-0600 perms (or a parent
// dir looser than 0700) per cli.md §7.3.
func (s *credStore) load() (*credentialsFile, error) {
	if err := s.checkPerms(); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return &credentialsFile{Version: credStoreVersion, Profiles: map[string]*profileEntry{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", s.path, err)
	}
	var doc credentialsFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", s.path, err)
	}
	if doc.Version != credStoreVersion {
		return nil, fmt.Errorf("%s: unsupported credentials version %d (want %d)", s.path, doc.Version, credStoreVersion)
	}
	if doc.Profiles == nil {
		doc.Profiles = map[string]*profileEntry{}
	}
	return &doc, nil
}

// save writes the file atomically with 0600 perms; creates the parent
// dir at 0700 if missing. Atomic write avoids a torn file on a crash
// mid-rename.
func (s *credStore) save(doc *credentialsFile) error {
	if doc == nil {
		return fmt.Errorf("save: nil document")
	}
	if doc.Version == 0 {
		doc.Version = credStoreVersion
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	if err := tightenDirPerms(dir); err != nil {
		return err
	}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling credentials: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".credentials.json.*")
	if err != nil {
		return fmt.Errorf("opening tempfile in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing %s: %w", tmpPath, err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmpPath, s.path, err)
	}
	cleanup = false
	return nil
}

// getProfile returns the profile matching `name` AND the requested
// (addr, namespace) keying. Returns (nil, nil) when no such profile
// exists — callers treat that as "no stored token" (anon dial / let the
// server return Unauthenticated).
func (s *credStore) getProfile(name, addr, namespace string) (*profileEntry, error) {
	doc, err := s.load()
	if err != nil {
		return nil, err
	}
	p, ok := doc.Profiles[name]
	if !ok {
		return nil, nil
	}
	if p.Addr != addr {
		return nil, nil
	}
	if p.Namespace != namespace {
		return nil, nil
	}
	return p, nil
}

// putProfile inserts or replaces a profile, then atomically rewrites the
// file. Caller is responsible for populating every field — partial
// updates would silently drop refresh tokens.
func (s *credStore) putProfile(name string, p *profileEntry) error {
	doc, err := s.load()
	if err != nil {
		return err
	}
	doc.Profiles[name] = p
	return s.save(doc)
}

// deleteProfile removes a profile. Missing-name is a no-op (idempotent
// logout).
func (s *credStore) deleteProfile(name string) (bool, error) {
	doc, err := s.load()
	if err != nil {
		return false, err
	}
	if _, ok := doc.Profiles[name]; !ok {
		return false, nil
	}
	delete(doc.Profiles, name)
	return true, s.save(doc)
}

// listProfiles returns every profile name in deterministic order, for
// `pth auth whoami`-style enumeration. (Not currently exposed but used
// by tests; cheap to keep.)
func (s *credStore) listProfiles() ([]string, error) {
	doc, err := s.load()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(doc.Profiles))
	for k := range doc.Profiles {
		names = append(names, k)
	}
	sort.Strings(names)
	return names, nil
}

// checkPerms enforces cli.md §7.3 perm rules. We look at the file (must
// be 0600 or stricter) and the parent dir (0700 or stricter). Anything
// looser is refused with remediation; the user can re-`chmod` and retry.
//
// Skipped on Windows because POSIX mode bits are an unreliable signal
// there — Windows ACLs do the access control and Go's FileMode is a
// best-effort emulation.
func (s *credStore) checkPerms() error {
	if runtime.GOOS == goosWindows {
		return nil
	}
	if info, err := os.Stat(s.path); err == nil {
		if mode := info.Mode().Perm(); mode&0o077 != 0 {
			return fmt.Errorf("%s: permissions %o are too loose; run: chmod 600 %s", s.path, mode, s.path)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", s.path, err)
	}
	dir := filepath.Dir(s.path)
	if info, err := os.Stat(dir); err == nil {
		if mode := info.Mode().Perm(); mode&0o077 != 0 {
			return fmt.Errorf("%s: permissions %o are too loose; run: chmod 700 %s", dir, mode, dir)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", dir, err)
	}
	return nil
}

// tightenDirPerms forces the parent dir to 0700 after MkdirAll. MkdirAll
// honours umask, so a permissive umask would leave it 0755 — callers
// would then hit the perms check on the next read.
func tightenDirPerms(dir string) error {
	if runtime.GOOS == goosWindows {
		return nil
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("chmod %s: %w", dir, err)
	}
	return nil
}
