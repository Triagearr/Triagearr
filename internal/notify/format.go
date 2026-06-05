package notify

import (
	"fmt"
	"strings"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// ReportItem is one torrent the Actor attempted to delete during the run.
type ReportItem struct {
	Name      string
	Hash      triagearr.Hash
	SizeBytes int64
	Status    triagearr.ActionStatus
}

// Succeeded reports whether the candidate was fully deleted (*arr + qBit).
func (it ReportItem) Succeeded() bool {
	return it.Status == triagearr.ActionSucceeded
}

// hardFailed reports whether the item failed in a way that counts against the
// run outcome. A cross-seed skip is benign (the torrent was intentionally left
// because siblings still hardlink it) and is excluded from the failure tally.
func (it ReportItem) hardFailed() bool {
	return !it.Succeeded() && it.Status != triagearr.ActionSkippedCrossSeed
}

// Report is the payload describing one executed disk-pressure run. Byte counts
// for free space come from a real statfs sample before (at fire time) and after
// the Actor finished — never inferred from media sizes.
type Report struct {
	VolumeName      string
	Mode            string
	RunID           int64
	FreePctBefore   float64
	FreeBytesBefore uint64
	FreePctAfter    float64
	FreeBytesAfter  uint64
	TargetFreePct   float64
	Items           []ReportItem
	// TotalFreedBytes is the sum of freed_bytes across succeeded actions —
	// the "a priori" freed total, distinct from the before/after disk delta.
	TotalFreedBytes int64
	// RealFreedBytes is the observed disk delta (FreeBytesAfter -
	// FreeBytesBefore) measured by the post-action statfs re-sample. Signed:
	// concurrent writes from other processes during the action window can
	// produce a negative value. Zero when no after-sample was taken.
	RealFreedBytes int64
}

// SucceededCount returns how many items were fully deleted.
func (r Report) SucceededCount() int {
	n := 0
	for _, it := range r.Items {
		if it.Succeeded() {
			n++
		}
	}
	return n
}

// hardFailedCount returns how many items failed in a non-benign way.
func (r Report) hardFailedCount() int {
	n := 0
	for _, it := range r.Items {
		if it.hardFailed() {
			n++
		}
	}
	return n
}

// Alert is the payload for the "disk-pressure target is unreachable" event
// (ADR-0032): even after deleting every eligible candidate the volume would not
// reach target_free_percent.
type Alert struct {
	VolumeName       string
	Mode             string
	FreePct          float64
	TargetFreePct    float64
	NeedBytes        int64 // bytes to free to reach target
	ReclaimableBytes int64 // bytes a run would actually free (eligible candidates)
	CandidateCount   int
}

// testText is the synthetic connectivity-check body for the generic "send test"
// action (see Dispatcher.SendTest).
const testText = "Triagearr — test notification\n" +
	"If you can read this, notifications are wired up correctly."

// RunOutcome classifies a Report into its run.* kind. An empty plan yields the
// empty kind so the caller can drop it. Classification is on succeeded-vs-hard-
// failed: a cross-seed skip is benign and never demotes the outcome.
func RunOutcome(r Report) EventKind {
	if len(r.Items) == 0 {
		return ""
	}
	switch {
	case r.SucceededCount() == 0:
		return EventRunFailed
	case r.hardFailedCount() > 0:
		return EventRunPartial
	default:
		return EventRunExecuted
	}
}

// FormatRun renders an executed-run Report as a typed Event. No markup is used
// so torrent names containing Markdown/HTML metacharacters need no escaping.
// The kind/severity follow the run outcome (RunOutcome).
func FormatRun(r Report) Event {
	kind := RunOutcome(r)
	if kind == "" {
		return Event{}
	}
	rep := r
	var b strings.Builder
	fmt.Fprintf(&b, "Triagearr — disk pressure on %q\n", r.VolumeName)
	fmt.Fprintf(&b, "Free space: %.1f%% -> %.1f%% (target %.1f%%)\n",
		r.FreePctBefore, r.FreePctAfter, r.TargetFreePct)
	// disk free bytes physically cannot exceed int64 max (9.2 EB)
	fmt.Fprintf(&b, "Disk free: %s -> %s\n",
		HumanBytes(int64(r.FreeBytesBefore)), HumanBytes(int64(r.FreeBytesAfter))) //nolint:gosec // bounded disk free bytes
	fmt.Fprintf(&b, "Run #%d · %s mode\n\n", r.RunID, r.Mode)

	fmt.Fprintf(&b, "Deleted %d/%d items, %s freed:\n",
		r.SucceededCount(), len(r.Items), HumanBytes(r.TotalFreedBytes))
	if r.RealFreedBytes != 0 && r.TotalFreedBytes > 0 {
		fmt.Fprintf(&b, "Disk delta: %s (claimed %s)\n",
			HumanBytes(r.RealFreedBytes), HumanBytes(r.TotalFreedBytes))
	}
	for _, it := range r.Items {
		name := it.Name
		if name == "" {
			name = string(it.Hash)
		}
		fmt.Fprintf(&b, "  • %s — %s [%s]\n", name, HumanBytes(it.SizeBytes), itemMark(it.Status))
	}
	return Event{
		Kind:     kind,
		Severity: kind.Severity(),
		Title:    runTitle(kind, r),
		Text:     strings.TrimRight(b.String(), "\n"),
		Run:      &rep,
	}
}

// runTitle is the short one-liner for providers with a title slot.
func runTitle(kind EventKind, r Report) string {
	switch kind {
	case EventRunFailed:
		return fmt.Sprintf("Triagearr — run failed on %q", r.VolumeName)
	case EventRunPartial:
		return fmt.Sprintf("Triagearr — run partial on %q", r.VolumeName)
	default:
		return fmt.Sprintf("Triagearr — disk pressure on %q", r.VolumeName)
	}
}

// FormatAlert renders a target-unreachable Alert as a typed Event (ADR-0032).
// It states the gap to target, what a run could actually reclaim, and the
// residual shortfall the operator cannot close by deleting more.
func FormatAlert(a Alert) Event {
	al := a
	shortfall := a.NeedBytes - a.ReclaimableBytes
	if shortfall < 0 {
		shortfall = 0
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Triagearr — disk-pressure target unreachable on %q\n", a.VolumeName)
	fmt.Fprintf(&b, "Free space: %.1f%% (target %.1f%%)\n", a.FreePct, a.TargetFreePct)
	fmt.Fprintf(&b, "Need %s to reach target; only %s reclaimable from %d candidate(s).\n",
		HumanBytes(a.NeedBytes), HumanBytes(a.ReclaimableBytes), a.CandidateCount)
	fmt.Fprintf(&b, "Shortfall: %s — not enough eligible content to delete.\n", HumanBytes(shortfall))
	fmt.Fprintf(&b, "Mode: %s", a.Mode)
	return Event{
		Kind:     EventTargetUnreachable,
		Severity: EventTargetUnreachable.Severity(),
		Title:    fmt.Sprintf("Triagearr — target unreachable on %q", a.VolumeName),
		Text:     b.String(),
		Alert:    &al,
	}
}

// FormatHealth renders a connection-health transition as a typed Event.
func FormatHealth(h HealthEvent) Event {
	he := h
	kind := EventHealthDegraded
	if h.Healthy {
		kind = EventHealthRecovered
	}
	var b strings.Builder
	if h.Healthy {
		fmt.Fprintf(&b, "Triagearr — %s recovered\n", h.Component)
		fmt.Fprintf(&b, "The %s connection is reachable again.", h.Component)
	} else {
		fmt.Fprintf(&b, "Triagearr — %s unreachable\n", h.Component)
		if h.LastError != "" {
			fmt.Fprintf(&b, "Last error: %s", h.LastError)
		} else {
			b.WriteString("The connection's health check is failing.")
		}
	}
	title := fmt.Sprintf("Triagearr — %s unreachable", h.Component)
	if h.Healthy {
		title = fmt.Sprintf("Triagearr — %s recovered", h.Component)
	}
	return Event{
		Kind:     kind,
		Severity: kind.Severity(),
		Title:    title,
		Text:     b.String(),
		Health:   &he,
	}
}

// sampleEvent builds a representative event for the per-event dashboard test.
// An empty kind (or "test") yields the generic connectivity check.
func sampleEvent(kind EventKind) Event {
	switch kind {
	case EventRunExecuted, EventRunPartial, EventRunFailed:
		return FormatRun(sampleReport(kind))
	case EventTargetUnreachable:
		return FormatAlert(Alert{
			VolumeName: "media", Mode: "live",
			FreePct: 4.2, TargetFreePct: 15,
			NeedBytes: 120 << 30, ReclaimableBytes: 30 << 30, CandidateCount: 3,
		})
	case EventHealthDegraded:
		return FormatHealth(HealthEvent{Component: "sonarr", Kind: "arr", Healthy: false, LastError: "dial tcp: connection refused"})
	case EventHealthRecovered:
		return FormatHealth(HealthEvent{Component: "sonarr", Kind: "arr", Healthy: true})
	default:
		return Event{Kind: EventTest, Severity: SeverityInfo, Title: "Triagearr — test notification", Text: testText}
	}
}

// sampleReport crafts a Report whose item statuses produce the requested run
// outcome, for the per-event test.
func sampleReport(kind EventKind) Report {
	base := Report{
		VolumeName: "media", Mode: "live", RunID: 0,
		FreePctBefore: 8, FreeBytesBefore: 80 << 30,
		FreePctAfter: 15, FreeBytesAfter: 150 << 30,
		TargetFreePct: 15, TotalFreedBytes: 70 << 30, RealFreedBytes: 68 << 30,
	}
	ok := ReportItem{Name: "Example.Movie.2024.2160p", Hash: "abc123", SizeBytes: 40 << 30, Status: triagearr.ActionSucceeded}
	failed := ReportItem{Name: "Example.Show.S01.1080p", Hash: "def456", SizeBytes: 30 << 30, Status: triagearr.ActionFailedQbit}
	switch kind {
	case EventRunFailed:
		base.Items = []ReportItem{failed}
		base.TotalFreedBytes = 0
	case EventRunPartial:
		base.Items = []ReportItem{ok, failed}
	default:
		base.Items = []ReportItem{ok}
	}
	return base
}

// itemMark renders an action status as a short tag for the message body.
func itemMark(s triagearr.ActionStatus) string {
	switch s {
	case triagearr.ActionSucceeded:
		return "ok"
	case triagearr.ActionAbortedArrFail:
		return "failed: arr"
	case triagearr.ActionAbortedNlinkCheck:
		return "aborted: nlink check"
	case triagearr.ActionFailedQbit:
		return "failed: qbit"
	case triagearr.ActionSkippedCrossSeed:
		return "skipped: cross-seed"
	default:
		return string(s)
	}
}

// HumanBytes renders a byte count in binary units with one decimal place.
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
