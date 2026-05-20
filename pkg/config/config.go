// Package config parses the backend's runtime environment per PRD §5.9.
//
// All configuration lives in env vars — there are no config files. Load()
// is the single entry point; it aggregates every parse failure into one
// error so operators see every missing/invalid key at once instead of
// discovering them one boot at a time.
package config

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Config is the parsed runtime configuration. All durations are seconds —
// callers wrap with time.Duration at the boundary where one is needed.
type Config struct {
	// Required (PRD §5.9).
	DatabaseURL        string
	DiscordBotToken    string
	AGSIAMClientID     string
	AGSIAMClientSecret string
	AGSBaseURL         string
	AGSNamespace       string
	BasePath           string

	// AGSStoreID is the AGS Platform Store id under which playtesthub
	// creates/deletes Items for the AGS_CAMPAIGN distribution model
	// (PRD §4.6 / docs/ags-failure-modes.md). Optional: when empty the
	// backend wires the in-memory ags.MemClient and AGS_CAMPAIGN
	// playtests are recorded only locally — no real AGS resources are
	// provisioned. Set this in production deployments to enable real
	// Item / Campaign / code generation.
	AGSStoreID string

	// AGS_CAMPAIGN region pricing — see docs/engineering.md "AGS namespace
	// prerequisites". AGS rejects CreateItem with errorCode 30022
	// "Default region [<region>] is required" unless RegionData has a
	// fully-formed entry for the store's defaultRegion (currencyCode +
	// currencyType + price), even for non-purchasable CODE items.
	//
	// AGSRegionCurrencyCode names a currency that already exists in
	// AGSNamespace (often a VIRTUAL coin). When unset, items are created
	// without RegionData — this works only against namespaces / stores
	// where AGS does not enforce the region requirement.
	AGSRegionCurrencyCode string
	// AGSRegionCurrencyType is "VIRTUAL" or "REAL". Defaults to VIRTUAL
	// since playtest items are never sold.
	AGSRegionCurrencyType string
	// AGSRegionCode is the store's defaultRegion. Defaults to "US".
	AGSRegionCode string

	// Optional with defaults (PRD §5.9).
	ReservationTTLSeconds  int
	ReclaimIntervalSeconds int
	LeaderLeaseTTLSeconds  int
	LeaderHeartbeatSeconds int
	// WindowTickSeconds is the tick interval of the internal/window/
	// auto-transition worker (PRD §5.1 "Window-driven auto-transition").
	// 0 disables the worker entirely — status then sticks at its
	// current value until a manual `TransitionPlaytestStatus`.
	WindowTickSeconds int
	AGSCodeBatchSize  int
	DMTimeoutSeconds  int
	DMDrainRatePerSec int
	DMQueueMaxDepth   int
	DBMaxConnections  int

	// Inherited from the template / operational tuning. Not PRD-specified
	// but consumed by main.go, so parsed here to keep all env reads in one
	// place.
	LogLevel               string
	AuthEnabled            bool
	RefreshIntervalSeconds int
	OtelServiceName        string

	// CORSAllowedOrigins is the comma-separated allowlist of browser
	// origins permitted to call the grpc-gateway HTTP surface. Empty →
	// no CORS handling (server returns 501 on OPTIONS, same as the
	// vanilla grpc-gateway). Set this when the player Svelte bundle is
	// hosted off-origin (e.g. GitHub Pages: "https://<user>.github.io")
	// — the middleware reflects the matched origin and sets
	// `Access-Control-Allow-Credentials: true` so cookie-based admin
	// auth keeps working.
	CORSAllowedOrigins []string

	// PlayerBaseURL is the public origin (with optional sub-path) where
	// the player Svelte bundle is hosted, e.g.
	// "https://anggorodewanto.github.io/playtesthub". Used to build the
	// deep link surfaced inside the approval DM so applicants can click
	// straight through to the pending page (where their granted code is
	// rendered). Optional: when empty, the DM body falls back to
	// non-clickable copy. The hash-router's "#" prefix is appended by
	// the service layer; do not include it here.
	PlayerBaseURL string

	// ADTBaseURL is the AccelByte Development Toolkit origin (no path)
	// the admin UI redirects to at link time, e.g.
	// "https://adt.example.com". Optional — when empty the ADT linkage
	// surface is disabled (StartADTLink returns FailedPrecondition per
	// errors.md). Required in production when any ADT-distribution
	// playtest exists. PRD §5.9.
	ADTBaseURL string

	// ADTRedirectBaseURL is the admin-UI origin ADT redirects back to
	// after the link round-trip, e.g.
	// "https://admin.example.com". Combined with the literal path
	// suffix "/adt-link-callback" to form the redirect_uri query param
	// on the ADT link URL. Required when ADTBaseURL is set. PRD §5.9.
	ADTRedirectBaseURL string

	// ADTLinkagePendingTTLSeconds bounds the lifetime of an
	// adt_link_pending row. Default 600 (10 minutes) — long enough for
	// an operator to drive the ADT-side sign-in, short enough that
	// stale rows don't accumulate. CompleteADTLink runs an inline
	// sweep on every call so a positive value also bounds the table
	// size. PRD §5.9.
	ADTLinkagePendingTTLSeconds int
}

