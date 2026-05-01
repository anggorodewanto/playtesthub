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
			{Name: "--distribution-model", Description: "STEAM_KEYS | AGS_CAMPAIGN (M1: AGS_CAMPAIGN returns Unimplemented)", ValueType: "enum"},
			{Name: "--initial-code-quantity", Description: "initial code quantity (AGS_CAMPAIGN only)", ValueType: "int"},
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

func platformsFlag(desc string) flagSpec {
	return flagSpec{Name: "--platforms", Description: desc, ValueType: "csv"}
}

func dryRunFlag() flagSpec {
	return flagSpec{Name: "--dry-run", Description: "print the request JSON and exit without dialling", ValueType: "bool"}
}
