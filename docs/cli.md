# playtesthub — CLI (`pth`)

First-class command-line client that drives the playtesthub backend over its real wire protocol. Exists so humans and AI agents can exercise the app end-to-end without touching the two frontends, and so the e2e test suite (PRD §4.1, `engineering.md` §3.3) has a single reusable harness.

Referenced from [`STATUS.md`](STATUS.md) M1 phase 10, [`engineering.md`](engineering.md) §2, and PRD §4.7.

---

## 1. Goals

- Give a human operator a frictionless way to poke any RPC during development, bug-repro, and demo walkthroughs.
- Give an AI agent (Claude Code, local automation) a self-describing surface for exercising flows — no scraping the proto, no hand-rolled curl against the grpc-gateway.
- Serve as the **only** e2e harness. The `e2e/*_test.go` suite shells out to `pth` subcommands against an in-process server + testcontainers-postgres. One code path, one set of bugs.
- Dogfood the wire contract. Every proto/HTTP-annotation change surfaces in the CLI on the next build; stale contracts fail loudly instead of quietly.

## 2. Non-goals

- **Not a prod admin tool.** `pth` is for developers and test harnesses. Studio admins use the Extend App UI.
- **Not a load generator.** Perf runs live in `scripts/loadtest/` (PRD §6 / §7).
- **No stateful session / REPL.** One command, one RPC, one exit code. Composition via shell.
- **No config files.** Env vars + flags only, matching backend convention (PRD §5.9).
- **No SDK layer.** Consumers do not import `pth` as a Go package. Everything is a subcommand; the CLI is the surface.

## 3. Overview

- **Binary**: `pth`. Source at `cmd/pth/`.
- **Language**: Go. Reuses the generated stubs in `pkg/pb/` — zero extra toolchain.
- **Transport**: **gRPC directly** against `:6565` (the native port), not the grpc-gateway REST proxy. Faithful exercise of the wire contract and identical semantics to what the Svelte player app's REST calls get routed through.
- **Distribution**: `go install github.com/anggorodewanto/playtesthub/cmd/pth@latest` for dev boxes; built into the backend Docker image for e2e.
- **Output**: JSON on stdout (one document per call, pipeable to `jq`). Human-readable error + gRPC status code on stderr. Exit code is `0` on gRPC `OK`, non-zero on any other status.

## 4. Invocation shape

```
pth [global flags] <domain> <action> [action flags]
pth <meta command>
```

Global flags (all overridable per invocation; most have env-var fallbacks so scripted flows stay terse):

| Flag | Env var | Purpose |
| --- | --- | --- |
| `--addr` | `PTH_ADDR` | gRPC endpoint. Default `localhost:6565`. |
| `--base-path` | `PTH_BASE_PATH` | Backend `BASE_PATH` if the server is fronted by a prefix; used when talking to Extend rather than a local instance. |
| `--namespace` | `PTH_NAMESPACE` | AGS namespace. Required for any auth flow and for AGS-scoped RPCs. |
| `--profile` | `PTH_PROFILE` | Named profile in the credentials store (§7). Default `default`. Lets you juggle prod / sandbox / per-test-user sessions without re-logging-in. |
| `--token` | `PTH_TOKEN` | Override: raw AGS IAM bearer token, passed verbatim. Bypasses the credentials store — useful for CI where the token is already in a secret. |
| `--anon` | — | Send no `Authorization` metadata. For unauth RPCs (`GetPlaytest` unauth variant). |
| `--timeout` | `PTH_TIMEOUT` | Per-call gRPC deadline. Default `10s`. |
| `--insecure` | `PTH_INSECURE` | TLS-off for local work. Default true when `--addr` is loopback. |
| `-v` / `--verbose` | — | Log the outgoing RPC + headers (token redacted) to stderr. |

Token resolution order: `--token` > `PTH_TOKEN` > credentials store (§7) keyed by `(addr, namespace, profile)`. `--anon` short-circuits all of them.

