package telegram

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Triagearr/Triagearr/internal/notify"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func sampleReport() notify.Report {
	return notify.Report{
		VolumeName: "media",
		Mode:       "live",
		RunID:      7,
		Items: []notify.ReportItem{
			{Name: "Show.S01", Hash: "abc", SizeBytes: 1024, Status: triagearr.ActionSucceeded},
		},
	}
}

func TestNewValidation(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		ok   bool
	}{
		{"missing token", Options{ChatID: "1"}, false},
		{"missing chat", Options{BotToken: "t"}, false},
		{"valid", Options{BotToken: "t", ChatID: "1"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.opts)
			if tt.ok && err != nil {
				t.Fatalf("New: unexpected error %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("New: expected error, got nil")
			}
		})
	}
}

func TestSend(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		wantErr   bool
		transient bool
	}{
		{"ok", http.StatusOK, false, false},
		{"server error is transient", http.StatusInternalServerError, true, true},
		{"bad token is hard failure", http.StatusUnauthorized, true, false},
		{"bad request is hard failure", http.StatusBadRequest, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath, gotBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				b, _ := io.ReadAll(r.Body)
				gotBody = string(b)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"ok":false}`))
			}))
			t.Cleanup(srv.Close)

			c, err := New(Options{BotToken: "secret-token", ChatID: "12345", baseURL: srv.URL})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			err = c.Send(context.Background(), notify.FormatRunReport(sampleReport()))

			if tt.wantErr && err == nil {
				t.Fatal("Send: expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Send: unexpected error %v", err)
			}
			if tt.wantErr {
				isTransient := errors.Is(err, triagearr.ErrTransient)
				if isTransient != tt.transient {
					t.Errorf("Send: transient=%v, want %v (err: %v)", isTransient, tt.transient, err)
				}
			}
			if tt.status == http.StatusOK {
				if gotPath != "/botsecret-token/sendMessage" {
					t.Errorf("request path = %q, want /botsecret-token/sendMessage", gotPath)
				}
				if !strings.Contains(gotBody, `"chat_id":"12345"`) {
					t.Errorf("request body missing chat_id: %s", gotBody)
				}
				if !strings.Contains(gotBody, "disk pressure") {
					t.Errorf("request body missing formatted text: %s", gotBody)
				}
			}
		})
	}
}

func TestSendTransportErrorIsTransient(t *testing.T) {
	c, err := New(Options{BotToken: "t", ChatID: "1", baseURL: "http://127.0.0.1:0"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = c.Send(context.Background(), notify.FormatRunReport(sampleReport()))
	if err == nil {
		t.Fatal("expected transport error")
	}
	if !errors.Is(err, triagearr.ErrTransient) {
		t.Errorf("transport error should be transient: %v", err)
	}
}
