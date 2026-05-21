package main

// catalogueSchema is the stable schema name in `pth describe` output and
// the checked-in golden file. cli.md §5 + §10 freeze it as `cli-schema.v1`;
// AI consumers can branch on it. Bump only on a breaking shape change.
const catalogueSchema = "cli-schema.v1"

// flagSpec describes one flag for a subcommand. ValueType is informational
// — agents can use it to coerce stringly-typed inputs without re-reading
// the prose. Names include the leading `--`.
type flagSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ValueType   string `json:"valueType"`
}

// commandSpec is one entry in the catalogue. Name is the full invocation
// (group + action, e.g. "playtest get-public"). Milestone follows cli.md
// §6 grouping. Example is a copy-pasteable invocation that demonstrates
// the happy path; it is *not* executed — agents should treat it as a
// template they fill in with their own values.
type commandSpec struct {
	Name          string     `json:"name"`
	Milestone     string     `json:"milestone"`
	Description   string     `json:"description"`
	RequiredFlags []flagSpec `json:"requiredFlags"`
	OptionalFlags []flagSpec `json:"optionalFlags"`
	Example       string     `json:"example"`
}

// catalogue is the deterministic list of M1 subcommands. Entries stay
// alphabetised by Name so the golden file has a stable layout. Adding a
// flag → edit the entry; renaming a flag → commit the same change to both
// the flagset and this registry, then regenerate the golden file (see
// cmd/pth/testdata/describe.golden.json).
//
// Why hand-rolled rather than reflected from the flagsets: the flag
// descriptions live next to their declarations and are tuned for `--help`
// readers; the AI-facing description is intentionally tighter and refers
// to PRD/cli.md sections. Drift is caught by the diff-check on
// describe.golden.json (cli.md §9, STATUS.md M1 phase 10.6).
var catalogue = []commandSpec{
	{
		Name:          "adt build list",
		Milestone:     "M5.B",
		Description:   "Admin: list ADT builds under a linkage's game (cli.md §6.5, PRD §4.8).",
		RequiredFlags: []flagSpec{{Name: "--linkage-id", Description: "adt_linkage_id (from `adt linkage list`)", ValueType: "uuid"}, {Name: "--game-id", Description: "ADT-side game id", ValueType: "string"}},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin adt build list --linkage-id 01J0... --game-id mygame",
	},
	{
		Name:          "adt diagnostics",
		Milestone:     "M5.B",
		Description:   "Admin: report which adt.Client kind was wired at boot + presence of the env vars that fed the gate (PRD §4.8 / 2026-05-21 silent-fallback recovery).",
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin adt diagnostics",
	},
	{
		Name:          "adt games list",
		Milestone:     "M5.B",
		Description:   "Admin: list ADT games under a linkage's namespace (cli.md §6.5, PRD §4.8; STATUS_M5.md B12).",
		RequiredFlags: []flagSpec{{Name: "--linkage-id", Description: "adt_linkage_id (from `adt linkage list`)", ValueType: "uuid"}},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin adt games list --linkage-id 01J0...",
	},
	{
		Name:        "adt linkage complete",
		Milestone:   "M5.B",
		Description: "Admin: finalize an ADT link by consuming the state nonce + adt_namespace echoed on the callback URL (PRD §4.8.2).",
		RequiredFlags: []flagSpec{
			{Name: "--state", Description: "linking state nonce returned from `adt linkage start`", ValueType: "string"},
			{Name: "--adt-namespace", Description: "ADT namespace echoed by the redirect-back URL", ValueType: "string"},
		},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin adt linkage complete --state EXAMPLE --adt-namespace adt-ns",
	},
	{
		Name:          "adt linkage list",
		Milestone:     "M5.B",
		Description:   "Admin: list every ADT linkage scoped to the caller's studio (cli.md §6.5, PRD §4.8).",
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin adt linkage list",
	},
	{
		Name:          "adt linkage recover",
		Milestone:     "M5.B",
		Description:   "Admin: adopt an orphan ADT-side linkage (flag present on ADT, no local row) without an OAuth round-trip (PRD §4.8).",
		RequiredFlags: []flagSpec{{Name: "--adt-namespace", Description: "ADT namespace whose orphan flag to adopt", ValueType: "string"}},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin adt linkage recover --adt-namespace adt-ns",
	},
	{
		Name:          "adt linkage start",
		Milestone:     "M5.B",
		Description:   "Admin: mint a linkUrl + state nonce for the ADT linking redirect (PRD §4.8.2).",
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin adt linkage start",
	},
	{
		Name:          "adt linkage unlink",
		Milestone:     "M5.B",
		Description:   "Admin: soft-delete an ADT linkage row + best-effort DELETE on the ADT side (PRD §4.8).",
		RequiredFlags: []flagSpec{idFlag()},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin adt linkage unlink --id 01J0...",
	},
	{
		Name:        "announcement create",
		Milestone:   "M5.C",
		Description: "Admin: send a bulk Discord DM broadcast (PRD §5.4 \"Bulk announcements\"). Recipients resolve at call time per the --send-to filter.",
		RequiredFlags: []flagSpec{
			{Name: "--playtest-id", Description: "playtest UUID", ValueType: "uuid"},
			{Name: "--subject", Description: "broadcast subject (1-200 chars; rejected with byte-exact errors.md string outside that range)", ValueType: "string"},
			{Name: "--message", Description: "broadcast message body (1-4000 chars)", ValueType: "string"},
		},
		OptionalFlags: []flagSpec{
			{Name: "--send-to", Description: "recipient filter: ALL|APPROVED_ONLY|PENDING_ONLY (default APPROVED_ONLY)", ValueType: "string"},
			dryRunFlag(),
		},
		Example: "pth --namespace mygame --profile admin announcement create --playtest-id 01J0... --send-to APPROVED_ONLY --subject \"Build update\" --message \"New patch live\"",
	},
	{
		Name:          "announcement list",
		Milestone:     "M5.C",
		Description:   "Admin: list announcement history for a playtest (PRD §5.4 \"Bulk announcements\").",
		RequiredFlags: []flagSpec{{Name: "--playtest-id", Description: "playtest UUID", ValueType: "uuid"}},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin announcement list --playtest-id 01J0...",
	},
	{
		Name:          "applicant accept-nda",
		Milestone:     "M2",
		Description:   "Persist a versioned NDA acceptance for the calling player (cli.md §6.2).",
		RequiredFlags: []flagSpec{playtestFlag()},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --profile player applicant accept-nda --playtest 01J0...",
	},
	{
		Name:          "applicant approve",
		Milestone:     "M2",
		Description:   "Admin: approve an applicant; reserves + grants a code (cli.md §6.2, PRD §4.1).",
		RequiredFlags: []flagSpec{idFlag()},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin applicant approve --id 01J0...",
	},
	{
		Name:          "applicant get-code",
		Milestone:     "M2",
		Description:   "Fetch the granted code for the calling player. Player token required (cli.md §6.2).",
		RequiredFlags: []flagSpec{playtestFlag()},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --profile player applicant get-code --playtest 01J0...",
	},
	{
		Name:          "applicant list",
		Milestone:     "M2",
		Description:   "Admin: list applicants on a playtest. Server-side status + dm-failed filters (cli.md §6.2).",
		RequiredFlags: []flagSpec{playtestFlag()},
		OptionalFlags: []flagSpec{
			{Name: "--status", Description: "applicant status filter: PENDING | APPROVED | REJECTED", ValueType: "enum"},
			{Name: "--dm-failed", Description: "only rows where last_dm_status='failed'", ValueType: "bool"},
			{Name: "--cursor", Description: "opaque page_token from a prior response", ValueType: "string"},
			{Name: "--page-size", Description: "page size (0 → server default 50)", ValueType: "int"},
			dryRunFlag(),
		},
		Example: "pth --namespace mygame --profile admin applicant list --playtest 01J0... --status PENDING",
	},
	{
		Name:          "applicant reject",
		Milestone:     "M2",
		Description:   "Admin: reject an applicant (cli.md §6.2). Optional --reason ≤500 chars.",
		RequiredFlags: []flagSpec{idFlag()},
		OptionalFlags: []flagSpec{
			{Name: "--reason", Description: "admin-visible rejection reason (≤500 chars)", ValueType: "string"},
			dryRunFlag(),
		},
		Example: "pth --namespace mygame --profile admin applicant reject --id 01J0... --reason 'duplicate signup'",
	},
	{
		Name:          "applicant retry-dm",
		Milestone:     "M2",
		Description:   "Admin: re-enqueue the Discord DM for an applicant (cli.md §6.2).",
		RequiredFlags: []flagSpec{idFlag()},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin applicant retry-dm --id 01J0...",
	},
	{
		Name:          "applicant retry-failed-dms",
		Milestone:     "M3",
		Description:   "Admin: bulk-enqueue every applicant whose last DM is in the failed state for a playtest (cli.md §6.3, PRD §5.5).",
		RequiredFlags: []flagSpec{playtestFlag()},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin applicant retry-failed-dms --playtest 01J0...",
	},
	{
		Name:          "applicant signup",
		Milestone:     "M1",
		Description:   "Sign up the calling player to a playtest. Requires a player token (cli.md §6.1).",
		RequiredFlags: []flagSpec{slugFlag(), platformsFlag("comma-separated platforms owned (at least one of STEAM,XBOX,PLAYSTATION,EPIC,OTHER)")},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --profile player applicant signup --slug summer-stress-test --platforms STEAM,XBOX",
	},
	{
		Name:          "applicant status",
		Milestone:     "M1",
		Description:   "Show the calling player's applicant row for a playtest (cli.md §6.1).",
		RequiredFlags: []flagSpec{slugFlag()},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --profile player applicant status --slug summer-stress-test",
	},
	{
		Name:          "audit list",
		Milestone:     "M3",
		Description:   "Admin: list audit log rows for a playtest. Cursor pagination on (createdAt, id) DESC; --actor='system' filters system-emitted rows (cli.md §6.3, PRD §4.7).",
		RequiredFlags: []flagSpec{playtestFlag()},
		OptionalFlags: []flagSpec{
			{Name: "--actor", Description: "actor filter: 'system' (rows where actor_user_id IS NULL) or a UUID", ValueType: "string"},
			{Name: "--action", Description: "action filter: exact match on the action string (schema.md audit-action catalogue)", ValueType: "string"},
			{Name: "--cursor", Description: "opaque page_token from a prior response", ValueType: "string"},
			{Name: "--page-size", Description: "page size (0 → server default 50)", ValueType: "int"},
			dryRunFlag(),
		},
		Example: "pth --namespace mygame --profile admin audit list --playtest 01J0... --actor system",
	},
	{
		Name:        "auth login --discord",
		Milestone:   "M1",
		Description: "Discord-federated login. Browser → loopback → backend ExchangeDiscordCode (cli.md §7.1).",
		OptionalFlags: []flagSpec{
			{Name: "--manual", Description: "skip the loopback listener; prompt for the pasted redirect URL", ValueType: "bool"},
			{Name: "--no-browser", Description: "do not auto-open the authorize URL", ValueType: "bool"},
			{Name: "--dry-run", Description: "print authorize/exchange URLs and exit", ValueType: "bool"},
		},
		Example: "pth --namespace mygame auth login --discord",
	},
	{
		Name:        "auth login --password",
		Milestone:   "M1",
		Description: "AGS IAM ROPC grant for native AGS users. Stores the token under --profile (cli.md §7.2).",
		RequiredFlags: []flagSpec{
			{Name: "--username", Description: "AGS username", ValueType: "string"},
		},
		OptionalFlags: []flagSpec{
			{Name: "--password-stdin", Description: "read password from one line on stdin instead of TTY prompt", ValueType: "bool"},
		},
		Example: "pth --namespace mygame --profile admin auth login --password --username admin@example.com --password-stdin <pw.txt",
	},
	{
		Name:        "auth logout",
		Milestone:   "M1",
		Description: "Remove the stored credential for --profile (cli.md §6.1).",
		Example:     "pth --profile admin auth logout",
	},
	{
		Name:        "auth token",
		Milestone:   "M1",
		Description: "Print the active bearer token to stdout for piping into curl/grpcurl (cli.md §6.1).",
		Example:     "pth --profile admin auth token",
	},
	{
		Name:        "auth whoami",
		Milestone:   "M1",
		Description: "Print {profile, userId, namespace, addr, expiresAt} for the active token. Non-zero exit if missing/expired (cli.md §6.1).",
		Example:     "pth --profile admin auth whoami",
	},
	{
		Name:          "code pool",
		Milestone:     "M2",
		Description:   "Admin: show CodePoolStats + raw code values for a playtest (cli.md §6.2, PRD §5.7).",
		RequiredFlags: []flagSpec{playtestFlag()},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin code pool --playtest 01J0...",
	},
	{
		Name:          "code sync-from-ags",
		Milestone:     "M2",
		Description:   "Admin: re-sync the local code pool from AGS Campaign API. AGS_CAMPAIGN only (cli.md §6.2).",
		RequiredFlags: []flagSpec{playtestFlag()},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin code sync-from-ags --playtest 01J0...",
	},
	{
		Name:        "code top-up",
		Milestone:   "M2",
		Description: "Admin: generate more codes via AGS Campaign API. AGS_CAMPAIGN only (cli.md §6.2, PRD §4.6).",
		RequiredFlags: []flagSpec{
			playtestFlag(),
			{Name: "--quantity", Description: "number of codes to generate (1..50000)", ValueType: "int"},
		},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin code top-up --playtest 01J0... --quantity 100",
	},
	{
		Name:        "code upload",
		Milestone:   "M2",
		Description: "Admin: upload a CSV of codes (STEAM_KEYS only). Whole-file reject on any violation (cli.md §6.2, PRD §4.3).",
		RequiredFlags: []flagSpec{
			playtestFlag(),
			{Name: "--file", Description: "path to CSV file ('-' reads stdin)", ValueType: "string"},
		},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin code upload --playtest 01J0... --file ./keys.csv",
	},
	{
		Name:        "describe",
		Milestone:   "M1",
		Description: "Emit the JSON catalogue of every subcommand. Stable schema cli-schema.v1 (cli.md §5, §10).",
		Example:     "pth describe",
	},
	{
		Name:        "doctor",
		Milestone:   "M1",
		Description: "Probe the backend with an unauth GetPublicPlaytest sentinel call. Reports gRPC code + round-trip latency (cli.md §5).",
		Example:     "pth --addr localhost:6565 doctor",
	},
	{
		Name:        "flow golden-m1",
		Milestone:   "M1",
		Description: "Composite: create-playtest → transition OPEN → signup (synthetic player) → assert PENDING. NDJSON output (cli.md §6.4, §8).",
		RequiredFlags: []flagSpec{
			slugFlag(),
			{Name: "--admin-profile", Description: "credential profile for admin steps (create, transition)", ValueType: "string"},
			{Name: "--player-profile", Description: "credential profile for player steps (signup, status)", ValueType: "string"},
		},
		OptionalFlags: []flagSpec{
			{Name: "--title", Description: "playtest title (default: 'Playtest <slug>')", ValueType: "string"},
			platformsFlag("platforms for both create and signup (default STEAM)"),
			{Name: "--dry-run", Description: "print every step's request JSON and exit without dialling", ValueType: "bool"},
		},
		Example: "pth --namespace mygame flow golden-m1 --slug e2e-1234 --admin-profile admin --player-profile player",
	},
	{
		Name:        "flow golden-m2",
		Milestone:   "M2",
		Description: "Composite: golden-m1 → accept-nda → upload codes → approve → assert APPROVED + code visible. NDJSON output (cli.md §6.4). With --auto-approve, upload-codes hoists before signup and the manual approve step is replaced by assert-applicant-auto-approved (M5.A).",
		RequiredFlags: []flagSpec{
			slugFlag(),
			{Name: "--admin-profile", Description: "credential profile for admin steps (create, transition, upload, approve)", ValueType: "string"},
			{Name: "--player-profile", Description: "credential profile for player steps (signup, accept-nda, get-code)", ValueType: "string"},
		},
		OptionalFlags: []flagSpec{
			{Name: "--title", Description: "playtest title (default: 'Playtest <slug>')", ValueType: "string"},
			platformsFlag("platforms for both create and signup (default STEAM)"),
			{Name: "--nda-text", Description: "NDA prose; @file to load from disk", ValueType: "string"},
			{Name: "--codes-file", Description: "CSV path to upload (overrides --codes-count; '-' reads stdin)", ValueType: "string"},
			{Name: "--codes-count", Description: "number of synthetic codes when --codes-file is empty (1..50, default 1)", ValueType: "int"},
			{Name: "--auto-approve", Description: "create the playtest with auto-approve enabled; rewrites the signup tail to assert-applicant-auto-approved (PRD §5.4 / M5.A)", ValueType: "bool"},
			{Name: "--auto-approve-limit", Description: "auto-approve cap (1..100,000; required when --auto-approve)", ValueType: "int"},
			{Name: "--dry-run", Description: "print every step's request JSON and exit without dialling", ValueType: "bool"},
		},
		Example: "pth --namespace mygame flow golden-m2 --slug e2e-1234 --admin-profile admin --player-profile player",
	},
	{
		Name:        "flow golden-m3",
		Milestone:   "M3",
		Description: "Composite: golden-m2 → create-survey → submit-response → list-responses. Ten NDJSON lines, stop-on-first-failure (cli.md §6.4, STATUS M3 phase 12). Inherits the golden-m2 --auto-approve variant (M5.A).",
		RequiredFlags: []flagSpec{
			slugFlag(),
			{Name: "--admin-profile", Description: "credential profile for admin steps (create, transition, upload, approve, create-survey, list-responses)", ValueType: "string"},
			{Name: "--player-profile", Description: "credential profile for player steps (signup, accept-nda, get-code, submit-response)", ValueType: "string"},
		},
		OptionalFlags: []flagSpec{
			{Name: "--title", Description: "playtest title (default: 'Playtest <slug>')", ValueType: "string"},
			platformsFlag("platforms for both create and signup (default STEAM)"),
			{Name: "--nda-text", Description: "NDA prose; @file to load from disk", ValueType: "string"},
			{Name: "--codes-file", Description: "CSV path to upload (overrides --codes-count; '-' reads stdin)", ValueType: "string"},
			{Name: "--codes-count", Description: "number of synthetic codes when --codes-file is empty (1..50, default 1)", ValueType: "int"},
			{Name: "--auto-approve", Description: "create the playtest with auto-approve enabled; rewrites the M2 prefix to assert-applicant-auto-approved (PRD §5.4 / M5.A)", ValueType: "bool"},
			{Name: "--auto-approve-limit", Description: "auto-approve cap (1..100,000; required when --auto-approve)", ValueType: "int"},
			{Name: "--dry-run", Description: "print every step's request JSON and exit without dialling", ValueType: "bool"},
		},
		Example: "pth --namespace mygame flow golden-m3 --slug e2e-m3 --admin-profile admin --player-profile player",
	},
	{
		Name:        "flow golden-m4",
		Milestone:   "M4",
		Description: "Composite: create playtest (DRAFT + startsAt/endsAt set) → await auto-open → await auto-close → assert ≥2 system playtest.status_transition audit rows. Four NDJSON lines, stop-on-first-failure (cli.md §6.4, STATUS_M4 phase 8).",
		RequiredFlags: []flagSpec{
			slugFlag(),
			{Name: "--admin-profile", Description: "credential profile for admin steps (create, schedule-info, audit list)", ValueType: "string"},
		},
		OptionalFlags: []flagSpec{
			{Name: "--title", Description: "playtest title (default: 'Playtest <slug>')", ValueType: "string"},
			{Name: "--start-offset", Description: "how far in the future to set starts_at (default 2s)", ValueType: "duration"},
			{Name: "--end-offset", Description: "how far in the future to set ends_at (default 4s)", ValueType: "duration"},
			{Name: "--poll-interval", Description: "schedule-info poll cadence (default 250ms)", ValueType: "duration"},
			{Name: "--poll-timeout-open", Description: "max wait for DRAFT→OPEN (default 15s)", ValueType: "duration"},
			{Name: "--poll-timeout-close", Description: "max wait for OPEN→CLOSED (default 15s)", ValueType: "duration"},
			{Name: "--dry-run", Description: "print every step's request JSON and exit without dialling", ValueType: "bool"},
		},
		Example: "pth --namespace mygame flow golden-m4 --slug e2e-m4 --admin-profile admin",
	},
	{
		Name:          "flow golden-m5",
		Milestone:     "M5.B",
		Description:   "Composite ADT flow: link-adt-start → link-adt-complete → adt-build-list → create-playtest (ADT + --auto-approve) → transition OPEN → signup → assert-applicant-auto-approved → get-adt-download-info → audit assertions. Eleven NDJSON lines. Dry-run-only until B10 wires the live MemClient harness (cli.md §6.5, PRD §4.8).",
		RequiredFlags: []flagSpec{slugFlag()},
		OptionalFlags: []flagSpec{
			{Name: "--title", Description: "playtest title (default 'Playtest <slug>')", ValueType: "string"},
			{Name: "--adt-namespace", Description: "ADT namespace to link + use (default adt-ns-1)", ValueType: "string"},
			{Name: "--adt-game-id", Description: "ADT-side game id (default game-x)", ValueType: "string"},
			{Name: "--adt-build-id", Description: "ADT-side build id (default build-001)", ValueType: "string"},
			{Name: "--auto-approve-limit", Description: "auto-approve cap (default 5)", ValueType: "int"},
			dryRunFlag(),
		},
		Example: "pth --namespace mygame --profile admin flow golden-m5 --slug adt-flow --dry-run",
	},
	{
		Name:          "playtest create",
		Milestone:     "M1",
		Description:   "Create a playtest. Admin token required. Mutable fields are PRD-whitelisted (cli.md §6.1, PRD §5.1).",
		RequiredFlags: []flagSpec{slugFlag(), {Name: "--title", Description: "playtest title", ValueType: "string"}},
		OptionalFlags: []flagSpec{
			{Name: "--description", Description: "playtest description", ValueType: "string"},
			{Name: "--banner-image-url", Description: "banner image URL", ValueType: "string"},
			platformsFlag("comma-separated platforms supported"),
			{Name: "--starts-at", Description: "RFC3339 timestamp", ValueType: "string"},
			{Name: "--ends-at", Description: "RFC3339 timestamp", ValueType: "string"},
			{Name: "--nda-required", Description: "set if the playtest requires NDA acceptance", ValueType: "bool"},
			{Name: "--nda-text", Description: "NDA prose; prefix with @ to read from a file (e.g. @nda.md)", ValueType: "string"},
			{Name: "--distribution-model", Description: "STEAM_KEYS | AGS_CAMPAIGN | ADT", ValueType: "enum"},
			{Name: "--initial-code-quantity", Description: "initial code quantity (AGS_CAMPAIGN only)", ValueType: "int"},
			{Name: "--auto-approve", Description: "enable auto-approve in Signup (PRD §5.4 / M5.A)", ValueType: "bool"},
			{Name: "--auto-approve-limit", Description: "auto-approve cap (1..100,000; required when --auto-approve)", ValueType: "int"},
			{Name: "--adt-namespace", Description: "ADT namespace (ADT only; required when --distribution-model=ADT)", ValueType: "string"},
			{Name: "--adt-game-id", Description: "ADT game id (ADT only)", ValueType: "string"},
			{Name: "--adt-build-id", Description: "ADT build id (ADT only)", ValueType: "string"},
			{Name: "--adt-fallback-url", Description: "Static https fallback download URL (ADT only)", ValueType: "string"},
			dryRunFlag(),
		},
		Example: "pth --namespace mygame --profile admin playtest create --slug summer-stress-test --title 'Summer Stress Test' --platforms STEAM",
	},
	{
		Name:          "playtest delete",
		Milestone:     "M1",
		Description:   "Soft-delete a playtest (idempotent). Admin token required (cli.md §6.1).",
		RequiredFlags: []flagSpec{idFlag()},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin playtest delete --id 01J0...",
	},
	{
		Name:          "playtest edit",
		Milestone:     "M1",
		Description:   "Edit PRD-whitelisted mutable fields only. Slug + status + distribution-model are immutable post-creation (PRD §5.1).",
		RequiredFlags: []flagSpec{idFlag()},
		OptionalFlags: []flagSpec{
			{Name: "--title", Description: "playtest title", ValueType: "string"},
			{Name: "--description", Description: "playtest description", ValueType: "string"},
			{Name: "--banner-image-url", Description: "banner image URL", ValueType: "string"},
			platformsFlag("comma-separated platforms"),
			{Name: "--starts-at", Description: "RFC3339 timestamp", ValueType: "string"},
			{Name: "--ends-at", Description: "RFC3339 timestamp", ValueType: "string"},
			{Name: "--nda-required", Description: "NDA required", ValueType: "bool"},
			{Name: "--nda-text", Description: "NDA prose; @file to load from disk", ValueType: "string"},
			{Name: "--auto-approve", Description: "enable auto-approve in Signup (PRD §5.4 / M5.A)", ValueType: "bool"},
			{Name: "--auto-approve-limit", Description: "auto-approve cap (1..100,000; required when --auto-approve)", ValueType: "int"},
			dryRunFlag(),
		},
		Example: "pth --namespace mygame --profile admin playtest edit --id 01J0... --title 'Updated Title'",
	},
	{
		Name:          "playtest get",
		Milestone:     "M1",
		Description:   "Fetch the admin view of a playtest. Admin token required (cli.md §6.1).",
		RequiredFlags: []flagSpec{idFlag()},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin playtest get --id 01J0...",
	},
	{
		Name:          "playtest get-player",
		Milestone:     "M1",
		Description:   "Fetch the player view of a playtest. Player token required (cli.md §6.1).",
		RequiredFlags: []flagSpec{slugFlag()},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --profile player playtest get-player --slug summer-stress-test",
	},
	{
		Name:          "playtest get-public",
		Milestone:     "M1",
		Description:   "Fetch the public view of a playtest. --anon implied (cli.md §6.1).",
		RequiredFlags: []flagSpec{slugFlag()},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --addr localhost:6565 playtest get-public --slug summer-stress-test",
	},
	{
		Name:          "playtest list",
		Milestone:     "M1",
		Description:   "List all playtests in --namespace. Admin token required (cli.md §6.1).",
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin playtest list",
	},
	{
		Name:          "playtest schedule-info",
		Milestone:     "M4",
		Description:   "Admin: print {slug, status, startsAt, endsAt, nextAutoTransition} for a playtest. Reads through AdminGetPlaytest (cli.md §6.4, STATUS_M4 phase 7).",
		RequiredFlags: []flagSpec{idFlag()},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin playtest schedule-info --id 01J0...",
	},
	{
		Name:        "playtest transition",
		Milestone:   "M1",
		Description: "Drive the playtest status machine (DRAFT → OPEN → CLOSED). Admin token required (cli.md §6.1).",
		RequiredFlags: []flagSpec{
			idFlag(),
			{Name: "--to", Description: "target status: DRAFT | OPEN | CLOSED", ValueType: "enum"},
		},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin playtest transition --id 01J0... --to OPEN",
	},
	{
		Name:          "public-config",
		Milestone:     "M5.B",
		Description:   "Fetch the public client config (unauth). Returns player_base_url so the admin AppUI can build cross-app share links.",
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --addr localhost:6565 public-config",
	},
	{
		Name:        "survey create",
		Milestone:   "M3",
		Description: "Admin: create the first-version survey for a playtest. Server mints question + option UUIDs (cli.md §6.3).",
		RequiredFlags: []flagSpec{
			playtestFlag(),
			{Name: "--from", Description: "path to JSON array of SurveyQuestion entries ('-' reads stdin)", ValueType: "string"},
		},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin survey create --playtest 01J0... --from ./questions.json",
	},
	{
		Name:        "survey edit",
		Milestone:   "M3",
		Description: "Admin: edit the survey, bumping version and preserving question/option UUIDs for kept entries (cli.md §6.3).",
		RequiredFlags: []flagSpec{
			playtestFlag(),
			{Name: "--from", Description: "path to JSON array of SurveyQuestion entries ('-' reads stdin)", ValueType: "string"},
		},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin survey edit --playtest 01J0... --from ./questions.json",
	},
	{
		Name:          "survey get",
		Milestone:     "M3",
		Description:   "Player: fetch the current survey version for a playtest (cli.md §6.3).",
		RequiredFlags: []flagSpec{playtestFlag()},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --profile player survey get --playtest 01J0...",
	},
	{
		Name:          "survey responses",
		Milestone:     "M3",
		Description:   "Admin: list submitted survey responses for a playtest. Cursor pagination on (submittedAt, id) DESC; optional --survey narrows to one version (cli.md §6.3).",
		RequiredFlags: []flagSpec{playtestFlag()},
		OptionalFlags: []flagSpec{
			{Name: "--survey", Description: "narrow to a specific Survey version (id from `survey get`)", ValueType: "string"},
			{Name: "--cursor", Description: "opaque page_token from a prior response", ValueType: "string"},
			{Name: "--page-size", Description: "page size (0 → server default 50)", ValueType: "int"},
			dryRunFlag(),
		},
		Example: "pth --namespace mygame --profile admin survey responses --playtest 01J0...",
	},
	{
		Name:        "survey submit",
		Milestone:   "M3",
		Description: "Player: submit one-shot survey answers (APPROVED + NDA-current; second submit is AlreadyExists with empty body) (cli.md §6.3).",
		RequiredFlags: []flagSpec{
			playtestFlag(),
			{Name: "--survey", Description: "survey id the answers target (the version fetched via `survey get`)", ValueType: "string"},
			{Name: "--from", Description: "path to JSON array of SurveyAnswer entries ('-' reads stdin)", ValueType: "string"},
		},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --profile player survey submit --playtest 01J0... --survey 01K0... --from ./answers.json",
	},
	{
		Name:        "user create",
		Milestone:   "M1",
		Description: "Create N AGS test users via /iam/v4/admin/.../test_users. Admin token required (cli.md §6.1).",
		OptionalFlags: []flagSpec{
			{Name: "--count", Description: "number of test users to create (1..100, default 1)", ValueType: "int"},
			{Name: "--country", Description: "ISO3166-1 alpha-2 country code (default US)", ValueType: "string"},
			dryRunFlag(),
		},
		Example: "pth --namespace mygame --profile admin user create --count 1",
	},
	{
		Name:        "user delete",
		Milestone:   "M1",
		Description: "Delete an AGS user (destructive). Refuses without --yes (cli.md §6.1).",
		RequiredFlags: []flagSpec{
			idFlag(),
			{Name: "--yes", Description: "skip the destructive-confirm guard", ValueType: "bool"},
		},
		OptionalFlags: []flagSpec{dryRunFlag()},
		Example:       "pth --namespace mygame --profile admin user delete --id <userId> --yes",
	},
	{
		Name:          "user login-as",
		Milestone:     "M1",
		Description:   "Password-login as a previously-created test user; stores credential under --profile (cli.md §6.1).",
		RequiredFlags: []flagSpec{idFlag()},
		OptionalFlags: []flagSpec{
			{Name: "--password-stdin", Description: "read password from one line on stdin instead of TTY prompt", ValueType: "bool"},
			dryRunFlag(),
		},
		Example: "pth --namespace mygame --profile player user login-as --id <userId> --password-stdin <pw.txt",
	},
	{
		Name:        "version",
		Milestone:   "M1",
		Description: "Print {buildSHA, buildDate, goVersion, protoSchema, protoFileCount} (cli.md §5).",
		Example:     "pth version",
	},
}

// slugFlag, idFlag, platformsFlag, dryRunFlag deduplicate the most common
// flag specs across the registry. The actual parsing lives in playtest.go
// (parsePlatforms etc.); this file only describes the flags for catalogue
// consumers.
func slugFlag() flagSpec {
	return flagSpec{Name: "--slug", Description: "playtest slug (PRD §5.1 regex ^[a-z0-9-]{3,50}$)", ValueType: "string"}
}

func idFlag() flagSpec {
	return flagSpec{Name: "--id", Description: "ULID identifier", ValueType: "string"}
}

func playtestFlag() flagSpec {
	return flagSpec{Name: "--playtest", Description: "playtest id (ULID)", ValueType: "string"}
}

func platformsFlag(desc string) flagSpec {
	return flagSpec{Name: "--platforms", Description: desc, ValueType: "csv"}
}

func dryRunFlag() flagSpec {
	return flagSpec{Name: "--dry-run", Description: "print the request JSON and exit without dialling", ValueType: "bool"}
}