## 5. Meta commands (for humans and AI)

These exist so a fresh operator — person or agent — can discover the surface without reading this document.

- `pth version` — build metadata (git SHA, proto schema version, Go version).
- `pth doctor` — connectivity + auth smoke test. Attempts `GetPlaytest` (unauth) against a known sentinel slug; reports gRPC status, round-trip latency, and the server's `BASE_PATH`. Non-zero exit if anything fails.
- `pth describe` — emits a JSON catalogue of every subcommand: name, milestone, required flags, optional flags, description, example. Stable schema (`cli-schema.v1`). AI agents read this instead of parsing `--help` prose.
- `pth auth …` — login / logout / whoami against a real AGS IAM. Full detail in §7.
- `pth <cmd> --help` — GNU-style help for humans.
- `pth <cmd> --dry-run` — prints the gRPC request body (JSON) that *would* be sent, and exits. Does not open a connection.

## 6. Subcommand catalogue

Commands mirror PRD §4.7, grouped by domain. Each command lands **in the same milestone as the RPC it wraps** — the CLI grows with the backend, not ahead of it.

### 6.1 M1 — shipped in milestone M1

**Auth group** (wraps AGS IAM directly, not a playtesthub RPC):

| Command | Purpose |
| --- | --- |
| `pth auth login --discord [--manual] [--no-browser]` | Interactive Discord-federated login (§7.1). Captures an AGS access token via loopback callback or manual paste. |
| `pth auth login --password --username <u> [--password-stdin]` | AGS IAM ROPC grant for a native AGS user — the path test users and admins with AGS credentials use (§7.2). Password via TTY prompt by default; `--password-stdin` for CI. |
| `pth auth logout [--profile <p>]` | Clears the stored credential. |
| `pth auth whoami` | Prints `{userId, namespace, expiresAt, loginMode}` for the active token; non-zero exit if expired or missing. |
| `pth auth token` | Prints the active bearer token to stdout. For piping into other tools. |

**User group** (AGS IAM admin endpoints; admin token required):

| Command | Purpose |
| --- | --- |
| `pth user create --username <u> [--password-stdin] [--email <e>] [--display-name <n>]` | Creates a native AGS user in `$PTH_NAMESPACE`. Emits `{userId, username, email}`. No Discord federation — the user has no Discord ID claim, so backend Signup falls back to the raw IAM `sub` per PRD §10 M1. |
| `pth user delete --id <userId>` | Destructive — prompts for `yes` unless `--yes`. Used by e2e teardown. |
| `pth user login-as --id <userId> [--password-stdin]` | Convenience: password-login as a previously-created test user and store the credential under a named profile. Equivalent to `auth login --password` but lets the caller look the user up by id. |

**Playtest + applicant group** (playtesthub RPCs):

| Command | Wraps RPC | Notes |
| --- | --- | --- |
| `pth playtest get-public --slug <s>` | `GetPlaytest` (unauth) | `--anon` implied. |
| `pth playtest get-player --slug <s>` | `GetPlaytestForPlayer` | Requires player token. |
| `pth playtest get --id <id>` | `GetPlaytest` (admin) | Admin token. |
| `pth playtest list` | `ListPlaytests` | Admin. |
| `pth playtest create --slug <s> --title <t> [--distribution-model STEAM_KEYS\|AGS_CAMPAIGN] [--nda-required] [--nda-text @file.md] [--starts-at <ts>] [--ends-at <ts>] [--platforms STEAM,XBOX,...]` | `CreatePlaytest` | M1 note: `AGS_CAMPAIGN` returns `Unimplemented` — surfaced as a non-zero exit with the raw gRPC message. |
| `pth playtest edit --id <id> [mutable fields]` | `EditPlaytest` | Only PRD-whitelisted fields. |
| `pth playtest delete --id <id>` | `SoftDeletePlaytest` | Idempotent. |
| `pth playtest transition --id <id> --to <status>` | `TransitionPlaytestStatus` | |
| `pth applicant signup --playtest <id> --platforms STEAM,XBOX` | `Signup` | Requires player token. |
| `pth applicant status --playtest <id>` | `GetApplicantStatus` | Player's own. |

