// Package telegram delivers Triagearr notifications via the Telegram Bot API.
// It calls the sendMessage endpoint with net/http only — no SDK dependency
// (ADR-0021).
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Triagearr/Triagearr/internal/notify"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// apiBase is the Telegram Bot API root. Overridable in tests.
const apiBase = "https://api.telegram.org"

// Options configures a Telegram Client.
type Options struct {
	BotToken string
	ChatID   string
	Timeout  time.Duration
	// baseURL overrides apiBase in tests; empty uses the real API.
	baseURL string
}

// Client is a notify.Notifier backed by the Telegram Bot API.
type Client struct {
	token   string
	chatID  string
	baseURL string
	http    *http.Client
}

// New constructs a Telegram client. BotToken and ChatID are required.
func New(opts Options) (*Client, error) {
	if opts.BotToken == "" {
		return nil, errors.New("telegram: BotToken is required")
	}
	if opts.ChatID == "" {
		return nil, errors.New("telegram: ChatID is required")
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	base := opts.baseURL
	if base == "" {
		base = apiBase
	}
	return &Client{
		token:   opts.BotToken,
		chatID:  opts.ChatID,
		baseURL: base,
		http:    &http.Client{Timeout: timeout},
	}, nil
}

// Name implements notify.Notifier.
func (c *Client) Name() string { return "telegram" }

type sendMessageRequest struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

// Send implements notify.Notifier. A 5xx response or transport error is
// wrapped with triagearr.ErrTransient; a 4xx is a hard failure (bad token,
// unknown chat) the caller cannot fix by retrying.
func (c *Client) Send(ctx context.Context, r notify.Report) error {
	payload, err := json.Marshal(sendMessageRequest{
		ChatID: c.chatID,
		Text:   notify.FormatText(r),
	})
	if err != nil {
		return fmt.Errorf("telegram: encoding sendMessage: %w", err)
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", c.baseURL, c.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("telegram: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// Transport failures (timeout, connection reset) are transient.
		return fmt.Errorf("telegram: POST sendMessage: %w: %w", err, triagearr.ErrTransient)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode >= 500 {
		return fmt.Errorf("telegram: sendMessage HTTP %d: %s: %w",
			resp.StatusCode, string(body), triagearr.ErrTransient)
	}
	return fmt.Errorf("telegram: sendMessage HTTP %d: %s", resp.StatusCode, string(body))
}
