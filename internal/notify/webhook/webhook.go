// Package webhook is the native (non-shoutrrr) notification provider: it
// serialises the typed notify.Event to structured JSON and POSTs it to an
// operator-supplied URL (ADR-0033). This is the one provider that reads the
// event's typed payload rather than its plain text, so automation consumers
// (n8n, Home Assistant, custom scripts) get machine-readable fields instead of
// a pre-rendered string. An optional shared secret signs the body
// (HMAC-SHA256) so the receiver can verify authenticity.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Triagearr/Triagearr/internal/notify"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// signatureHeader carries the lowercase hex HMAC-SHA256 of the request body,
// keyed by the configured secret. Absent when no secret is configured.
const signatureHeader = "X-Triagearr-Signature"

// Options configures a webhook Client.
type Options struct {
	URL     string
	Secret  string // optional HMAC-SHA256 key over the request body
	Timeout time.Duration
	Routing notify.Routing
}

// Client is a notify.Notifier that POSTs structured JSON.
type Client struct {
	url     string
	secret  string
	routing notify.Routing
	http    *http.Client
}

// New constructs a webhook Client. URL is required.
func New(opts Options) (*Client, error) {
	if opts.URL == "" {
		return nil, fmt.Errorf("webhook: URL is required")
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		url:     opts.URL,
		secret:  opts.Secret,
		routing: opts.Routing,
		http:    &http.Client{Timeout: timeout},
	}, nil
}

// Name implements notify.Notifier.
func (c *Client) Name() string { return "webhook" }

// Routing implements notify.Notifier.
func (c *Client) Routing() notify.Routing { return c.routing }

// payload is the structured wire format. Only the payload matching kind is set.
type payload struct {
	Kind     string              `json:"kind"`
	Severity string              `json:"severity"`
	Title    string              `json:"title"`
	Text     string              `json:"text"`
	Run      *notify.Report      `json:"run,omitempty"`
	Alert    *notify.Alert       `json:"alert,omitempty"`
	Health   *notify.HealthEvent `json:"health,omitempty"`
}

// Send implements notify.Notifier. A 5xx response or transport error is wrapped
// with triagearr.ErrTransient; a 4xx is a hard failure the caller cannot fix by
// retrying.
func (c *Client) Send(ctx context.Context, ev notify.Event) error {
	body, err := json.Marshal(payload{
		Kind:     string(ev.Kind),
		Severity: ev.Severity.String(),
		Title:    ev.Title,
		Text:     ev.Text,
		Run:      ev.Run,
		Alert:    ev.Alert,
		Health:   ev.Health,
	})
	if err != nil {
		return fmt.Errorf("webhook: encoding payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.secret != "" {
		mac := hmac.New(sha256.New, []byte(c.secret))
		mac.Write(body)
		req.Header.Set(signatureHeader, hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: POST: %w: %w", err, triagearr.ErrTransient)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode >= 500 {
		return fmt.Errorf("webhook: HTTP %d: %s: %w", resp.StatusCode, string(msg), triagearr.ErrTransient)
	}
	return fmt.Errorf("webhook: HTTP %d: %s", resp.StatusCode, string(msg))
}