### 6.2 M2 — land alongside M2 RPCs

| Command | Wraps RPC |
| --- | --- |
| `pth applicant accept-nda --playtest <id>` | `AcceptNDA` |
| `pth applicant list --playtest <id> [--status <s>] [--cursor <c>]` | `ListApplicants` |
| `pth applicant approve --id <id>` | `ApproveApplicant` |
| `pth applicant reject --id <id> [--reason <r>]` | `RejectApplicant` |
| `pth applicant retry-dm --id <id>` | `RetryDM` |
| `pth applicant get-code --playtest <id>` | `GetGrantedCode` |
| `pth code upload --playtest <id> --file <csv>` | `UploadCodes` |
| `pth code top-up --playtest <id> --quantity <n>` | `TopUpCodes` |
| `pth code sync-from-ags --playtest <id>` | `SyncFromAGS` |
| `pth code pool --playtest <id>` | `GetCodePool` |

### 6.3 M3 — land alongside M3 RPCs

| Command | Wraps RPC |
| --- | --- |
| `pth survey create --playtest <id> --from <yaml>` | `CreateSurvey` |
| `pth survey edit --id <id> --from <yaml>` | `EditSurvey` |
| `pth survey get --playtest <id>` | `GetSurvey` |
| `pth survey submit --playtest <id> --from <yaml>` | `SubmitSurveyResponse` |
| `pth survey responses --playtest <id> [--survey <sid>] [--cursor <c>]` | `ListSurveyResponses` |
| `pth audit list --playtest <id> [--actor <a>] [--action <a>] [--cursor <c>]` | `ListAuditLog` |
| `pth applicant retry-failed-dms --playtest <id>` | `RetryFailedDms` |

### 6.4 Scripted flows

Composite commands that run a whole PRD §4.1 golden flow end-to-end in one shot. Each flow prints one JSON document per step to stdout (one line per step, NDJSON), so the caller can grep/jq through the sequence. Flows are the bread-and-butter surface for the e2e suite.

| Command | Milestone | Steps |
| --- | --- | --- |
| `pth flow golden-m1 --slug <s>` | M1 | create-playtest → transition OPEN → signup (synthetic player) → assert status=PENDING. |
| `pth flow golden-m2 --slug <s>` | M2 | golden-m1 → accept-nda → upload codes → approve → assert status=APPROVED + code visible. |
| `pth flow golden-m3 --slug <s>` | M3 | golden-m2 → create survey → submit response → list responses. |

Each flow takes `--admin-token` and `--player-token` flags (or their `--fake-jwt` equivalents) so it can run against any environment. Flows are pure CLI composition — they do not ship their own gRPC client; they invoke the single-RPC subcommands in-process.

## 7. Authentication

`pth` talks to **real AGS IAM** only. There is no fake-JWT / dev-bypass mode — every token is minted by AGS IAM in the configured namespace. Two login flows cover the human-vs-automation split.

### 7.1 Discord-federated login (`pth auth login --discord`)

Mirrors the player-side browser flow byte-for-byte: Discord OAuth in the user's browser → CLI loopback receives the Discord authorization code → CLI POSTs the code to the backend's `Player.ExchangeDiscordCode` RPC → backend runs the AGS platform-token grant and returns AGS access + refresh tokens. Used when a human wants to exercise playtesthub as a real Discord-federated player, or when validating the federation path itself works.

