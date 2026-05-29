package triagearr_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func TestTrackerStatus_String(t *testing.T) {
	cases := map[triagearr.TrackerStatus]string{
		triagearr.TrackerDisabled:     "disabled",
		triagearr.TrackerNotContacted: "not_contacted",
		triagearr.TrackerWorking:      "working",
		triagearr.TrackerUpdating:     "updating",
		triagearr.TrackerNotWorking:   "not_working",
	}
	for status, want := range cases {
		require.Equal(t, want, status.String())
	}
	// Out-of-range values fall through to the labelled-unknown form.
	require.Equal(t, "unknown(99)", triagearr.TrackerStatus(99).String())
}
