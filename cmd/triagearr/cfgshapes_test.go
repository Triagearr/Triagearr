package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/notify"
	"github.com/Triagearr/Triagearr/internal/pollers"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func TestParseRouting(t *testing.T) {
	tests := []struct {
		name     string
		in       config.ProviderRouting
		wantSev  notify.Severity
		wantMute map[notify.EventKind]bool
	}{
		{
			name:    "empty falls back to permissive info floor",
			in:      config.ProviderRouting{},
			wantSev: notify.SeverityInfo,
		},
		{
			name:    "warning floor parsed",
			in:      config.ProviderRouting{MinSeverity: "warning"},
			wantSev: notify.SeverityWarning,
		},
		{
			// Validation already rejects unknown severities; if one slips
			// through, parseRouting must not fail the daemon — it falls back.
			name:    "unknown severity falls back to info",
			in:      config.ProviderRouting{MinSeverity: "bogus"},
			wantSev: notify.SeverityInfo,
		},
		{
			name:     "mute list populated",
			in:       config.ProviderRouting{Mute: []string{"run.partial", "test"}},
			wantSev:  notify.SeverityInfo,
			wantMute: map[notify.EventKind]bool{notify.EventRunPartial: true, notify.EventTest: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRouting(tt.in)
			if got.MinSeverity != tt.wantSev {
				t.Errorf("MinSeverity = %v, want %v", got.MinSeverity, tt.wantSev)
			}
			if len(got.Mute) != len(tt.wantMute) {
				t.Fatalf("Mute = %v, want %v", got.Mute, tt.wantMute)
			}
			for k, v := range tt.wantMute {
				if got.Mute[k] != v {
					t.Errorf("Mute[%q] = %v, want %v", k, got.Mute[k], v)
				}
			}
		})
	}
}

func TestArrURLMap(t *testing.T) {
	cfg := &config.Config{}
	cfg.Arrs.Sonarr.URL = "http://sonarr:8989"
	cfg.Arrs.Radarr.URL = "http://radarr:7878"
	cfg.Arrs.Lidarr.URL = "http://lidarr:8686"
	cfg.Arrs.WhisparrV2.URL = "http://whisparr2:6969"
	cfg.Arrs.WhisparrV3.URL = "http://whisparr3:6969"

	got := arrURLMap(cfg)

	want := map[triagearr.ArrType]string{
		triagearr.ArrTypeSonarr:     "http://sonarr:8989",
		triagearr.ArrTypeRadarr:     "http://radarr:7878",
		triagearr.ArrTypeLidarr:     "http://lidarr:8686",
		triagearr.ArrTypeWhisparrV2: "http://whisparr2:6969",
		triagearr.ArrTypeWhisparrV3: "http://whisparr3:6969",
	}
	if len(got) != len(want) {
		t.Fatalf("map has %d entries, want %d: %v", len(got), len(want), got)
	}
	for typ, url := range want {
		if got[pollers.URLKey(typ)] != url {
			t.Errorf("URL for %s = %q, want %q", typ, got[pollers.URLKey(typ)], url)
		}
	}
}

func TestPressureRule(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*config.Config)
		wantOK bool
	}{
		{
			name:   "disabled yields no rule",
			mutate: func(c *config.Config) { c.Volume.DiskPressure.Enabled = false },
			wantOK: false,
		},
		{
			name: "enabled but no threshold yields no rule",
			mutate: func(c *config.Config) {
				c.Volume.DiskPressure.Enabled = true
				c.Volume.DiskPressure.ThresholdFreePercent = 0
			},
			wantOK: false,
		},
		{
			name: "enabled with threshold yields a rule",
			mutate: func(c *config.Config) {
				c.Volume.Name = "media"
				c.Volume.Path = "/data"
				c.Volume.DiskPressure.Enabled = true
				c.Volume.DiskPressure.ThresholdFreePercent = 10
				c.Volume.DiskPressure.TargetFreePercent = 20
			},
			wantOK: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			tt.mutate(cfg)
			rule, ok := pressureRule(cfg)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok {
				if rule.Name != cfg.Volume.Name || rule.Path != cfg.Volume.Path {
					t.Errorf("rule = %+v, want name/path from cfg", rule)
				}
				if rule.ThresholdFreePercent != cfg.Volume.DiskPressure.ThresholdFreePercent ||
					rule.TargetFreePercent != cfg.Volume.DiskPressure.TargetFreePercent {
					t.Errorf("rule percents = %+v, want from cfg", rule)
				}
			}
		})
	}
}

func TestTheVolume(t *testing.T) {
	cfg := &config.Config{}
	cfg.Volume.Name = "media"
	cfg.Volume.Path = "/data"
	cfg.Volume.DiskPressure.TargetFreePercent = 15

	got := theVolume(cfg)
	if got.Name != "media" || got.Path != "/data" || got.TargetFreePercent != 15 {
		t.Errorf("theVolume = %+v, want {media /data 15}", got)
	}
}

func TestEnabledVolume(t *testing.T) {
	t.Run("disabled yields no volume", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Volume.DiskPressure.Enabled = false
		if _, ok := enabledVolume(cfg); ok {
			t.Fatal("ok = true, want false when disk pressure disabled")
		}
	})

	t.Run("enabled without source has nil sampler", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Volume.Path = "/data"
		cfg.Volume.DiskPressure.Enabled = true
		vol, ok := enabledVolume(cfg)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if vol.Path != "/data" {
			t.Errorf("Path = %q, want /data", vol.Path)
		}
		if vol.Sample != nil {
			t.Error("Sample != nil, want nil when source unset")
		}
	})

	t.Run("enabled with source wires an http sampler", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Volume.Path = "/data"
		cfg.Volume.Source = "http://fakedisk/usage"
		cfg.Volume.DiskPressure.Enabled = true
		vol, ok := enabledVolume(cfg)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if vol.Sample == nil {
			t.Error("Sample = nil, want a sampler when source is set")
		}
	})
}

func TestHTTPDiskSampler(t *testing.T) {
	t.Run("decodes fakedisk json", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"total_bytes":1000,"used_bytes":700,"free_bytes":300,"free_percent":30.0}`))
		}))
		defer srv.Close()

		got, err := httpDiskSampler(srv.URL)(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := triagearr.DiskUsage{TotalBytes: 1000, UsedBytes: 700, FreeBytes: 300, FreePercent: 30.0}
		if got != want {
			t.Errorf("DiskUsage = %+v, want %+v", got, want)
		}
	})

	t.Run("non-200 is an error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		if _, err := httpDiskSampler(srv.URL)(context.Background()); err == nil {
			t.Error("expected error on HTTP 500, got nil")
		}
	})

	t.Run("undecodable body is an error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("not json"))
		}))
		defer srv.Close()

		if _, err := httpDiskSampler(srv.URL)(context.Background()); err == nil {
			t.Error("expected decode error, got nil")
		}
	})
}