This flow does **not** hit AGS IAM's `/iam/v3/oauth/authorize` endpoint. STATUS.md M1 phase 9.2 + 9.3 documented that the AGS authorization-code grant fails with `invalid_grant: justice platform account not found` on shared-cloud game namespaces because that codepath skips Justice-platform-account autocreation. The platform-token grant (which `Player.ExchangeDiscordCode` wraps) is the one AGS path that works for player-side Discord login on shared cloud.

Sequence:

1. CLI binds a one-shot loopback HTTP listener at `http://127.0.0.1:<PTH_DISCORD_LOOPBACK_PORT>/callback` (default port `14565`). The port is **fixed** rather than ephemeral — see "Setup requirement" below for why.
2. CLI constructs the **Discord** OAuth authorize URL: `https://discord.com/oauth2/authorize?response_type=code&client_id=<PTH_DISCORD_CLIENT_ID>&redirect_uri=http://127.0.0.1:<port>/callback&state=<random>&scope=identify+email`.
3. CLI prints the URL to stderr and — unless `--no-browser` — opens it via `xdg-open` / `open` / `start`. User authenticates with Discord; Discord redirects to the loopback URL with `?code=...&state=...`.
4. Loopback handler validates `state`, then POSTs `{"code": "...", "redirect_uri": "http://127.0.0.1:<port>/callback"}` to `<PTH_BACKEND_REST_URL>/v1/player/discord/exchange` (the grpc-gateway REST surface — gRPC's native port can't carry the unauth REST mapping). Backend runs the AGS platform-token grant and returns `{access_token, refresh_token, expires_in, token_type}`.
5. CLI renders a "you can close this tab" page, shuts down the listener, writes the AGS tokens + a synthetic `userId` (decoded from the JWT `sub` claim) to the credentials store with `loginMode="discord"` (§7.3).

`--manual` bypasses the loopback listener: CLI prints the Discord authorize URL, user logs in in any browser, user copy-pastes the final redirect URL (or just the `code` param) back into the CLI prompt. CLI then performs steps 4–5. Use this when the user can't hit `127.0.0.1:14565` from their browser (remote dev box, port already in use, restricted network).

`--no-browser` prints the URL and waits for callback but does not auto-open — useful over SSH with browser-on-laptop port forwarding.

`--dry-run` prints the constructed authorize URL + listener address + exchange URL to stdout (one JSON object) and exits 0 without binding the listener or POSTing anything. Establishes the pattern reused by every other subcommand for offline introspection.

