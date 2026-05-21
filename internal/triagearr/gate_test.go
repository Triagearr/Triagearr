package triagearr_test

import (
	"testing"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func TestResolveRunMode(t *testing.T) {
	cases := []struct {
		name       string
		daemonLive bool
		trigger    triagearr.RunTrigger
		requested  bool
		want       triagearr.RunMode
	}{
		{"dry daemon + pressure", false, triagearr.RunTriggerDiskPressure, false, triagearr.RunModeDryRun},
		{"dry daemon + http opt-in ignored", false, triagearr.RunTriggerHTTP, true, triagearr.RunModeDryRun},
		{"dry daemon + cli opt-in ignored", false, triagearr.RunTriggerCLI, true, triagearr.RunModeDryRun},

		{"live daemon + pressure auto", true, triagearr.RunTriggerDiskPressure, false, triagearr.RunModeLive},
		{"live daemon + http no opt-in", true, triagearr.RunTriggerHTTP, false, triagearr.RunModeDryRun},
		{"live daemon + http opt-in", true, triagearr.RunTriggerHTTP, true, triagearr.RunModeLive},
		{"live daemon + cli no opt-in", true, triagearr.RunTriggerCLI, false, triagearr.RunModeDryRun},
		{"live daemon + cli opt-in", true, triagearr.RunTriggerCLI, true, triagearr.RunModeLive},

		{"unknown trigger always dry-run", true, triagearr.RunTrigger("cron"), true, triagearr.RunModeDryRun},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := triagearr.ResolveRunMode(tc.daemonLive, tc.trigger, tc.requested)
			if got != tc.want {
				t.Fatalf("ResolveRunMode(daemonLive=%v, trigger=%q, requested=%v) = %q, want %q",
					tc.daemonLive, tc.trigger, tc.requested, got, tc.want)
			}
		})
	}
}
