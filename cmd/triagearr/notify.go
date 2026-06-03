package main

import (
	"log/slog"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/notify"
	"github.com/Triagearr/Triagearr/internal/notify/apprisex"
	"github.com/Triagearr/Triagearr/internal/notify/webhook"
)

// buildNotifier assembles the notification dispatcher from config (ADR-0033).
// The human channels (Telegram/Discord/ntfy/Email/Slack) are built into Apprise
// service URLs and delivered through the apprisex adapter; Webhook is the native
// structured-JSON provider. A provider that fails to construct (bad credential,
// malformed URL) is logged and skipped — it must not prevent the daemon from
// starting. An all-disabled config yields an empty, no-op dispatcher.
func buildNotifier(cfg config.NotificationsConfig) *notify.Dispatcher {
	var notifiers []notify.Notifier

	// add registers an Apprise-backed provider, building its service URL from the
	// channel's structured fields. A build/parse failure logs and skips.
	add := func(name string, routing config.ProviderRouting, build func() (string, error)) {
		url, err := build()
		if err != nil {
			slog.Error("notifications: provider disabled", "provider", name, "err", err)
			return
		}
		p, err := apprisex.New(name, url, parseRouting(routing))
		if err != nil {
			slog.Error("notifications: provider disabled", "provider", name, "err", err)
			return
		}
		notifiers = append(notifiers, p)
		slog.Info("notifications: provider enabled", "provider", name)
	}

	if c := cfg.Telegram; c.Enabled {
		add("telegram", c.ProviderRouting, func() (string, error) {
			return apprisex.TelegramURL(c.BotToken, c.ChatID)
		})
	}
	if c := cfg.Discord; c.Enabled {
		add("discord", c.ProviderRouting, func() (string, error) {
			return apprisex.DiscordURL(c.WebhookURL)
		})
	}
	if c := cfg.Ntfy; c.Enabled {
		add("ntfy", c.ProviderRouting, func() (string, error) {
			return apprisex.NtfyURL(c.Server, c.Topic, c.Username, c.Password)
		})
	}
	if c := cfg.Email; c.Enabled {
		add("email", c.ProviderRouting, func() (string, error) {
			return apprisex.EmailURL(c.Host, c.Port, c.Username, c.Password, c.From, c.To, c.UseStartTLS)
		})
	}
	if c := cfg.Slack; c.Enabled {
		add("slack", c.ProviderRouting, func() (string, error) {
			return apprisex.SlackURL(c.WebhookURL)
		})
	}
	if c := cfg.Webhook; c.Enabled {
		wh, err := webhook.New(webhook.Options{URL: c.URL, Secret: c.Secret, Routing: parseRouting(c.ProviderRouting)})
		if err != nil {
			slog.Error("notifications: provider disabled", "provider", "webhook", "err", err)
		} else {
			notifiers = append(notifiers, wh)
			slog.Info("notifications: provider enabled", "provider", "webhook")
		}
	}

	return notify.NewDispatcher(notifiers...)
}

// parseRouting converts a config routing block into a notify.Routing. Validation
// already rejected unknown severities/kinds (config.Validate), so a parse error
// here falls back to the permissive default rather than failing the daemon.
func parseRouting(r config.ProviderRouting) notify.Routing {
	sev, _ := notify.ParseSeverity(r.MinSeverity)
	var mute map[notify.EventKind]bool
	if len(r.Mute) > 0 {
		mute = make(map[notify.EventKind]bool, len(r.Mute))
		for _, k := range r.Mute {
			mute[notify.EventKind(k)] = true
		}
	}
	return notify.Routing{MinSeverity: sev, Mute: mute}
}