**Required env vars** (no global flag equivalents — CLI-only secret-of-config that the rest of the surface doesn't need):

| Env var | Purpose |
| --- | --- |
| `PTH_DISCORD_CLIENT_ID` | The **Discord** OAuth Client ID (public). Same value the player Vite bundle reads from `player/public/config.json` as `discordClientId`. |
| `PTH_DISCORD_LOOPBACK_PORT` | Port the loopback listener binds to. Default `14565`. Must match the value registered on Discord + AGS Admin Portal — see below. |
| `PTH_BACKEND_REST_URL` | HTTPS base URL of the backend's grpc-gateway (e.g. `https://<ags-host>/ext-<ns>-<app>`). The exchange POST goes here, not the gRPC `--addr`. |

**Setup requirement**: register **`http://127.0.0.1:14565/callback`** (or whatever fixed port `PTH_DISCORD_LOOPBACK_PORT` resolves to) as an allowed redirect URI in **two** places:

1. **Discord developer portal → OAuth2 → Redirects**. Discord matches byte-for-byte including port — random ephemeral ports cannot work here. The fixed-port choice exists specifically to make this allowlist a one-time operator step.
2. **AGS Admin Portal → Login Methods → Platforms → Discord → RedirectUri**. AGS forwards this exact value to Discord's `/oauth2/token` when redeeming the code; mismatch → `invalid_grant: Invalid "redirect_uri" in request.` See `docs/runbooks/setup-ags-discord.md` § "Three URLs that must agree byte-for-byte" — the same constraint that governs the player flow applies here, and the implication is the same: **one Discord-platform credential per redirect URI per AGS tenant**. Operators who want Discord login to work for both the player web app *and* the CLI need either two AGS namespaces (each with its own Discord platform credential) or two Discord OAuth applications targeting one AGS tenant via separate platform configs.

Procedure documented in `docs/runbooks/setup-ags-discord.md` § "CLI loopback origin (`pth auth login --discord`)". `--manual` mode is governed by the same allowlist constraints — the pasted redirect URL still has to match a registered value.

### 7.2 Password login (`pth auth login --password`)

AGS IAM ROPC grant against a native (non-federated) AGS user. Used for:

- Test players created via `pth user create` — no Discord account required.
- Admins logging in with their AGS Admin Portal credentials for scripted admin work.
- All CI / e2e runs.

Password is read from a TTY prompt by default; `--password-stdin` consumes one line from stdin for headless use. The password never appears in flags, argv, or shell history.

### 7.3 Credentials store

Stored at `~/.config/playtesthub/credentials.json` (Linux/macOS) / `%APPDATA%\playtesthub\credentials.json` (Windows). File perms `0600`, directory perms `0700`; CLI refuses to read the file if perms are looser and prints remediation.

Schema:

```json
{
  "version": 1,
  "profiles": {
    "default":    { "addr": "...", "namespace": "...", "userId": "...", "loginMode": "discord",  "accessToken": "...", "refreshToken": "...", "expiresAt": "..." },
    "test-admin": { "addr": "...", "namespace": "...", "userId": "...", "loginMode": "password", "accessToken": "...", "refreshToken": "...", "expiresAt": "..." },
    "test-player-a": { ... }
  }
}
```

`--profile <name>` selects which profile to use for any subsequent call. CLI auto-refreshes a token that is within 60s of expiry using the stored refresh token; if the refresh fails, the CLI prompts the user to re-login with a clear message (e.g. `token for profile 'default' has expired — run 'pth auth login --discord' to re-authenticate`).

### 7.4 End-to-end and CI

E2E tests drive the CLI like any other consumer:

1. At suite setup, an admin-credentialed profile logs in once via `pth auth login --password` using credentials sourced from CI secrets.
2. Each test creates one or more throwaway test users via `pth user create`, then logs each in as its own profile via `pth user login-as --id <userId> --password-stdin`.
3. Test exercises the flow using `--profile test-player-<n>` for player calls and `--profile test-admin` for admin calls.
4. Teardown calls `pth user delete --id <userId> --yes` for each created user and clears the test profiles.

Because tests run against the user's own AGS namespace (not a dedicated e2e namespace), tests must use unique slugs and user IDs per run so a failed teardown does not poison the next run. Slug strategy: `e2e-<timestamp>-<random>`. Username strategy: same pattern. The e2e harness owns its own cleanup scheduler that deletes stale test rows older than 24h as a belt-and-braces.

## 8. Output contract

Single-RPC commands:

- stdout: **one** JSON document — the unmarshalled proto response. Protobuf field names, not Go field names. `null` / absent fields omitted per `protojson`.
- stderr: empty on success; `gRPC <CODE>: <message>\n` on failure.
- exit code: `0` on gRPC `OK`; `1` for `InvalidArgument`/`NotFound`/client errors; `2` for `Unavailable`/`DeadlineExceeded`/transport errors; `3` for local flag-parse / env errors.

Flow commands:

- stdout: NDJSON, one step per line. Each line is `{"step":"signup","status":"OK","response":{...}}` on success or `{"step":"approve","status":"FAILED","error":{"code":"FailedPrecondition","message":"..."}}` on failure.
- Flow stops at the first failed step and exits non-zero.

`--dry-run` for single-RPC commands prints the request JSON to stdout and exits 0 without opening a connection.

## 9. Repo placement and test strategy

Lives at `cmd/pth/`. This is the first `cmd/` dir in the tree — the template is flat, but a standalone binary justifies it.

```
cmd/pth/
├── main.go                 # flag parsing + dispatch
├── globals.go              # global-flag parser + dial-time bearer resolution
├── auth.go                 # `pth auth …` subcommand dispatcher (login/logout/whoami/token)
├── credstore.go            # credentials.json read/write + profile selection
├── iamclient.go            # AGS IAM ROPC + refresh-token grants
├── discord.go              # Discord-direct → backend ExchangeDiscordCode flow + --manual
├── output.go               # JSON/NDJSON writers, exit-code mapping
├── version.go              # `pth version`
├── doctor.go               # `pth doctor`
├── describe.go             # `pth describe` catalogue (phase 10.6)
├── user.go                 # user create/delete/login-as — AGS IAM admin endpoints (phase 10.4)
├── playtest.go             # playtest subcommands (10.5 fans this out)
├── applicant.go            # applicant subcommands (phase 10.5)
├── flow.go                 # `pth flow golden-m1` (phase 10.6)
└── *_test.go               # unit tests (see below)
```

Tests:

- **Unit** (`cmd/pth/*_test.go`): flag parsing, output formatting, exit-code mapping, `describe` catalogue stability. Mock the gRPC client at the `pb.PlaytesthubServiceClient` interface boundary.
- **Integration** — none. The CLI's integration path *is* the e2e suite.
- **E2E** (`e2e/*_test.go`): boot backend in-process + testcontainers-postgres, shell out to the `pth` binary, assert on stdout/exit code. The M1 phase 10 e2e test (STATUS.md) is the first consumer; M2/M3 e2e tests extend the same harness.

CI gate: `pth describe` output is regenerated and diff-checked on every PR (same discipline as the admin codegen gate in `engineering.md` §5). This catches silent command-catalogue drift before it reaches AI consumers.

## 10. AI-agent affordances

The CLI is the primary surface for AI-driven exercise of the app. Four deliberate choices support this:

1. **Self-describing**: `pth describe` gives a stable JSON catalogue so an agent can enumerate the surface without reading this doc or the proto.
2. **Dry-run**: `pth <cmd> --dry-run` lets an agent validate a request shape before committing an action — a cheap way to avoid destructive mistakes mid-conversation.
3. **Deterministic output**: one JSON doc per RPC, protobuf field names, no prose. No log noise on stdout. Agents can `jq` without brittle parsers.
4. **Scriptable auth**: `pth user create` + `pth auth login --password --password-stdin` lets an agent mint and log in as an isolated test user without any interactive step. Destructive endpoints (`user delete`) still require `--yes` so an agent can't silently wipe a real user.

Agents should prefer `pth flow golden-m*` for reproducing the PRD golden flow; single-RPC subcommands are for targeted probing. For any multi-step scenario, an agent should create a dedicated test user per run rather than reusing a profile — keeps runs independent and teardown scoped.

## 11. Milestone hooks

| Milestone | Deliverable |
| --- | --- |
| M1 phase 10 | `pth` binary; meta commands (`doctor`, `describe`, `version`, `--dry-run`); **auth group** (`login --discord` loopback + `--manual`, `login --password`, `logout`, `whoami`, `token`) with credentials store + refresh; **user group** (`create`, `delete`, `login-as`) against AGS IAM admin endpoints; all M1 playtest/applicant subcommands (§6.1); `pth flow golden-m1`. E2E test (phase 11) consumes it. |
| M2 | All M2 subcommands (§6.2); `pth flow golden-m2`. |
| M3 | All M3 subcommands (§6.3); `pth flow golden-m3`. |

## 12. When this document is wrong

Update it. The CLI surface is a public contract for humans, AI agents, and the e2e harness — silent drift here is worse than silent drift in internal code. If an RPC is added, renamed, or removed, the matching row in §6 changes in the same commit as the proto change.
