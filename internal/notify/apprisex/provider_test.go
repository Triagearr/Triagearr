package apprisex_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Triagearr/Triagearr/internal/notify"
	"github.com/Triagearr/Triagearr/internal/notify/apprisex"
)

func TestProviderSendsText(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	// ntfy posts the message body to {server}/{topic}; point it at httptest.
	host := strings.TrimPrefix(srv.URL, "http://")
	serviceURL, err := apprisex.NtfyURL("http://"+host, "triagearr-test", "", "")
	if err != nil {
		t.Fatalf("building URL: %v", err)
	}
	p, err := apprisex.New("ntfy", serviceURL, notify.Routing{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.Name() != "ntfy" {
		t.Errorf("name = %q", p.Name())
	}

	ev := notify.FormatAlert(notify.Alert{VolumeName: "media", Mode: "live"})
	if err := p.Send(context.Background(), ev); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.Contains(gotBody, "target unreachable") {
		t.Errorf("ntfy body should carry the event text, got %q", gotBody)
	}
}

func TestNewRejectsBadURL(t *testing.T) {
	if _, err := apprisex.New("x", "definitely-not-a-valid-scheme://nope", notify.Routing{}); err == nil {
		t.Error("expected error for unsupported scheme")
	}
	if _, err := apprisex.New("x", "", notify.Routing{}); err == nil {
		t.Error("expected error for empty service URL")
	}
}

func TestProviderRoutingPassThrough(t *testing.T) {
	r := notify.Routing{MinSeverity: notify.SeverityWarning, Mute: map[notify.EventKind]bool{notify.EventRunExecuted: true}}
	url, err := apprisex.NtfyURL("ntfy.sh", "topic", "", "")
	if err != nil {
		t.Fatalf("building URL: %v", err)
	}
	p, err := apprisex.New("ntfy", url, r)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := p.Routing(); got.MinSeverity != notify.SeverityWarning || !got.Mute[notify.EventRunExecuted] {
		t.Errorf("routing not preserved: %+v", got)
	}
}
