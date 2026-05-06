package e2e_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestGoldenM3Flow drives the M3 golden path (PRD §4.1, stop at "player
// submits survey response") through the `pth` binary against the
// in-process backend and the operator's real AGS IAM tenant. Mirrors
// M1/M2 in shape — admin login → user create → login-as → composite
// flow — but the composite is `pth flow golden-m3`, which runs ten
// RPCs end-to-end:
//
//  1. create-playtest (NDA-required, STEAM_KEYS)
//  2. transition-open
//  3. signup
//  4. accept-nda
//  5. upload-codes (one synthesised STEAM key)
//  6. approve
//  7. get-code (asserts non-empty value)
//  8. create-survey (TEXT + RATING)
//  9. submit-response
//
// 10. list-responses (asserts non-empty responses[])
//
// DM Sender stays the no-op fake in-process — the real Discord client
// lives behind DISCORD_BOT_TOKEN; e2e covers contract, not delivery.
// AGS_CAMPAIGN sub-test is operator-driven (no testcontainer for AGS,
// no testcontainer for Discord).
func TestGoldenM3Flow(t *testing.T) {
	h := getHarness(t)
	suffix := uniqueSuffix(t)
	slug := "e2e-m3-" + suffix
	adminProfile := "e2e-m3-admin-" + suffix

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
	if got := jsonString(t, loginOut, "userId"); got == "" {
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
	playerProfile := "e2e-m3-player-" + testUserID

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

	// 4. Run the composite M3 flow -------------------------------------
	flowOut := runPTH(t, h, runOpts{
		args: []string{
			"--addr", h.addr, "--insecure",
			"--namespace", h.env.AGSNamespace,
			"flow", "golden-m3", "--slug", slug,
			"--admin-profile", adminProfile,
			"--player-profile", playerProfile,
		},
	})

	steps := parseNDJSON(t, flowOut)
	wantOrder := []string{
		"create-playtest",
		"transition-open",
		"signup",
		"accept-nda",
		"upload-codes",
		"approve",
		"get-code",
		"create-survey",
		"submit-response",
		"list-responses",
	}
	if len(steps) != len(wantOrder) {
		t.Fatalf("flow emitted %d lines, want %d: %s", len(steps), len(wantOrder), flowOut)
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
	// soft-delete by id.
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

	// Belt-and-braces: the get-code step really carries a non-empty
	// STEAM key. The CLI already short-circuits on empty value, but
	// asserting here keeps the e2e contract explicit.
	gotValue := jsonNested(t, steps[6], "response", "value")
	if gotValue == "" {
		t.Fatalf("get-code response missing or empty value: %s", steps[6])
	}
	if !strings.HasPrefix(gotValue, "GOLDEN-M2-") {
		t.Fatalf("get-code value=%q does not match synthesised STEAM key prefix (line: %s)", gotValue, steps[6])
	}
	if got := jsonNested(t, steps[6], "response", "distribution_model"); got != "DISTRIBUTION_MODEL_STEAM_KEYS" {
		t.Fatalf("get-code distribution_model=%q want DISTRIBUTION_MODEL_STEAM_KEYS (line: %s)", got, steps[6])
	}

	// list-responses must carry a non-zero responses[] — the just-
	// submitted row. Mirrors the post-condition the flow itself
	// enforces, but pinned at the test layer so a future flow refactor
	// can't silently regress the terminal state.
	if n := jsonArrayLen(t, steps[9], "response", "responses"); n == 0 {
		t.Fatalf("list-responses response.responses[] is empty (expected the just-submitted row): %s", steps[9])
	}
}

// jsonArrayLen walks a nested JSON object by string keys to a terminal
// array and returns its length. Returns 0 if any segment is missing or
// the terminal value is not an array — callers compare against the
// expected lower bound rather than an exact length.
func jsonArrayLen(t *testing.T, data []byte, path ...string) int {
	t.Helper()
	cur := data
	for i, key := range path {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(cur, &raw); err != nil {
			return 0
		}
		v, ok := raw[key]
		if !ok {
			return 0
		}
		if i == len(path)-1 {
			var arr []json.RawMessage
			if err := json.Unmarshal(v, &arr); err != nil {
				return 0
			}
			return len(arr)
		}
		cur = v
	}
	return 0
}
