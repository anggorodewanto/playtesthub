package e2e_test

import (
	"encoding/json"
	"strings"
	"testing"
)

const distributionModelSteamKeys = "DISTRIBUTION_MODEL_STEAM_KEYS"

// TestGoldenM2Flow drives the M2 golden path (PRD §4.1, stop at "player
// sees code in UI") through the `pth` binary against the in-process
// backend and the operator's real AGS IAM tenant. Steps mirror M1's
// shape — admin login → user create → login-as → composite flow → cleanup
// — but the composite flow is `pth flow golden-m2`, which runs seven
// RPCs end-to-end:
//
//  1. create-playtest (NDA-required, STEAM_KEYS)
//  2. transition-open
//  3. signup
//  4. accept-nda
//  5. upload-codes (one synthesised STEAM key)
//  6. approve
//  7. get-code (asserts non-empty value)
//
// The STEAM_KEYS path is the M2 e2e gate per phase 13: it works without
// AGS Platform creds, so CI runs the same suite the operator runs
// locally. The AGS_CAMPAIGN sub-test would require live AGS namespace
// creds and is deferred to phase 8.1's SDK-backed adapter work.
func TestGoldenM2Flow(t *testing.T) {
	h := getHarness(t)
	suffix := uniqueSuffix(t)
	slug := "e2e-m2-" + suffix
	adminProfile := "e2e-m2-admin-" + suffix

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
	playerProfile := "e2e-m2-player-" + testUserID

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

	// 3. Login-as the test user under its own profile.
	//
	// `pth user login-as` reads the admin bearer from the same --profile
	// it later writes the player token to, so we seed an admin login on
	// playerProfile first; login-as overwrites it with the player ROPC
	// token in the next step. adminProfile stays admin-only so the
	// flow's admin steps still authenticate.
	runPTH(t, h, runOpts{
		stdin: h.env.AdminPassword,
		args: []string{
			"--addr", h.addr, "--insecure",
			"--namespace", h.env.AGSNamespace,
			"--profile", playerProfile,
			"auth", "login", "--password",
			"--username", h.env.AdminUsername,
			"--password-stdin",
		},
	})
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

	// 4. Run the composite M2 flow -------------------------------------
	flowOut := runPTH(t, h, runOpts{
		args: []string{
			"--addr", h.addr, "--insecure",
			"--namespace", h.env.AGSNamespace,
			"flow", "golden-m2", "--slug", slug,
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

	// Belt-and-braces: the get-code step's response really carries a
	// non-empty STEAM key. The CLI already short-circuits on an empty
	// value, but asserting here keeps the e2e contract explicit so a
	// future flow refactor can't silently regress the terminal state.
	gotValue := jsonNested(t, steps[6], "response", "value")
	if gotValue == "" {
		t.Fatalf("get-code response missing or empty value: %s", steps[6])
	}
	if !strings.HasPrefix(gotValue, "GOLDEN-M2-") {
		t.Fatalf("get-code value=%q does not match synthesised STEAM key prefix (line: %s)", gotValue, steps[6])
	}
	if got := jsonNested(t, steps[6], "response", "distribution_model"); got != distributionModelSteamKeys {
		t.Fatalf("get-code distribution_model=%q want DISTRIBUTION_MODEL_STEAM_KEYS (line: %s)", got, steps[6])
	}
}

// TestGoldenM2_AutoApprove drives the same M2 STEAM_KEYS golden path with
// `--auto-approve --auto-approve-limit 5` (M5.A phase 6). The flow re-orders
// the seven steps so upload-codes runs before signup — the auto-approve
// chain inside the Signup handler consumes from the pool inside the same tx
// — and replaces the manual `approve` step with
// `assert-applicant-auto-approved`, which calls ListApplicants and asserts
// the just-signed-up row landed APPROVED with auto_approved=true.
//
// Beyond the flow's own assertions we belt-and-braces three things from the
// outside:
//
//  1. `get-code` still returns a non-empty granted code without a manual
//     approve having been issued (the auto-approve chain reused M2's
//     reserve→fenced-finalize path inside the signup tx).
//  2. ListAuditLog with action_filter=applicant.auto_approved returns
//     exactly one row for the playtest — proves the audit emission is
//     idempotent + scoped (one signup, one auto-approve, one audit row).
//  3. The applicant.approve audit row is **absent** — proves the
//     auto-approve path is a distinct action, not double-attributed.
func TestGoldenM2_AutoApprove(t *testing.T) {
	h := getHarness(t)
	suffix := uniqueSuffix(t)
	slug := "e2e-m2-aa-" + suffix
	adminProfile := "e2e-m2-aa-admin-" + suffix

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
	playerProfile := "e2e-m2-aa-player-" + testUserID

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

	// 3. Login-as the test user under its own profile.
	runPTH(t, h, runOpts{
		stdin: h.env.AdminPassword,
		args: []string{
			"--addr", h.addr, "--insecure",
			"--namespace", h.env.AGSNamespace,
			"--profile", playerProfile,
			"auth", "login", "--password",
			"--username", h.env.AdminUsername,
			"--password-stdin",
		},
	})
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

	// 4. Run the auto-approve M2 flow ----------------------------------
	flowOut := runPTH(t, h, runOpts{
		args: []string{
			"--addr", h.addr, "--insecure",
			"--namespace", h.env.AGSNamespace,
			"flow", "golden-m2", "--slug", slug,
			"--admin-profile", adminProfile,
			"--player-profile", playerProfile,
			"--auto-approve", "--auto-approve-limit", "5",
		},
	})

	steps := parseNDJSON(t, flowOut)
	wantOrder := []string{
		"create-playtest",
		"transition-open",
		"upload-codes",
		"signup",
		"accept-nda",
		"assert-applicant-auto-approved",
		"get-code",
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
	// soft-delete by id, and the audit-log assertion below can scope its
	// query.
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

	// 5. get-code carried a real STEAM key — proves the auto-approve
	// chain ran reserve→fenced-finalize inside the signup tx without any
	// manual approve having been issued.
	gotValue := jsonNested(t, steps[6], "response", "value")
	if gotValue == "" {
		t.Fatalf("get-code response missing or empty value: %s", steps[6])
	}
	if !strings.HasPrefix(gotValue, "GOLDEN-M2-") {
		t.Fatalf("get-code value=%q does not match synthesised STEAM key prefix (line: %s)", gotValue, steps[6])
	}
	if got := jsonNested(t, steps[6], "response", "distribution_model"); got != distributionModelSteamKeys {
		t.Fatalf("get-code distribution_model=%q want DISTRIBUTION_MODEL_STEAM_KEYS (line: %s)", got, steps[6])
	}

	// 6. Audit log: exactly one applicant.auto_approved row, and zero
	// applicant.approve rows (auto-approve is a distinct action).
	auditAutoOut := runPTH(t, h, runOpts{
		args: []string{
			"--addr", h.addr, "--insecure",
			"--namespace", h.env.AGSNamespace,
			"--profile", adminProfile,
			"audit", "list",
			"--playtest", playtestID,
			"--action", "applicant.auto_approved",
		},
	})
	if got := countAuditEntries(t, auditAutoOut); got != 1 {
		t.Fatalf("applicant.auto_approved audit rows=%d, want 1: %s", got, auditAutoOut)
	}

	auditApproveOut := runPTH(t, h, runOpts{
		args: []string{
			"--addr", h.addr, "--insecure",
			"--namespace", h.env.AGSNamespace,
			"--profile", adminProfile,
			"audit", "list",
			"--playtest", playtestID,
			"--action", "applicant.approve",
		},
	})
	if got := countAuditEntries(t, auditApproveOut); got != 0 {
		t.Fatalf("applicant.approve audit rows=%d, want 0 (auto-approve must not double-attribute): %s", got, auditApproveOut)
	}
}

// countAuditEntries unmarshals a ListAuditLog response line and returns
// the length of the `entries` array. Returns -1 if the document doesn't
// parse — callers compare against the expected count.
func countAuditEntries(t *testing.T, data []byte) int {
	t.Helper()
	var resp struct {
		Entries []json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal audit list response: %v (data: %s)", err, data)
	}
	return len(resp.Entries)
}
