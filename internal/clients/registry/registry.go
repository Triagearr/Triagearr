// Package registry builds the live set of *arr clients from configuration
// and exposes filtered views for the pollers and (eventually) the actor.
package registry

import (
	"context"
	"fmt"
	"time"

	"github.com/Triagearr/Triagearr/internal/clients/lidarr"
	"github.com/Triagearr/Triagearr/internal/clients/radarr"
	"github.com/Triagearr/Triagearr/internal/clients/readarr"
	"github.com/Triagearr/Triagearr/internal/clients/sonarr"
	"github.com/Triagearr/Triagearr/internal/clients/whisparr_v2"
	"github.com/Triagearr/Triagearr/internal/clients/whisparr_v3"
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

	for _, inst := range cfg.Arrs.Sonarr {
		if !inst.Enabled {
			continue
		}
		c, err := sonarr.New(sonarr.Options{
			Name: inst.Name, BaseURL: inst.URL, APIKey: inst.APIKey,
			Poll: inst.Poll, Act: inst.Act, Timeout: inst.Timeout,
		})
		if err != nil {
			return nil, fmt.Errorf("registry: building sonarr/%s: %w", inst.Name, err)
		}
		r.instances = append(r.instances, c)
	}
	for _, inst := range cfg.Arrs.Radarr {
		if !inst.Enabled {
			continue
		}
		c, err := radarr.New(radarr.Options{
			Name: inst.Name, BaseURL: inst.URL, APIKey: inst.APIKey,
			Poll: inst.Poll, Act: inst.Act, Timeout: inst.Timeout,
		})
		if err != nil {
			return nil, fmt.Errorf("registry: building radarr/%s: %w", inst.Name, err)
		}
		r.instances = append(r.instances, c)
	}
	for _, inst := range cfg.Arrs.Lidarr {
		if !inst.Enabled {
			continue
		}
		c, err := lidarr.New(lidarr.Options{Name: inst.Name, BaseURL: inst.URL, Poll: inst.Poll, Act: inst.Act})
		if err != nil {
			return nil, fmt.Errorf("registry: building lidarr/%s: %w", inst.Name, err)
		}
		r.instances = append(r.instances, c)
	}
	for _, inst := range cfg.Arrs.Readarr {
		if !inst.Enabled {
			continue
		}
		c, err := readarr.New(readarr.Options{Name: inst.Name, BaseURL: inst.URL, Poll: inst.Poll, Act: inst.Act})
		if err != nil {
			return nil, fmt.Errorf("registry: building readarr/%s: %w", inst.Name, err)
		}
		r.instances = append(r.instances, c)
	}
	for _, inst := range cfg.Arrs.WhisparrV2 {
		if !inst.Enabled {
			continue
		}
		c, err := whisparr_v2.New(whisparr_v2.Options{Name: inst.Name, BaseURL: inst.URL, Poll: inst.Poll, Act: inst.Act})
		if err != nil {
			return nil, fmt.Errorf("registry: building whisparr_v2/%s: %w", inst.Name, err)
		}
		r.instances = append(r.instances, c)
	}
	for _, inst := range cfg.Arrs.WhisparrV3 {
		if !inst.Enabled {
			continue
		}
		c, err := whisparr_v3.New(whisparr_v3.Options{Name: inst.Name, BaseURL: inst.URL, Poll: inst.Poll, Act: inst.Act})
		if err != nil {
			return nil, fmt.Errorf("registry: building whisparr_v3/%s: %w", inst.Name, err)
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

// Deleter returns the FileDeleter for the named instance when (a) the
// instance exists, (b) it has `act: true`, and (c) the client implements
// FileDeleter. Stub *arr types (lidarr/readarr/whisparr) deliberately do
// not — they fail (c) and are rejected here.
func (r *Registry) Deleter(name string) (triagearr.FileDeleter, bool) {
	for _, inst := range r.instances {
		if inst.Name() != name {
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
