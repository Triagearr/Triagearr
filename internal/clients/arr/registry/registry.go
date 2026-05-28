// Package registry builds the live set of *arr clients from configuration
// and exposes filtered views for the pollers and (eventually) the actor.
package registry

import (
	"context"
	"fmt"
	"time"

	"github.com/Triagearr/Triagearr/internal/clients/arr/lidarr"
	"github.com/Triagearr/Triagearr/internal/clients/arr/radarr"
	"github.com/Triagearr/Triagearr/internal/clients/arr/readarr"
	"github.com/Triagearr/Triagearr/internal/clients/arr/sonarr"
	"github.com/Triagearr/Triagearr/internal/clients/arr/whisparr_v2"
	"github.com/Triagearr/Triagearr/internal/clients/arr/whisparr_v3"
	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// Registry owns the constructed ArrInstance clients.
type Registry struct {
	instances []triagearr.ArrInstance
}

// BuildFromConfig instantiates one client per enabled instance in cfg. Disabled
// instances are silently dropped. Instance construction errors fail-fast.
func BuildFromConfig(cfg *config.Config) (*Registry, error) {
	r := &Registry{}

	if inst := cfg.Arrs.Sonarr; inst.Enabled {
		c, err := sonarr.New(sonarr.Options{
			Name: string(triagearr.ArrTypeSonarr), BaseURL: inst.URL, APIKey: inst.APIKey,
			Poll: inst.Poll, Act: inst.Act, Timeout: inst.Timeout,
		})
		if err != nil {
			return nil, fmt.Errorf("registry: building sonarr: %w", err)
		}
		r.instances = append(r.instances, c)
	}
	if inst := cfg.Arrs.Radarr; inst.Enabled {
		c, err := radarr.New(radarr.Options{
			Name: string(triagearr.ArrTypeRadarr), BaseURL: inst.URL, APIKey: inst.APIKey,
			Poll: inst.Poll, Act: inst.Act, Timeout: inst.Timeout,
		})
		if err != nil {
			return nil, fmt.Errorf("registry: building radarr: %w", err)
		}
		r.instances = append(r.instances, c)
	}
	if inst := cfg.Arrs.Lidarr; inst.Enabled {
		c, err := lidarr.New(lidarr.Options{Name: string(triagearr.ArrTypeLidarr), BaseURL: inst.URL, Poll: inst.Poll, Act: inst.Act})
		if err != nil {
			return nil, fmt.Errorf("registry: building lidarr: %w", err)
		}
		r.instances = append(r.instances, c)
	}
	if inst := cfg.Arrs.Readarr; inst.Enabled {
		c, err := readarr.New(readarr.Options{Name: string(triagearr.ArrTypeReadarr), BaseURL: inst.URL, Poll: inst.Poll, Act: inst.Act})
		if err != nil {
			return nil, fmt.Errorf("registry: building readarr: %w", err)
		}
		r.instances = append(r.instances, c)
	}
	if inst := cfg.Arrs.WhisparrV2; inst.Enabled {
		c, err := whisparr_v2.New(whisparr_v2.Options{Name: string(triagearr.ArrTypeWhisparrV2), BaseURL: inst.URL, Poll: inst.Poll, Act: inst.Act})
		if err != nil {
			return nil, fmt.Errorf("registry: building whisparr_v2: %w", err)
		}
		r.instances = append(r.instances, c)
	}
	if inst := cfg.Arrs.WhisparrV3; inst.Enabled {
		c, err := whisparr_v3.New(whisparr_v3.Options{Name: string(triagearr.ArrTypeWhisparrV3), BaseURL: inst.URL, Poll: inst.Poll, Act: inst.Act})
		if err != nil {
			return nil, fmt.Errorf("registry: building whisparr_v3: %w", err)
		}
		r.instances = append(r.instances, c)
	}
	return r, nil
}

// KnownKinds is the set of *arr flavours the registry can construct. It
// doubles as the validation whitelist for the arr-connections HTTP layer.
var KnownKinds = []triagearr.ArrType{
	triagearr.ArrTypeSonarr,
	triagearr.ArrTypeRadarr,
	triagearr.ArrTypeLidarr,
	triagearr.ArrTypeReadarr,
	triagearr.ArrTypeWhisparrV2,
	triagearr.ArrTypeWhisparrV3,
}

// KnownKind reports whether kind names an *arr flavour the registry supports.
func KnownKind(kind string) bool {
	for _, k := range KnownKinds {
		if string(k) == kind {
			return true
		}
	}
	return false
}

// TestConnection builds an ephemeral client for kind and runs its HealthCheck,
// returning the underlying error so the dashboard can show the operator why a
// connection failed. It does not touch the live registry. Stub *arr kinds
// (lidarr/readarr/whisparr) return their "not implemented" health error.
func TestConnection(ctx context.Context, kind, baseURL, apiKey string, timeout time.Duration) error {
	var inst triagearr.ArrInstance
	var err error
	switch triagearr.ArrType(kind) {
	case triagearr.ArrTypeSonarr:
		inst, err = sonarr.New(sonarr.Options{Name: "test", BaseURL: baseURL, APIKey: apiKey, Timeout: timeout})
	case triagearr.ArrTypeRadarr:
		inst, err = radarr.New(radarr.Options{Name: "test", BaseURL: baseURL, APIKey: apiKey, Timeout: timeout})
	case triagearr.ArrTypeLidarr:
		inst, err = lidarr.New(lidarr.Options{Name: "test", BaseURL: baseURL})
	case triagearr.ArrTypeReadarr:
		inst, err = readarr.New(readarr.Options{Name: "test", BaseURL: baseURL})
	case triagearr.ArrTypeWhisparrV2:
		inst, err = whisparr_v2.New(whisparr_v2.Options{Name: "test", BaseURL: baseURL})
	case triagearr.ArrTypeWhisparrV3:
		inst, err = whisparr_v3.New(whisparr_v3.Options{Name: "test", BaseURL: baseURL})
	default:
		return fmt.Errorf("registry: unknown arr kind %q", kind)
	}
	if err != nil {
		return err
	}
	return inst.HealthCheck(ctx)
}

// All returns every registered instance, regardless of poll/act flags.
func (r *Registry) All() []triagearr.ArrInstance { return r.instances }

// AllPolling returns instances flagged for polling. Used by the arr poller.
func (r *Registry) AllPolling() []triagearr.ArrInstance {
	var out []triagearr.ArrInstance
	for _, inst := range r.instances {
		if inst.Poll() {
			out = append(out, inst)
		}
	}
	return out
}

// Deleter returns the FileDeleter for the given arr kind when (a) the
// instance exists, (b) it has `act: true`, and (c) the client implements
// FileDeleter. Stub *arr types (lidarr/readarr/whisparr) deliberately do
// not — they fail (c) and are rejected here.
func (r *Registry) Deleter(kind string) (triagearr.FileDeleter, bool) {
	for _, inst := range r.instances {
		if string(inst.Type()) != kind {
			continue
		}
		if !inst.Act() {
			return nil, false
		}
		d, ok := inst.(triagearr.FileDeleter)
		if !ok {
			return nil, false
		}
		return d, true
	}
	return nil, false
}
