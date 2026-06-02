package notify_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Triagearr/Triagearr/internal/notify"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// fakeNotifier records the messages it receives and optionally fails.
type fakeNotifier struct {
	name string
	err  error
	got  []notify.Message
}

func (f *fakeNotifier) Name() string { return f.name }
func (f *fakeNotifier) Send(_ context.Context, m notify.Message) error {
	f.got = append(f.got, m)
	return f.err
}

func sampleReport() notify.Report {
	return notify.Report{
		VolumeName:      "media",
		Mode:            "live",
		RunID:           42,
		FreePctBefore:   7.8,
		FreeBytesBefore: 80 * 1024 * 1024 * 1024,
		FreePctAfter:    14.2,
		FreeBytesAfter:  145 * 1024 * 1024 * 1024,
		TargetFreePct:   15,
		TotalFreedBytes: 38 * 1024 * 1024 * 1024,
		Items: []notify.ReportItem{
			{Name: "Show.S01", Hash: "aaa", SizeBytes: 12 * 1024 * 1024 * 1024, Status: triagearr.ActionSucceeded},
			{Name: "Movie.2021", Hash: "bbb", SizeBytes: 26 * 1024 * 1024 * 1024, Status: triagearr.ActionSucceeded},
			{Name: "Other", Hash: "ccc", SizeBytes: 4 * 1024 * 1024 * 1024, Status: triagearr.ActionFailedQbit},
		},
	}
}

func TestDispatchFansOutBestEffort(t *testing.T) {
	ok1 := &fakeNotifier{name: "ok1"}
	broken := &fakeNotifier{name: "broken", err: errors.New("boom")}
	ok2 := &fakeNotifier{name: "ok2"}

	d := notify.NewDispatcher(ok1, broken, ok2)
	d.Dispatch(context.Background(), sampleReport())

	// A failing provider must not stop the others.
	for _, n := range []*fakeNotifier{ok1, broken, ok2} {
		if len(n.got) != 1 {
			t.Fatalf("notifier %q: got %d messages, want 1", n.name, len(n.got))
		}
		if n.got[0].Kind != notify.EventRunReport {
			t.Errorf("notifier %q: kind = %q, want run_report", n.name, n.got[0].Kind)
		}
	}
}

func TestDispatchAlertFansOut(t *testing.T) {
	ok1 := &fakeNotifier{name: "ok1"}
	broken := &fakeNotifier{name: "broken", err: errors.New("boom")}

	d := notify.NewDispatcher(ok1, broken)
	d.DispatchAlert(context.Background(), sampleAlert())

	for _, n := range []*fakeNotifier{ok1, broken} {
		if len(n.got) != 1 || n.got[0].Kind != notify.EventTargetUnreachable {
			t.Fatalf("notifier %q: got %+v, want one target_unreachable message", n.name, n.got)
		}
	}
}

func TestDispatchEmptyIsNoop(t *testing.T) {
	d := notify.NewDispatcher()
	if !d.Empty() {
		t.Fatal("expected Empty() true for no notifiers")
	}
	d.Dispatch(context.Background(), sampleReport())   // must not panic
	d.DispatchAlert(context.Background(), sampleAlert()) // must not panic

	var nilDisp *notify.Dispatcher
	if !nilDisp.Empty() {
		t.Fatal("expected Empty() true for nil dispatcher")
	}
	nilDisp.Dispatch(context.Background(), sampleReport()) // must not panic
}

func TestFormatRunReport(t *testing.T) {
	out := notify.FormatRunReport(sampleReport())
	if out.Kind != notify.EventRunReport {
		t.Fatalf("kind = %q, want run_report", out.Kind)
	}
	wantSubstrings := []string{
		`disk pressure on "media"`,
		"7.8% -> 14.2%",
		"target 15.0%",
		"Run #42 · live mode",
		"Deleted 2/3 items",
		"Show.S01 — 12.0 GiB [ok]",
		"Other — 4.0 GiB [failed: qbit]",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(out.Text, want) {
			t.Errorf("FormatRunReport output missing %q\n--- got ---\n%s", want, out.Text)
		}
	}
}

func TestFormatRunReportFallsBackToHash(t *testing.T) {
	r := notify.Report{
		VolumeName: "v",
		Items: []notify.ReportItem{
			{Hash: "deadbeef", SizeBytes: 1024, Status: triagearr.ActionSucceeded},
		},
	}
	if !strings.Contains(notify.FormatRunReport(r).Text, "deadbeef") {
		t.Error("expected hash fallback when item name is empty")
	}
}

func sampleAlert() notify.Alert {
	return notify.Alert{
		VolumeName:       "media",
		Mode:             "dry-run",
		FreePct:          5.0,
		TargetFreePct:    20.0,
		NeedBytes:        46_600_000_000,
		ReclaimableBytes: 28_400_000_000,
		CandidateCount:   12,
	}
}

func TestFormatAlert(t *testing.T) {
	out := notify.FormatAlert(sampleAlert())
	if out.Kind != notify.EventTargetUnreachable {
		t.Fatalf("kind = %q, want target_unreachable", out.Kind)
	}
	wantSubstrings := []string{
		`target unreachable on "media"`,
		"Free space: 5.0% (target 20.0%)",
		"from 12 candidate(s)",
		"Shortfall:",
		"Mode: dry-run",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(out.Text, want) {
			t.Errorf("FormatAlert output missing %q\n--- got ---\n%s", want, out.Text)
		}
	}
}

func TestSendTest(t *testing.T) {
	t.Run("empty dispatcher errors", func(t *testing.T) {
		if err := notify.NewDispatcher().SendTest(context.Background()); err == nil {
			t.Fatal("expected error for no providers")
		}
	})
	t.Run("delivers to providers", func(t *testing.T) {
		fn := &fakeNotifier{name: "tg"}
		if err := notify.NewDispatcher(fn).SendTest(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(fn.got) != 1 || fn.got[0].Kind != notify.EventTest {
			t.Fatalf("expected one test message, got %+v", fn.got)
		}
		if !strings.Contains(fn.got[0].Text, "test notification") {
			t.Errorf("test message body unexpected: %q", fn.got[0].Text)
		}
	})
	t.Run("surfaces provider failure", func(t *testing.T) {
		broken := &fakeNotifier{name: "tg", err: errors.New("bad token")}
		err := notify.NewDispatcher(broken).SendTest(context.Background())
		if err == nil || !strings.Contains(err.Error(), "tg: bad token") {
			t.Fatalf("expected provider-prefixed error, got %v", err)
		}
	})
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{5 * 1024 * 1024 * 1024, "5.0 GiB"},
	}
	for _, tt := range tests {
		if got := notify.HumanBytes(tt.in); got != tt.want {
			t.Errorf("HumanBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
