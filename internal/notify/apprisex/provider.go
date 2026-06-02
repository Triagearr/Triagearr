// Package apprisex adapts unraid/apprise-go into a notify.Notifier (ADR-0033).
// Apprise is a text sink with semantic notification types: each human channel
// (Telegram, Discord, ntfy, email, Slack) is reached by building an Apprise
// service URL from structured config fields and sending the event's plain-text
// body plus a title and a NotifyType derived from the event severity. Apprise
// renders that type per service (ntfy priority, Discord embed colour, …), so we
// do not hand-craft rich payloads here — that is the native webhook's job. The
// URL carries secrets, so it is built only at wiring time and never logged or
// surfaced to the UI.
package apprisex

import (
	"context"
	"fmt"

	"github.com/unraid/apprise-go"

	"github.com/Triagearr/Triagearr/internal/notify"
)

// Provider is a notify.Notifier backed by a single Apprise service URL.
type Provider struct {
	name    string
	client  *apprise.Apprise
	routing notify.Routing
}

// New constructs a Provider for the named channel from a built service URL.
// Apprise validates the URL (scheme + structure) at Add time, so a malformed
// URL fails fast at daemon wiring rather than on first send.
func New(name, serviceURL string, routing notify.Routing) (*Provider, error) {
	if serviceURL == "" {
		return nil, fmt.Errorf("apprise %s: empty service URL", name)
	}
	client := apprise.New()
	if err := client.Add(serviceURL); err != nil {
		return nil, fmt.Errorf("apprise %s: %w", name, err)
	}
	return &Provider{name: name, client: client, routing: routing}, nil
}

// Name implements notify.Notifier.
func (p *Provider) Name() string { return p.name }

// Routing implements notify.Notifier.
func (p *Provider) Routing() notify.Routing { return p.routing }

// Send implements notify.Notifier. Apprise is text-only at our seam, so the
// event's plain-text body is sent with the title and a NotifyType mapped from
// severity. Apprise returns a joined error across the (single) target URL; it
// does not classify transient vs permanent, so errors are returned as-is — the
// Dispatcher is best-effort and never retries regardless.
func (p *Provider) Send(_ context.Context, ev notify.Event) error {
	return p.client.Send(ev.Text,
		apprise.WithTitle(ev.Title),
		apprise.WithNotifyType(notifyType(ev.Severity)),
	)
}

// notifyType maps a Triagearr severity onto an Apprise semantic type so each
// service can render it natively (colour, priority, icon).
func notifyType(s notify.Severity) apprise.NotifyType {
	switch s {
	case notify.SeverityError:
		return apprise.NotifyFailure
	case notify.SeverityWarning:
		return apprise.NotifyWarning
	default:
		return apprise.NotifyInfo
	}
}