// MissingRequiredError lists every required env var that was unset or
// empty. Load returns it unwrapped so callers can type-assert with
// errors.As and render a clean boot-failure message.
type MissingRequiredError struct {
	Keys []string
}

func (e *MissingRequiredError) Error() string {
	return "missing required environment variables: " + strings.Join(e.Keys, ", ")
}

// Load reads every env var described by PRD §5.9 and returns a populated
// Config. Missing required vars produce a *MissingRequiredError listing
// every key at once. Invalid values (bad int, malformed path) return a
// plain error naming the offending key.
func Load() (*Config, error) {
	cfg := &Config{}

	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	cfg.DiscordBotToken = os.Getenv("DISCORD_BOT_TOKEN")
	cfg.AGSIAMClientID = os.Getenv("AGS_IAM_CLIENT_ID")
	cfg.AGSIAMClientSecret = os.Getenv("AGS_IAM_CLIENT_SECRET")
	cfg.AGSBaseURL = os.Getenv("AGS_BASE_URL")
	cfg.AGSNamespace = os.Getenv("AGS_NAMESPACE")
	cfg.BasePath = os.Getenv("BASE_PATH")

	missing := collectMissing(map[string]string{
		"DATABASE_URL":          cfg.DatabaseURL,
		"DISCORD_BOT_TOKEN":     cfg.DiscordBotToken,
		"AGS_IAM_CLIENT_ID":     cfg.AGSIAMClientID,
		"AGS_IAM_CLIENT_SECRET": cfg.AGSIAMClientSecret,
		"AGS_BASE_URL":          cfg.AGSBaseURL,
		"AGS_NAMESPACE":         cfg.AGSNamespace,
		"BASE_PATH":             cfg.BasePath,
	})
	if len(missing) > 0 {
		return nil, &MissingRequiredError{Keys: missing}
	}

	if !strings.HasPrefix(cfg.BasePath, "/") {
		return nil, fmt.Errorf("BASE_PATH must start with '/', got %q", cfg.BasePath)
	}

	var err error
	if cfg.ReservationTTLSeconds, err = getInt("RESERVATION_TTL_SECONDS", 60); err != nil {
		return nil, err
	}
	if cfg.ReclaimIntervalSeconds, err = getInt("RECLAIM_INTERVAL_SECONDS", 30); err != nil {
		return nil, err
	}
	if cfg.LeaderLeaseTTLSeconds, err = getInt("LEADER_LEASE_TTL_SECONDS", 30); err != nil {
		return nil, err
	}
	if cfg.LeaderHeartbeatSeconds, err = getInt("LEADER_HEARTBEAT_SECONDS", 10); err != nil {
		return nil, err
	}
	if cfg.WindowTickSeconds, err = getInt("WINDOW_TICK_SECONDS", 60); err != nil {
		return nil, err
	}
	if cfg.WindowTickSeconds < 0 {
		return nil, fmt.Errorf("WINDOW_TICK_SECONDS must be >= 0, got %d", cfg.WindowTickSeconds)
	}
	if cfg.AGSCodeBatchSize, err = getInt("AGS_CODE_BATCH_SIZE", 1000); err != nil {
		return nil, err
	}
	if cfg.DMTimeoutSeconds, err = getInt("DM_TIMEOUT_SECONDS", 5); err != nil {
		return nil, err
	}
	if cfg.DMDrainRatePerSec, err = getInt("DM_DRAIN_RATE_PER_SEC", 5); err != nil {
		return nil, err
	}
	if cfg.DMQueueMaxDepth, err = getInt("DM_QUEUE_MAX_DEPTH", 10000); err != nil {
		return nil, err
	}
	if cfg.DBMaxConnections, err = getInt("DB_MAX_CONNECTIONS", 10); err != nil {
		return nil, err
	}
	if cfg.RefreshIntervalSeconds, err = getInt("REFRESH_INTERVAL", 600); err != nil {
		return nil, err
	}

	cfg.AGSStoreID = os.Getenv("AGS_STORE_ID")
	cfg.AGSRegionCurrencyCode = os.Getenv("AGS_REGION_CURRENCY_CODE")
	cfg.AGSRegionCurrencyType = getString("AGS_REGION_CURRENCY_TYPE", "VIRTUAL")
	cfg.AGSRegionCode = getString("AGS_REGION_CODE", "US")

	cfg.LogLevel = getString("LOG_LEVEL", "info")
	cfg.AuthEnabled = getBool("PLUGIN_GRPC_SERVER_AUTH_ENABLED", true)
	cfg.OtelServiceName = os.Getenv("OTEL_SERVICE_NAME")
	cfg.CORSAllowedOrigins = parseCSV(os.Getenv("CORS_ALLOWED_ORIGINS"))

	cfg.PlayerBaseURL = strings.TrimRight(os.Getenv("PLAYER_BASE_URL"), "/")
	if err := validateHTTPURL("PLAYER_BASE_URL", cfg.PlayerBaseURL); err != nil {
		return nil, err
	}

	if err := loadADT(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// loadADT reads + validates the ADT-related env vars (PRD §5.9). Split
// out of Load to keep cognitive complexity under the lint threshold.
func loadADT(cfg *Config) error {
	cfg.ADTBaseURL = strings.TrimRight(os.Getenv("ADT_BASE_URL"), "/")
	if err := validateHTTPURL("ADT_BASE_URL", cfg.ADTBaseURL); err != nil {
		return err
	}
	cfg.ADTRedirectBaseURL = strings.TrimRight(os.Getenv("ADT_REDIRECT_BASE_URL"), "/")
	if err := validateHTTPURL("ADT_REDIRECT_BASE_URL", cfg.ADTRedirectBaseURL); err != nil {
		return err
	}
	if cfg.ADTBaseURL != "" && cfg.ADTRedirectBaseURL == "" {
		return fmt.Errorf("ADT_REDIRECT_BASE_URL is required when ADT_BASE_URL is set")
	}
	ttl, err := getInt("ADT_LINKAGE_PENDING_TTL_SECONDS", 600)
	if err != nil {
		return err
	}
	if ttl < 1 {
		return fmt.Errorf("ADT_LINKAGE_PENDING_TTL_SECONDS must be >= 1, got %d", ttl)
	}
	cfg.ADTLinkagePendingTTLSeconds = ttl
	return nil
}

// validateHTTPURL accepts an empty string (caller's "optional" sentinel)
// or an http(s) URL with a host. Anything else returns a uniform error
// naming the env var key.
func validateHTTPURL(key, raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%s must be an http(s) URL with a host, got %q", key, raw)
	}
	return nil
}

func parseCSV(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func collectMissing(required map[string]string) []string {
	var missing []string
	for k, v := range required {
		if v == "" {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	return missing
}

func getString(key, fallback string) string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	return v
}

func getInt(key string, fallback int) (int, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q: %w", key, raw, err)
	}
	return v, nil
}

func getBool(key string, fallback bool) bool {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return fallback
	}
	return strings.EqualFold(raw, "true")
}
