// Package config parses the backend's runtime environment per PRD §5.9.
//
// All configuration lives in env vars — there are no config files. Load()
// is the single entry point; it aggregates every parse failure into one
// error so operators see every missing/invalid key at once instead of
// discovering them one boot at a time.
package config

import (
	"fmt"
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

	// Optional with defaults (PRD §5.9).
	ReservationTTLSeconds  int
	ReclaimIntervalSeconds int
	LeaderLeaseTTLSeconds  int
	LeaderHeartbeatSeconds int
	AGSCodeBatchSize       int
	DMTimeoutSeconds       int
	DMDrainRatePerSec      int
	DBMaxConnections       int

	// Inherited from the template / operational tuning. Not PRD-specified
	// but consumed by main.go, so parsed here to keep all env reads in one
	// place.
	LogLevel               string
	AuthEnabled            bool
	RefreshIntervalSeconds int
	OtelServiceName        string
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
	if cfg.AGSCodeBatchSize, err = getInt("AGS_CODE_BATCH_SIZE", 1000); err != nil {
		return nil, err
	}
	if cfg.DMTimeoutSeconds, err = getInt("DM_TIMEOUT_SECONDS", 5); err != nil {
		return nil, err
	}
	if cfg.DMDrainRatePerSec, err = getInt("DM_DRAIN_RATE_PER_SEC", 5); err != nil {
		return nil, err
	}
	if cfg.DBMaxConnections, err = getInt("DB_MAX_CONNECTIONS", 10); err != nil {
		return nil, err
	}
	if cfg.RefreshIntervalSeconds, err = getInt("REFRESH_INTERVAL", 600); err != nil {
		return nil, err
	}

	cfg.LogLevel = getString("LOG_LEVEL", "info")
	cfg.AuthEnabled = getBool("PLUGIN_GRPC_SERVER_AUTH_ENABLED", true)
	cfg.OtelServiceName = os.Getenv("OTEL_SERVICE_NAME")

	return cfg, nil
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
