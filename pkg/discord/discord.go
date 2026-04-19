// Package discord wraps the narrow slice of the Discord API playtesthub
// needs. M1 only: bot-token handle lookup at signup (PRD §10 M1). The DM
// worker lands in M2.
package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultBaseURL  = "https://discord.com/api/v10"
	defaultLookupTO = 5 * time.Second
)

// HandleLookup resolves a Discord snowflake to a human-readable handle.
// Production implementation hits Discord's REST API; tests inject a fake.
type HandleLookup interface {
	LookupHandle(ctx context.Context, discordID string) (string, error)
}

// BotClient is a bot-token-authed Discord client. Reuses a single
// http.Client so callers get connection pooling and timeout control.
type BotClient struct {
	token   string
	baseURL string
	http    *http.Client
}

// NewBotClient constructs a BotClient. An empty token returns nil so
// callers can treat Discord as "not configured" without nil-check
// branches at every callsite — Signup falls back to the raw Discord ID.
func NewBotClient(token string) *BotClient {
	if token == "" {
		return nil
	}
	return &BotClient{
		token:   token,
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: defaultLookupTO},
	}
}

// WithBaseURL overrides the Discord API root. Tests use it to point at a
// local httptest.Server.
func (c *BotClient) WithBaseURL(base string) *BotClient {
	c.baseURL = strings.TrimRight(base, "/")
	return c
}

// WithHTTPClient overrides the HTTP client. Tests inject a client with a
// short timeout or a RoundTripper that records requests.
func (c *BotClient) WithHTTPClient(h *http.Client) *BotClient {
	c.http = h
	return c
}

type discordUser struct {
	Username      string `json:"username"`
	Discriminator string `json:"discriminator"`
	GlobalName    string `json:"global_name"`
}

// LookupHandle calls `GET /users/{discordID}`. Returns the best
// human-readable handle Discord offers: `global_name` when present (the
// post-pomelo display name), else `username`, optionally appended with
// `#discriminator` for legacy accounts that still carry one.
//
// Non-200 responses return an error; the service layer treats any error
// as "fall back to raw Discord ID" per PRD §10 M1.
func (c *BotClient) LookupHandle(ctx context.Context, discordID string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("discord: bot client not configured")
	}
	if discordID == "" {
		return "", fmt.Errorf("discord: empty discord id")
	}

	endpoint := fmt.Sprintf("%s/users/%s", c.baseURL, url.PathEscape(discordID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("discord: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bot "+c.token)
	req.Header.Set("User-Agent", "playtesthub (+https://github.com/anggorodewanto/playtesthub, v1)")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("discord: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("discord: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var u discordUser
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return "", fmt.Errorf("discord: decode response: %w", err)
	}
	return formatHandle(u), nil
}

func formatHandle(u discordUser) string {
	if u.GlobalName != "" {
		return u.GlobalName
	}
	if u.Discriminator != "" && u.Discriminator != "0" {
		return u.Username + "#" + u.Discriminator
	}
	return u.Username
}
