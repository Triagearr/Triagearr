package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/Triagearr/Triagearr/internal/logging"
)

func newJSONLogger(buf *bytes.Buffer) *slog.Logger {
	inner := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(logging.NewRedactHandler(inner))
}

func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("decoding log line: %v\nraw: %s", err, buf.String())
	}
	return m
}

func TestRedact_ApiKeyAttr(t *testing.T) {
	var buf bytes.Buffer
	newJSONLogger(&buf).Info("hi", "api_key", "deadbeef")
	got := decode(t, &buf)
	if got["api_key"] != "***" {
		t.Fatalf("api_key not redacted: %v", got["api_key"])
	}
}

func TestRedact_PasswordAttr_DotPath(t *testing.T) {
	var buf bytes.Buffer
	newJSONLogger(&buf).Info("hi", "qbit.password", "p@ssw0rd")
	got := decode(t, &buf)
	if got["qbit.password"] != "***" {
		t.Fatalf("qbit.password not redacted: %v", got["qbit.password"])
	}
}

func TestRedact_BotTokenSubstring(t *testing.T) {
	var buf bytes.Buffer
	newJSONLogger(&buf).Info("hi", "telegram_bot_token", "xyz")
	got := decode(t, &buf)
	if got["telegram_bot_token"] != "***" {
		t.Fatalf("bot_token not redacted: %v", got["telegram_bot_token"])
	}
}

func TestRedact_NonSecretAttrUnchanged(t *testing.T) {
	var buf bytes.Buffer
	newJSONLogger(&buf).Info("hi", "user", "nikita")
	got := decode(t, &buf)
	if got["user"] != "nikita" {
		t.Fatalf("benign attr was mangled: %v", got["user"])
	}
}

func TestRedact_QueryStringInValue(t *testing.T) {
	var buf bytes.Buffer
	url := "http://sonarr.local/api/v3/health?apikey=topsecret&foo=bar"
	newJSONLogger(&buf).Info("hi", "url", url)
	got := decode(t, &buf)
	val, _ := got["url"].(string)
	if strings.Contains(val, "topsecret") {
		t.Fatalf("apikey value leaked through: %q", val)
	}
	if !strings.Contains(val, "apikey=***") {
		t.Fatalf("apikey placeholder missing: %q", val)
	}
	if !strings.Contains(val, "foo=bar") {
		t.Fatalf("non-secret query param lost: %q", val)
	}
}

func TestRedact_GroupedSecret(t *testing.T) {
	var buf bytes.Buffer
	newJSONLogger(&buf).Info("hi", slog.Group("arr",
		slog.String("api_key", "shh"),
		slog.String("name", "sonarr"),
	))
	got := decode(t, &buf)
	group, _ := got["arr"].(map[string]any)
	if group == nil {
		t.Fatalf("group missing: %v", got)
	}
	if group["api_key"] != "***" {
		t.Fatalf("grouped api_key not redacted: %v", group["api_key"])
	}
	if group["name"] != "sonarr" {
		t.Fatalf("grouped non-secret mangled: %v", group["name"])
	}
}

func TestRedact_WithAttrs_BoundSecret(t *testing.T) {
	var buf bytes.Buffer
	logger := newJSONLogger(&buf).With("api_key", "leak")
	logger.Info("hi")
	got := decode(t, &buf)
	if got["api_key"] != "***" {
		t.Fatalf("bound api_key not redacted: %v", got["api_key"])
	}
}

func TestRedact_HandlerImplementsEnabled(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := logging.NewRedactHandler(inner)
	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("Enabled should defer to inner handler's level filter")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Fatal("Enabled should allow Error level")
	}
}
