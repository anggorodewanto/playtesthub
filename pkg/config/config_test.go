package config_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/anggorodewanto/playtesthub/pkg/config"
)

// setRequired seeds every required env var with a valid value so individual
// tests can clear exactly one thing and observe the effect.
func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://pg:pg@localhost:5432/pg?sslmode=disable")
	t.Setenv("DISCORD_BOT_TOKEN", "discord-bot-token")
	t.Setenv("AGS_IAM_CLIENT_ID", "client-id")
	t.Setenv("AGS_IAM_CLIENT_SECRET", "client-secret")
	t.Setenv("AGS_BASE_URL", "https://ags.example.com")
	t.Setenv("AGS_NAMESPACE", "playtesthub-dev")
	t.Setenv("BASE_PATH", "/playtesthub")
}

func TestLoad_RequiredVars_AllSet_ReturnsConfig(t *testing.T) {
	setRequired(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.DatabaseURL != "postgres://pg:pg@localhost:5432/pg?sslmode=disable" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.DiscordBotToken != "discord-bot-token" {
		t.Errorf("DiscordBotToken = %q", cfg.DiscordBotToken)
	}
	if cfg.AGSIAMClientID != "client-id" {
		t.Errorf("AGSIAMClientID = %q", cfg.AGSIAMClientID)
	}
	if cfg.AGSIAMClientSecret != "client-secret" {
		t.Errorf("AGSIAMClientSecret = %q", cfg.AGSIAMClientSecret)
	}
	if cfg.AGSBaseURL != "https://ags.example.com" {
		t.Errorf("AGSBaseURL = %q", cfg.AGSBaseURL)
	}
	if cfg.AGSNamespace != "playtesthub-dev" {
		t.Errorf("AGSNamespace = %q", cfg.AGSNamespace)
	}
	if cfg.BasePath != "/playtesthub" {
		t.Errorf("BasePath = %q", cfg.BasePath)
	}
}

func TestLoad_OptionalDefaults(t *testing.T) {
	setRequired(t)
	// No optional vars set — expect PRD §5.9 defaults.
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"ReservationTTLSeconds", cfg.ReservationTTLSeconds, 60},
		{"ReclaimIntervalSeconds", cfg.ReclaimIntervalSeconds, 30},
		{"LeaderLeaseTTLSeconds", cfg.LeaderLeaseTTLSeconds, 30},
		{"LeaderHeartbeatSeconds", cfg.LeaderHeartbeatSeconds, 10},
		{"AGSCodeBatchSize", cfg.AGSCodeBatchSize, 1000},
		{"DMTimeoutSeconds", cfg.DMTimeoutSeconds, 5},
		{"DMDrainRatePerSec", cfg.DMDrainRatePerSec, 5},
		{"DBMaxConnections", cfg.DBMaxConnections, 10},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.LogLevel)
	}
	if !cfg.AuthEnabled {
		t.Errorf("AuthEnabled = false, want true (default)")
	}
	if cfg.RefreshIntervalSeconds != 600 {
		t.Errorf("RefreshIntervalSeconds = %d, want 600", cfg.RefreshIntervalSeconds)
	}
}

func TestLoad_OptionalOverrides(t *testing.T) {
	setRequired(t)
	t.Setenv("RESERVATION_TTL_SECONDS", "120")
	t.Setenv("RECLAIM_INTERVAL_SECONDS", "45")
	t.Setenv("LEADER_LEASE_TTL_SECONDS", "90")
	t.Setenv("LEADER_HEARTBEAT_SECONDS", "20")
	t.Setenv("AGS_CODE_BATCH_SIZE", "250")
	t.Setenv("DM_TIMEOUT_SECONDS", "7")
	t.Setenv("DM_DRAIN_RATE_PER_SEC", "12")
	t.Setenv("DB_MAX_CONNECTIONS", "25")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("PLUGIN_GRPC_SERVER_AUTH_ENABLED", "false")
	t.Setenv("REFRESH_INTERVAL", "900")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ReservationTTLSeconds != 120 {
		t.Errorf("ReservationTTLSeconds = %d", cfg.ReservationTTLSeconds)
	}
	if cfg.AGSCodeBatchSize != 250 {
		t.Errorf("AGSCodeBatchSize = %d", cfg.AGSCodeBatchSize)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q", cfg.LogLevel)
	}
	if cfg.AuthEnabled {
		t.Errorf("AuthEnabled = true, want false")
	}
	if cfg.RefreshIntervalSeconds != 900 {
		t.Errorf("RefreshIntervalSeconds = %d", cfg.RefreshIntervalSeconds)
	}
}

func TestLoad_MissingRequired_AggregatesAllNames(t *testing.T) {
	// Do NOT seed anything — every required var is missing.
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DISCORD_BOT_TOKEN", "")
	t.Setenv("AGS_IAM_CLIENT_ID", "")
	t.Setenv("AGS_IAM_CLIENT_SECRET", "")
	t.Setenv("AGS_BASE_URL", "")
	t.Setenv("AGS_NAMESPACE", "")
	t.Setenv("BASE_PATH", "")

	_, err := config.Load()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var missing *config.MissingRequiredError
	if !errors.As(err, &missing) {
		t.Fatalf("expected *MissingRequiredError, got %T: %v", err, err)
	}
	want := []string{
		"AGS_BASE_URL",
		"AGS_IAM_CLIENT_ID",
		"AGS_IAM_CLIENT_SECRET",
		"AGS_NAMESPACE",
		"BASE_PATH",
		"DATABASE_URL",
		"DISCORD_BOT_TOKEN",
	}
	for _, k := range want {
		if !slices.Contains(missing.Keys, k) {
			t.Errorf("missing error does not list %s (got %v)", k, missing.Keys)
		}
	}
}

func TestLoad_MissingOneRequired_ListsOnlyThatOne(t *testing.T) {
	setRequired(t)
	t.Setenv("AGS_NAMESPACE", "")

	_, err := config.Load()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var missing *config.MissingRequiredError
	if !errors.As(err, &missing) {
		t.Fatalf("expected *MissingRequiredError, got %T", err)
	}
	if len(missing.Keys) != 1 || missing.Keys[0] != "AGS_NAMESPACE" {
		t.Fatalf("missing = %v, want [AGS_NAMESPACE]", missing.Keys)
	}
}

func TestLoad_BasePath_MissingLeadingSlash_Error(t *testing.T) {
	setRequired(t)
	t.Setenv("BASE_PATH", "playtesthub")

	_, err := config.Load()
	if err == nil {
		t.Fatalf("expected error for bad BASE_PATH, got nil")
	}
	if !strings.Contains(err.Error(), "BASE_PATH") {
		t.Errorf("error does not mention BASE_PATH: %v", err)
	}
}

func TestLoad_BadInt_Error(t *testing.T) {
	setRequired(t)
	t.Setenv("RESERVATION_TTL_SECONDS", "not-a-number")

	_, err := config.Load()
	if err == nil {
		t.Fatalf("expected error for bad int, got nil")
	}
	if !strings.Contains(err.Error(), "RESERVATION_TTL_SECONDS") {
		t.Errorf("error does not mention RESERVATION_TTL_SECONDS: %v", err)
	}
}

func TestMissingRequiredError_Message(t *testing.T) {
	err := &config.MissingRequiredError{Keys: []string{"A", "B"}}
	msg := err.Error()
	if !strings.Contains(msg, "A") || !strings.Contains(msg, "B") {
		t.Errorf("error message %q does not list keys", msg)
	}
}
