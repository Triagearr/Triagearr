package webhook_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Triagearr/Triagearr/internal/notify"
	"github.com/Triagearr/Triagearr/internal/notify/webhook"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func TestSendStructuredJSON(t *testing.T) {
	var gotBody []byte
	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotSig = r.Header.Get("X-Triagearr-Signature")
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q", ct)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	const secret = "s3cr3t"
	c, err := webhook.New(webhook.Options{URL: srv.URL, Secret: secret})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ev := notify.FormatHealth(notify.HealthEvent{Component: "sonarr", Kind: "arr", Healthy: false, LastError: "refused"})
	if err := c.Send(context.Background(), ev); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var payload struct {
		Kind     string              `json:"kind"`
		Severity string              `json:"severity"`
		Health   *notify.HealthEvent `json:"health"`
	}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if payload.Kind != "health.degraded" || payload.Severity != "error" {
		t.Errorf("kind/severity = %q/%q", payload.Kind, payload.Severity)
	}
	if payload.Health == nil || payload.Health.Component != "sonarr" {
		t.Errorf("structured health payload missing: %+v", payload.Health)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(gotBody)
	if want := hex.EncodeToString(mac.Sum(nil)); gotSig != want {
		t.Errorf("signature = %q, want %q", gotSig, want)
	}
}

func TestSendNoSecretNoSignature(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Triagearr-Signature") != "" {
			t.Error("no signature expected without a secret")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c, _ := webhook.New(webhook.Options{URL: srv.URL})
	if err := c.Send(context.Background(), notify.FormatAlert(notify.Alert{VolumeName: "v"})); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func TestSendTransientOn5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c, _ := webhook.New(webhook.Options{URL: srv.URL})
	err := c.Send(context.Background(), notify.FormatAlert(notify.Alert{VolumeName: "v"}))
	if !errors.Is(err, triagearr.ErrTransient) {
		t.Fatalf("5xx should be transient, got %v", err)
	}
}

func TestSendHardOn4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c, _ := webhook.New(webhook.Options{URL: srv.URL})
	err := c.Send(context.Background(), notify.FormatAlert(notify.Alert{VolumeName: "v"}))
	if err == nil || errors.Is(err, triagearr.ErrTransient) {
		t.Fatalf("4xx should be a hard failure, got %v", err)
	}
}

func TestNewRequiresURL(t *testing.T) {
	if _, err := webhook.New(webhook.Options{}); err == nil {
		t.Error("empty URL should error")
	}
}
