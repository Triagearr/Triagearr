package main

import (
	"testing"

	"github.com/Triagearr/Triagearr/internal/config"
)

func TestBuildNotifier(t *testing.T) {
	t.Run("all disabled yields an empty dispatcher", func(t *testing.T) {
		d := buildNotifier(config.NotificationsConfig{})
		if d == nil {
			t.Fatal("dispatcher is nil")
		}
		if !d.Empty() {
			t.Error("Empty() = false, want true when no provider is enabled")
		}
	})

	t.Run("an enabled provider yields a non-empty dispatcher", func(t *testing.T) {
		cfg := config.NotificationsConfig{}
		cfg.Webhook.Enabled = true
		cfg.Webhook.URL = "https://example.test/hook"

		d := buildNotifier(cfg)
		if d.Empty() {
			t.Error("Empty() = true, want false when webhook is enabled with a valid URL")
		}
	})

	t.Run("a provider with bad config is skipped without panicking", func(t *testing.T) {
		// An enabled-but-unconfigured provider (webhook needs a URL) must be
		// logged and skipped, never fatal: one broken provider can't take the
		// daemon down. With no other provider, the dispatcher ends up empty.
		cfg := config.NotificationsConfig{}
		cfg.Webhook.Enabled = true
		cfg.Webhook.URL = ""

		d := buildNotifier(cfg)
		if d == nil {
			t.Fatal("dispatcher is nil")
		}
		if !d.Empty() {
			t.Error("Empty() = false, want true when the only provider fails to build")
		}
	})
}
