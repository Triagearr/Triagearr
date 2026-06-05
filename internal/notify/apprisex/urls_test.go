package apprisex_test

import (
	"strings"
	"testing"

	"github.com/unraid/apprise-go"

	"github.com/Triagearr/Triagearr/internal/notify/apprisex"
)

// assertAppriseParses confirms apprise accepts the built URL — the ultimate
// correctness check for escaping and structure.
func assertAppriseParses(t *testing.T, url string) {
	t.Helper()
	if err := apprise.New().Add(url); err != nil {
		t.Fatalf("apprise rejected built URL %q: %v", url, err)
	}
}

func TestTelegramURL(t *testing.T) {
	got, err := apprisex.TelegramURL("123456789:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsaw", "-1001234567890")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(got, "tgram://") {
		t.Errorf("scheme wrong: %q", got)
	}
	assertAppriseParses(t, got)
}

func TestTelegramURLErrors(t *testing.T) {
	if _, err := apprisex.TelegramURL("", "c"); err == nil {
		t.Error("empty token should error")
	}
	if _, err := apprisex.TelegramURL("tok", ""); err == nil {
		t.Error("empty chat should error")
	}
	if _, err := apprisex.TelegramURL("no-colon-token", "c"); err == nil {
		t.Error("malformed token should error")
	}
}

func TestDiscordURL(t *testing.T) {
	got, err := apprisex.DiscordURL("https://discord.com/api/webhooks/111222333/AbC-token_XyZ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "111222333") {
		t.Errorf("webhook id missing: %q", got)
	}
	assertAppriseParses(t, got)
}

func TestDiscordURLErrors(t *testing.T) {
	if _, err := apprisex.DiscordURL("https://discord.com/api/webhooks/onlyid"); err == nil {
		t.Error("missing token segment should error")
	}
}

func TestNtfyURL(t *testing.T) {
	got, err := apprisex.NtfyURL("https://ntfy.example.com", "my-topic", "user", "p@ss:word")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Password special chars must be escaped.
	if strings.Contains(got, "p@ss:word") {
		t.Errorf("password not escaped: %q", got)
	}
	assertAppriseParses(t, got)

	// Bare host, http prefix → insecure ntfy scheme.
	httpURL, err := apprisex.NtfyURL("http://10.0.0.5:8080", "topic", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(httpURL, "ntfy://") {
		t.Errorf("http prefix should select insecure ntfy scheme: %q", httpURL)
	}
	assertAppriseParses(t, httpURL)
}

func TestNtfyURLErrors(t *testing.T) {
	if _, err := apprisex.NtfyURL("ntfy.sh", "", "", ""); err == nil {
		t.Error("empty topic should error")
	}
}

func TestEmailURL(t *testing.T) {
	got, err := apprisex.EmailURL("smtp.example.com", 587, "user@example.com", "secret", "triagearr@example.com", []string{"a@example.com", "b@example.com"}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(got, "mailtos://") {
		t.Errorf("StartTLS should select mailtos: %q", got)
	}
	assertAppriseParses(t, got)

	insecure, err := apprisex.EmailURL("smtp.example.com", 25, "u", "p", "from@x.com", []string{"to@x.com"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(insecure, "mailto://") {
		t.Errorf("no StartTLS should select mailto: %q", insecure)
	}
	assertAppriseParses(t, insecure)
}

func TestEmailURLErrors(t *testing.T) {
	if _, err := apprisex.EmailURL("", 0, "", "", "from@x", []string{"to@x"}, true); err == nil {
		t.Error("missing host should error")
	}
	if _, err := apprisex.EmailURL("h", 0, "", "", "", []string{"to@x"}, true); err == nil {
		t.Error("missing from should error")
	}
	if _, err := apprisex.EmailURL("h", 0, "", "", "from@x", nil, true); err == nil {
		t.Error("missing recipients should error")
	}
}

func TestSlackURL(t *testing.T) {
	got, err := apprisex.SlackURL("https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertAppriseParses(t, got)
}

func TestSlackURLErrors(t *testing.T) {
	if _, err := apprisex.SlackURL("https://hooks.slack.com/services/T0/B0"); err == nil {
		t.Error("too few token segments should error")
	}
}
