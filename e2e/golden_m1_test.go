package e2e_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestGoldenM1Flow drives the M1 golden path (PRD §4.1, stop at
// "applicant is PENDING") through the `pth` binary against the
// in-process backend and the operator's real AGS IAM tenant. Steps:
//
//  1. Admin logs in via `pth auth login --password` (ROPC).
//  2. Admin provisions a throwaway test user via `pth user create`.
//  3. Test user logs in as itself via `pth user login-as` (ROPC under a
//     fresh profile).
//  4. `pth flow golden-m1` runs create-playtest → transition-open →
//     signup → assert-pending across the two profiles.
//  5. Teardown: list-resolve the playtest, soft-delete it, delete the
//     test user (--yes), drop both credential profiles.
//
// Slugs and profile names carry an `e2e-<unix>-<rand>` suffix so a
// failed teardown does not poison the next run against the same
// namespace (cli.md §7.4).
func TestGoldenM1Flow(t *testing.T) {
	h := getHarness(t)
	suffix := uniqueSuffix(t)
	slug := "e2e-" + suffix
	adminProfile := "e2e-admin-" + suffix

	t.Logf("e2e harness: addr=%s suffix=%s namespace=%s", h.addr, suffix, h.env.AGSNamespace)

	// 1. Admin login ----------------------------------------------------
	loginOut := runPTH(t, h, runOpts{
		stdin: h.env.AdminPassword,
		args: []string{
			"--addr", h.addr, "--insecure",
			"--namespace", h.env.AGSNamespace,
			"--profile", adminProfile,
			"auth", "login", "--password",
			"--username", h.env.AdminUsername,
			"--password-stdin",
		},
	})
	adminUserID := jsonString(t, loginOut, "userId")
	if adminUserID == "" {
		t.Fatalf("auth login: missing userId in response: %s", loginOut)
	}

	// 2. Provision throwaway test user ---------------------------------
	createOut := runPTH(t, h, runOpts{
		args: []string{
			"--addr", h.addr, "--insecure",
			"--namespace", h.env.AGSNamespace,
			"--profile", adminProfile,
			"user", "create", "--count", "1",
		},
	})
	testUserID := jsonString(t, createOut, "userId")
	testPassword := jsonString(t, createOut, "password")
	if testUserID == "" || testPassword == "" {
		t.Fatalf("user create: missing userId or password: %s", createOut)
	}
	playerProfile := "e2e-player-" + testUserID

	// Always clean up the test user, even if subsequent steps fail.
	t.Cleanup(func() {
		_, _ = tryPTH(h, runOpts{
			args: []string{
				"--addr", h.addr, "--insecure",
				"--namespace", h.env.AGSNamespace,
				"--profile", adminProfile,
				"user", "delete", "--id", testUserID, "--yes",
			},
		})
	})

	// 3. Login-as the test user under its own profile -------------------
	loginAsOut := runPTH(t, h, runOpts{
		stdin: testPassword,
		args: []string{
			"--addr", h.addr, "--insecure",
			"--namespace", h.env.AGSNamespace,
			"--profile", playerProfile,
			"user", "login-as", "--id", testUserID, "--password-stdin",
		},
	})
	if got := jsonString(t, loginAsOut, "userId"); got != testUserID {
		t.Fatalf("user login-as userId mismatch: got %q want %q (out: %s)", got, testUserID, loginAsOut)
	}

	// 4. Run the composite flow ----------------------------------------
	flowOut := runPTH(t, h, runOpts{
		args: []string{
			"--addr", h.addr, "--insecure",
			"--namespace", h.env.AGSNamespace,
			"flow", "golden-m1", "--slug", slug,
			"--admin-profile", adminProfile,
			"--player-profile", playerProfile,
		},
	})

	steps := parseNDJSON(t, flowOut)
	wantOrder := []string{"create-playtest", "transition-open", "signup", "assert-pending"}
	if len(steps) != len(wantOrder) {
		t.Fatalf("flow emitted %d lines, want 4: %s", len(steps), flowOut)
	}
	for i, want := range wantOrder {
		if got := jsonString(t, steps[i], "step"); got != want {
			t.Fatalf("flow line %d step=%q want %q (line: %s)", i+1, got, want, steps[i])
		}
		if got := jsonString(t, steps[i], "status"); got != "OK" {
			t.Fatalf("flow line %d status=%q want OK (line: %s)", i+1, got, steps[i])
		}
	}

	// Pull the playtest id out of step 1's response so teardown can
	// soft-delete by id (the flow doesn't echo it as a top-level field).
	playtestID := jsonNested(t, steps[0], "response", "playtest", "id")
	if playtestID == "" {
		t.Fatalf("create-playtest response missing playtest.id: %s", steps[0])
	}
	t.Cleanup(func() {
		_, _ = tryPTH(h, runOpts{
			args: []string{
				"--addr", h.addr, "--insecure",
				"--namespace", h.env.AGSNamespace,
				"--profile", adminProfile,
				"playtest", "delete", "--id", playtestID,
			},
		})
	})

	// Belt-and-braces: verify the assert-pending step's response really
	// carries APPLICANT_STATUS_PENDING in the surfaced applicant block.
	if got := jsonNested(t, steps[3], "response", "applicant", "status"); got != "APPLICANT_STATUS_PENDING" {
		t.Fatalf("assert-pending applicant.status=%q want APPLICANT_STATUS_PENDING (line: %s)", got, steps[3])
	}
}

// runOpts is the shared shape for invoking pth from tests.
type runOpts struct {
	args  []string
	stdin string // sent to pth's stdin verbatim (e.g. for --password-stdin)
}

// runPTH runs pth with t.Fatal on non-zero exit. Use tryPTH in cleanup
// paths where a failure is informational only.
func runPTH(t *testing.T, h *suiteHarness, opts runOpts) []byte {
	t.Helper()
	out, err := tryPTH(h, opts)
	if err != nil {
		t.Fatalf("pth %s: %v\nstdout: %s", strings.Join(opts.args, " "), err, out)
	}
	return out
}

// tryPTH runs pth and returns stdout + a combined error. Stderr is
// surfaced inside the error for debugging; stdout is returned even on
// failure so callers can see partial JSON.
func tryPTH(h *suiteHarness, opts runOpts) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.pthBin, opts.args...)
	cmd.Env = pthEnv(h)
	if opts.stdin != "" {
		cmd.Stdin = strings.NewReader(opts.stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stdout.Bytes(), fmt.Errorf("%w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// pthEnv builds the env block every pth invocation gets: an isolated
// credentials file (so the harness never touches the operator's real
// ~/.config/playtesthub) plus the IAM bootstrap config (cli.md §7.2).
// Inherits PATH so `pth` can resolve sub-tools, but everything else is
// scrubbed to keep test runs deterministic.
func pthEnv(h *suiteHarness) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"), // some libs poke ~/.cache; harmless
		"PTH_CREDENTIALS_FILE=" + h.creds,
		"PTH_AGS_BASE_URL=" + h.env.AGSBaseURL,
		"PTH_IAM_CLIENT_ID=" + h.env.PTHIAMClientID,
	}
	if h.env.PTHIAMClientSecret != "" {
		env = append(env, "PTH_IAM_CLIENT_SECRET="+h.env.PTHIAMClientSecret)
	}
	return env
}

// parseNDJSON splits a newline-delimited JSON stream into raw lines so
// callers can probe each one independently. Empty trailing line (the
// usual artifact of fmt.Fprintf("%s\n", ...)) is dropped.
func parseNDJSON(t *testing.T, data []byte) [][]byte {
	t.Helper()
	var lines [][]byte
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		// Copy because Scanner reuses its buffer.
		out := make([]byte, len(line))
		copy(out, line)
		lines = append(lines, out)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan ndjson: %v", err)
	}
	return lines
}

// jsonString reads a top-level string field from a JSON document. Empty
// string is returned for absent fields; the test should distinguish
// "absent" from "explicitly empty" itself.
func jsonString(t *testing.T, data []byte, key string) string {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v (data: %s)", err, data)
	}
	v, ok := raw[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return ""
	}
	return s
}

// jsonNested walks a nested JSON object by string keys and returns the
// terminal string value. Returns "" if any segment is missing or not
// the expected shape — callers compare against the expected literal.
func jsonNested(t *testing.T, data []byte, path ...string) string {
	t.Helper()
	cur := data
	for i, key := range path {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(cur, &raw); err != nil {
			return ""
		}
		v, ok := raw[key]
		if !ok {
			return ""
		}
		if i == len(path)-1 {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return ""
			}
			return s
		}
		cur = v
	}
	return ""
}

// uniqueSuffix builds a per-run identifier for slugs + profile names.
// Format: <unix-seconds>-<8-hex>. The random tail keeps two runs in the
// same second from colliding.
func uniqueSuffix(t *testing.T) string {
	t.Helper()
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return fmt.Sprintf("%d-%s", time.Now().Unix(), hex.EncodeToString(buf[:]))
}
