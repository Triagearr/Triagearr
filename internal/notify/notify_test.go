package notify_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Triagearr/Triagearr/internal/notify"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// fakeNotifier records the events it receives and optionally fails. Its routing
// is configurable so dispatch filtering can be exercised.
type fakeNotifier struct {
	name    string
	err     error
	routing notify.Routing
	got     []notify.Event
}

func (f *fakeNotifier) Name() string            { return f.name }
func (f *fakeNotifier) Routing() notify.Routing { return f.routing }
func (f *fakeNotifier) Send(_ context.Context, ev notify.Event) error {
	f.got = append(f.got, ev)
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

	// A failing provider must not stop the others. This report has a hard
	// failure among successes → run.partial.
	for _, n := range []*fakeNotifier{ok1, broken, ok2} {
		if len(n.got) != 1 {
			t.Fatalf("notifier %q: got %d events, want 1", n.name, len(n.got))
		}
		if n.got[0].Kind != notify.EventRunPartial {
			t.Errorf("notifier %q: kind = %q, want run.partial", n.name, n.got[0].Kind)
		}
	}
}

func TestDispatchRoutingFilters(t *testing.T) {
	// info floor sees an info run; warning floor does not.
	allItems := []notify.ReportItem{
		{Name: "a", Hash: "a", SizeBytes: 1024, Status: triagearr.ActionSucceeded},
	}
	infoRun := notify.Report{VolumeName: "v", Items: allItems} // all-succeeded → run.executed (info)

	low := &fakeNotifier{name: "low", routing: notify.Routing{MinSeverity: notify.SeverityInfo}}
	high := &fakeNotifier{name: "high", routing: notify.Routing{MinSeverity: notify.SeverityWarning}}
	muted := &fakeNotifier{name: "muted", routing: notify.Routing{Mute: map[notify.EventKind]bool{notify.EventRunExecuted: true}}}

	d := notify.NewDispatcher(low, high, muted)
	d.Dispatch(context.Background(), infoRun)

	if len(low.got) != 1 {
		t.Errorf("info-floor provider should receive the info event, got %d", len(low.got))
	}
	if len(high.got) != 0 {
		t.Errorf("warning-floor provider should not receive the info event, got %d", len(high.got))
	}
	if len(muted.got) != 0 {
		t.Errorf("provider muting run.executed should not receive it, got %d", len(muted.got))
	}
}

func TestDispatchAlertFansOut(t *testing.T) {
	ok1 := &fakeNotifier{name: "ok1"}
	broken := &fakeNotifier{name: "broken", err: errors.New("boom")}

	d := notify.NewDispatcher(ok1, broken)
	d.DispatchAlert(context.Background(), sampleAlert())

	for _, n := range []*fakeNotifier{ok1, broken} {
		if len(n.got) != 1 || n.got[0].Kind != notify.EventTargetUnreachable {
			t.Fatalf("notifier %q: got %+v, want one target_unreachable event", n.name, n.got)
		}
	}
}

func TestDispatchHealthFansOut(t *testing.T) {
	fn := &fakeNotifier{name: "fn"}
	d := notify.NewDispatcher(fn)
	d.DispatchHealth(context.Background(), notify.HealthEvent{Component: "sonarr", Kind: "arr", Healthy: false, LastError: "refused"})
	if len(fn.got) != 1 || fn.got[0].Kind != notify.EventHealthDegraded {
		t.Fatalf("want one health.degraded event, got %+v", fn.got)
	}
	if fn.got[0].Health == nil || fn.got[0].Health.Component != "sonarr" {
		t.Errorf("health payload not propagated: %+v", fn.got[0].Health)
	}
}

func TestDispatchEmptyIsNoop(t *testing.T) {
	d := notify.NewDispatcher()
	if !d.Empty() {
		t.Fatal("expected Empty() true for no notifiers")
	}
	d.Dispatch(context.Background(), sampleReport())     // must not panic
	d.DispatchAlert(context.Background(), sampleAlert()) // must not panic

	var nilDisp *notify.Dispatcher
	if !nilDisp.Empty() {
		t.Fatal("expected Empty() true for nil dispatcher")
	}
	nilDisp.Dispatch(context.Background(), sampleReport()) // must not panic
}

