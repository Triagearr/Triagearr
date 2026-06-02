// Package notify delivers operator-facing notifications when Triagearr
// executes a destructive run. Per ADR-0021 the only notified event is a
// disk-pressure run that actually reached the Actor: manual HTTP/CLI runs are
// deliberately silent (the operator triggered them knowingly).
//
// A Report is the provider-agnostic payload. Concrete providers (Telegram in
// internal/notify/telegram, more later) implement Notifier; the Dispatcher
// fans a Report out to every configured provider best-effort — a provider
// failure is logged, never propagated, so a broken bot token cannot abort or
// taint a run.
package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

// Report is the payload describing one executed disk-pressure run. Byte
// counts for free space come from a real statfs sample before (at fire time)
// and after the Actor finished — never inferred from media sizes.
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

// testText is the synthetic connectivity-check body for the dashboard
// "send test" action (see Dispatcher.SendTest).
const testText = "Triagearr — test notification\n" +
	"If you can read this, notifications are wired up correctly."

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

// EventKind discriminates the notification events. Providers are plain-text
// sinks (ADR-0032): the kind is carried for logging and lets a future provider
// branch on event type without re-plumbing the Dispatcher.
type EventKind string

const (
	EventRunReport         EventKind = "run_report"
	EventTargetUnreachable EventKind = "target_unreachable"
	EventTest              EventKind = "test"
)

// Message is the provider-facing payload: a kind tag plus the already-formatted
// plain text. All event-specific formatting happens in this package
// (FormatRunReport / FormatAlert), so providers never see structured events.
type Message struct {
	Kind EventKind
	Text string
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

// Notifier delivers one preformatted Message to a single provider.
type Notifier interface {
	// Send delivers the message. A 5xx/timeout failure is wrapped with
	// triagearr.ErrTransient so callers could retry; the Dispatcher does not.
	Send(ctx context.Context, m Message) error
	// Name identifies the provider in logs (e.g. "telegram").
	Name() string
}

// Dispatcher fans a Report out to every configured Notifier.
type Dispatcher struct {
	notifiers []Notifier
}

// NewDispatcher builds a Dispatcher over the given notifiers. A nil/empty
// slice yields a Dispatcher whose Dispatch is a no-op.
func NewDispatcher(notifiers ...Notifier) *Dispatcher {
	return &Dispatcher{notifiers: notifiers}
}

// Empty reports whether no providers are configured.
func (d *Dispatcher) Empty() bool {
	return d == nil || len(d.notifiers) == 0
}

// Dispatch delivers an executed-run report to every provider, best-effort.
func (d *Dispatcher) Dispatch(ctx context.Context, r Report) {
	d.send(ctx, FormatRunReport(r))
}

// DispatchAlert delivers a target-unreachable alert to every provider,
// best-effort (ADR-0032). Same advisory contract as Dispatch.
func (d *Dispatcher) DispatchAlert(ctx context.Context, a Alert) {
	d.send(ctx, FormatAlert(a))
}

// send fans a Message out to every notifier best-effort. Each failure is logged
// and swallowed: notifications are advisory and must never affect run outcome.
func (d *Dispatcher) send(ctx context.Context, m Message) {
	if d == nil {
		return
	}
	for _, n := range d.notifiers {
		if err := n.Send(ctx, m); err != nil {
			slog.Warn("notification delivery failed", "provider", n.Name(), "kind", string(m.Kind), "err", err)
			continue
		}
		slog.Info("notification delivered", "provider", n.Name(), "kind", string(m.Kind))
	}
}

// SendTest delivers a synthetic connectivity-check message to every configured
// provider. Unlike send it surfaces failures (joined, prefixed by provider
// name) so the dashboard "send test" action can show the operator a bad token.
func (d *Dispatcher) SendTest(ctx context.Context) error {
	if d.Empty() {
		return errors.New("no notification provider is enabled")
	}
	m := Message{Kind: EventTest, Text: testText}
	var errs []error
	for _, n := range d.notifiers {
		if err := n.Send(ctx, m); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", n.Name(), err))
		}
	}
	return errors.Join(errs...)
}

// FormatRunReport renders an executed-run Report as a plain-text Message. No
// markup is used so torrent names containing Markdown/HTML metacharacters need
// no escaping.
func FormatRunReport(r Report) Message {
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
	return Message{Kind: EventRunReport, Text: strings.TrimRight(b.String(), "\n")}
}

// FormatAlert renders a target-unreachable Alert as a plain-text Message
// (ADR-0032). It states the gap to target, what a run could actually reclaim,
// and the residual shortfall the operator cannot close by deleting more.
func FormatAlert(a Alert) Message {
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
	return Message{Kind: EventTargetUnreachable, Text: b.String()}
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
