// Package discord wraps the narrow slice of the Discord API playtesthub
// needs: bot-token handle lookup at signup (PRD §10 M1) and outbound
// DM delivery (PRD §10 M3 / docs/dm-queue.md).
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL  = "https://discord.com/api/v10"
	defaultLookupTO = 5 * time.Second
	defaultSendTO   = 10 * time.Second
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

// SendDM opens a DM channel to a Discord snowflake and posts a single
// message. Two-call dance per the Discord REST API:
//  1. POST /users/@me/channels {recipient_id} → channel id;
//  2. POST /channels/{id}/messages {content}.
//
// Errors are surfaced verbatim so docs/dm-queue.md's `lastDmError`
// vocabulary stays small: 429 carries the Retry-After hint, 5xx the
// status, and any other non-2xx the trimmed body. The repo truncates
// the persisted reason to 500 UTF-8-safe bytes — callers do not need
// to pre-trim. Implements dmqueue.Sender.
func (c *BotClient) SendDM(ctx context.Context, discordUserID, message string) error {
	if c == nil {
		return fmt.Errorf("discord: bot client not configured")
	}
	if discordUserID == "" {
		return fmt.Errorf("discord: empty recipient id")
	}
	if message == "" {
		return fmt.Errorf("discord: empty message")
	}

	channelID, err := c.openDMChannel(ctx, discordUserID)
	if err != nil {
		return err
	}
	return c.postMessage(ctx, channelID, message)
}

func (c *BotClient) openDMChannel(ctx context.Context, discordUserID string) (string, error) {
	body, err := json.Marshal(map[string]string{"recipient_id": discordUserID})
	if err != nil {
		return "", fmt.Errorf("discord: marshal open-dm body: %w", err)
	}
	resp, err := c.doSend(ctx, "/users/@me/channels", body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if rerr := classifyDMResponse(resp); rerr != nil {
		return "", rerr
	}

	var ch struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ch); err != nil {
		return "", fmt.Errorf("discord: decode open-dm response: %w", err)
	}
	if ch.ID == "" {
		return "", fmt.Errorf("discord: open-dm response missing channel id")
	}
	return ch.ID, nil
}

func (c *BotClient) postMessage(ctx context.Context, channelID, message string) error {
	body, err := json.Marshal(map[string]string{"content": message})
	if err != nil {
		return fmt.Errorf("discord: marshal message body: %w", err)
	}
	resp, err := c.doSend(ctx, "/channels/"+url.PathEscape(channelID)+"/messages", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return classifyDMResponse(resp)
}

func (c *BotClient) doSend(ctx context.Context, path string, body []byte) (*http.Response, error) {
	endpoint := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("discord: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bot "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "playtesthub (+https://github.com/anggorodewanto/playtesthub, v1)")

	client := c.http
	if client.Timeout == 0 || client.Timeout > defaultSendTO {
		// Lookup defaults to a 5s timeout, which is fine for sends; only
		// inflate when an injected client opted out of timeouts.
		clone := *client
		clone.Timeout = defaultSendTO
		client = &clone
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discord: do request: %w", err)
	}
	return resp, nil
}

// classifyDMResponse converts a non-2xx Discord response into a
// terse error string the queue persists into `last_dm_error`. Honours
// the `Retry-After` header on 429 so an operator running RetryDM gets
// a hint without parsing the JSON body.
func classifyDMResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	trimmed := strings.TrimSpace(string(body))
	if resp.StatusCode == http.StatusTooManyRequests {
		retry := strings.TrimSpace(resp.Header.Get("Retry-After"))
		if retry == "" {
			retry = "unknown"
		} else if _, err := strconv.ParseFloat(retry, 64); err != nil {
			retry = "unknown"
		}
		return fmt.Errorf("discord: rate limited (retry_after=%s): %s", retry, trimmed)
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("discord: upstream %d: %s", resp.StatusCode, trimmed)
	}
	return fmt.Errorf("discord: unexpected status %d: %s", resp.StatusCode, trimmed)
}
