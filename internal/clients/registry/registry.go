// Package registry builds the live set of *arr clients from configuration
// and exposes filtered views for the pollers and (eventually) the actor.
package registry

import (
	"fmt"

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
