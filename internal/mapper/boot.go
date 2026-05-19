package mapper

import (
	"context"
	"fmt"
	"log/slog"
)

// BootInputs feeds [Boot] with everything it needs to settle the resolver.
// The arrays of samples are gathered by the caller (cmd/triagearr) from the
// live qBit + *arr polls (or from snapshots if a previous poll exists).
type BootInputs struct {
	VolumeName      string
	Root            string
	ManualRules     []ManualRule
	IndexMaxEntries int
	QbitSamples     []Sample
	ArrSamples      []Sample
}

// BootResult is the per-volume outcome of [Boot].
type BootResult struct {
	VolumeName string
	Rules      []Rule
	Inference  *InferenceResult // nil when manual rules covered the volume
}

// Boot runs the boot procedure from ADR-0010 for one volume. When manual rules
// are configured, inference is skipped and the manual rules are validated.
// Otherwise the volume is indexed and samples are matched against it.
//
// Returns a non-nil error when:
//   - manual rules fail validation (missing `to:`),
//   - no manual rules AND inference fails to reach the acceptance gate.
//
// The caller (cmd/triagearr serve) is expected to log the failure with the
// candidate distribution and exit non-zero — refuse-to-start.
func Boot(ctx context.Context, in BootInputs) (BootResult, error) {
	res := BootResult{VolumeName: in.VolumeName}

	if len(in.ManualRules) > 0 {
		rules, err := ValidateManual(in.ManualRules)
		if err != nil {
			return res, fmt.Errorf("volume %s: %w", in.VolumeName, err)
		}
		res.Rules = rules
		return res, nil
	}

	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	idx, err := BuildIndex(in.VolumeName, in.Root, in.IndexMaxEntries)
	if err != nil {
		return res, fmt.Errorf("volume %s: indexing %s: %w", in.VolumeName, in.Root, err)
	}
	slog.Info("mapper index built", "volume", in.VolumeName, "root", in.Root, "entries", idx.Entries)

	samples := append([]Sample{}, in.QbitSamples...)
	samples = append(samples, in.ArrSamples...)
	inf := Infer(idx, samples)
	res.Inference = &inf
	if !inf.Accepted {
		return res, fmt.Errorf("volume %s: path remap inference failed (samples_total=%d matched=%d) — see candidate distribution in logs, set volumes[*].path_remap as a manual override",
			in.VolumeName, inf.SamplesTotal, inf.SamplesMatched)
	}
	res.Rules = []Rule{inf.Rule}
	return res, nil
}