func TestRunOutcome(t *testing.T) {
	mk := func(statuses ...triagearr.ActionStatus) notify.Report {
		r := notify.Report{VolumeName: "v"}
		for i, s := range statuses {
			r.Items = append(r.Items, notify.ReportItem{Hash: triagearr.Hash(string(rune('a' + i))), SizeBytes: 1024, Status: s})
		}
		return r
	}
	tests := []struct {
		name string
		r    notify.Report
		want notify.EventKind
	}{
		{"empty", mk(), ""},
		{"all ok", mk(triagearr.ActionSucceeded, triagearr.ActionSucceeded), notify.EventRunExecuted},
		{"none ok", mk(triagearr.ActionFailedQbit), notify.EventRunFailed},
		{"mixed", mk(triagearr.ActionSucceeded, triagearr.ActionFailedQbit), notify.EventRunPartial},
		{"cross-seed skip is benign", mk(triagearr.ActionSucceeded, triagearr.ActionSkippedCrossSeed), notify.EventRunExecuted},
		{"only cross-seed skip counts as failed (no success)", mk(triagearr.ActionSkippedCrossSeed), notify.EventRunFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := notify.RunOutcome(tt.r); got != tt.want {
				t.Errorf("RunOutcome = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatRun(t *testing.T) {
	out := notify.FormatRun(sampleReport())
	if out.Kind != notify.EventRunPartial {
		t.Fatalf("kind = %q, want run.partial", out.Kind)
	}
	if out.Severity != notify.SeverityWarning {
		t.Errorf("severity = %v, want warning", out.Severity)
	}
	if out.Run == nil {
		t.Error("typed Run payload should be set")
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
			t.Errorf("FormatRun output missing %q\n--- got ---\n%s", want, out.Text)
		}
	}
}

func TestFormatRunFallsBackToHash(t *testing.T) {
	r := notify.Report{
		VolumeName: "v",
		Items: []notify.ReportItem{
			{Hash: "deadbeef", SizeBytes: 1024, Status: triagearr.ActionSucceeded},
		},
	}
	if !strings.Contains(notify.FormatRun(r).Text, "deadbeef") {
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

func TestFormatHealth(t *testing.T) {
	down := notify.FormatHealth(notify.HealthEvent{Component: "qbittorrent", Kind: "torrent_client", Healthy: false, LastError: "timeout"})
	if down.Kind != notify.EventHealthDegraded || down.Severity != notify.SeverityError {
		t.Errorf("degraded: kind/severity = %q/%v", down.Kind, down.Severity)
	}
	if !strings.Contains(down.Text, "timeout") {
		t.Errorf("degraded text should include the error: %q", down.Text)
	}
	up := notify.FormatHealth(notify.HealthEvent{Component: "qbittorrent", Kind: "torrent_client", Healthy: true})
	if up.Kind != notify.EventHealthRecovered || up.Severity != notify.SeverityInfo {
		t.Errorf("recovered: kind/severity = %q/%v", up.Kind, up.Severity)
	}
}

func TestSeverityRoundTrip(t *testing.T) {
	for _, s := range []notify.Severity{notify.SeverityInfo, notify.SeverityWarning, notify.SeverityError} {
		got, err := notify.ParseSeverity(s.String())
		if err != nil || got != s {
			t.Errorf("round-trip %v: got %v err %v", s, got, err)
		}
	}
	if _, err := notify.ParseSeverity("bogus"); err == nil {
		t.Error("expected error for unknown severity")
	}
	if got, _ := notify.ParseSeverity(""); got != notify.SeverityInfo {
		t.Error("empty severity should default to info")
	}
}

func TestCatalogue(t *testing.T) {
	cat := notify.Catalogue()
	if len(cat) == 0 {
		t.Fatal("catalogue is empty")
	}
	for _, e := range cat {
		if e.Severity != e.Kind.Severity() {
			t.Errorf("catalogue severity mismatch for %q", e.Kind)
		}
	}
}

func TestSendTest(t *testing.T) {
	t.Run("empty dispatcher errors", func(t *testing.T) {
		if err := notify.NewDispatcher().SendTest(context.Background()); err == nil {
			t.Fatal("expected error for no providers")
		}
	})
	t.Run("delivers to providers bypassing routing", func(t *testing.T) {
		// A provider with an error floor would normally drop a test (info), but
		// SendTest bypasses routing.
		fn := &fakeNotifier{name: "tg", routing: notify.Routing{MinSeverity: notify.SeverityError}}
		if err := notify.NewDispatcher(fn).SendTest(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(fn.got) != 1 || fn.got[0].Kind != notify.EventTest {
			t.Fatalf("expected one test event, got %+v", fn.got)
		}
		if !strings.Contains(fn.got[0].Text, "test notification") {
			t.Errorf("test event body unexpected: %q", fn.got[0].Text)
		}
	})
	t.Run("surfaces provider failure", func(t *testing.T) {
		broken := &fakeNotifier{name: "tg", err: errors.New("bad token")}
		err := notify.NewDispatcher(broken).SendTest(context.Background())
		if err == nil || !strings.Contains(err.Error(), "tg: bad token") {
			t.Fatalf("expected provider-prefixed error, got %v", err)
		}
	})
	t.Run("targets a single provider", func(t *testing.T) {
		a := &fakeNotifier{name: "a"}
		b := &fakeNotifier{name: "b"}
		d := notify.NewDispatcher(a, b)
		if err := d.SendTestEvent(context.Background(), notify.TestOptions{Provider: "b", Kind: notify.EventHealthDegraded}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(a.got) != 0 {
			t.Errorf("provider a should not be targeted, got %d", len(a.got))
		}
		if len(b.got) != 1 || b.got[0].Kind != notify.EventHealthDegraded {
			t.Fatalf("provider b should get a health.degraded sample, got %+v", b.got)
		}
	})
	t.Run("unknown provider errors", func(t *testing.T) {
		a := &fakeNotifier{name: "a"}
		err := notify.NewDispatcher(a).SendTestEvent(context.Background(), notify.TestOptions{Provider: "nope"})
		if err == nil {
			t.Fatal("expected error for unknown provider")
		}
	})
}

func TestDeliveriesRing(t *testing.T) {
	fn := &fakeNotifier{name: "fn"}
	broken := &fakeNotifier{name: "broken", err: errors.New("boom")}
	d := notify.NewDispatcher(fn, broken)
	d.DispatchHealth(context.Background(), notify.HealthEvent{Component: "sonarr", Healthy: false})

	dels := d.Deliveries()
	if len(dels) != 2 {
		t.Fatalf("want 2 deliveries, got %d", len(dels))
	}
	// Both deliveries are for the same kind; one ok, one failed.
	var sawOK, sawFail bool
	for _, del := range dels {
		if del.Kind != notify.EventHealthDegraded {
			t.Errorf("unexpected kind %q", del.Kind)
		}
		if del.OK {
			sawOK = true
		} else if del.Err != "" {
			sawFail = true
		}
	}
	if !sawOK || !sawFail {
		t.Errorf("expected one ok and one failed delivery, got %+v", dels)
	}
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
