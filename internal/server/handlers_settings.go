package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/store"
)

// settingsView is the GET /api/v1/settings response. Values mirrors the
// effective config (current YAML + overrides merged), narrowed to the
// editable sections. OverriddenKeys lets the UI badge fields that diverge
// from the YAML baseline. BaselineValues, when present, holds the same
// sections loaded without any overrides so the UI can show "what YAML says"
// on hover.
//
// These DTOs duplicate config field names rather than embedding config
// structs directly because:
//   - config.* structs only have `koanf:"..."` tags, so json.Marshal would
//     emit Go field names in CamelCase, which the UI doesn't expect.
//   - time.Duration marshals as int64 nanoseconds in stdlib JSON, which is
//     useless to the operator — we render durations as their humane string
//     form ("30m", "1h").
//
// Keeping the DTOs adjacent to the handler avoids forcing json tags onto
// every config struct (with the back-compat cost on other consumers).
type settingsView struct {
	Values         settingsValues  `json:"values"`
	OverriddenKeys []string        `json:"overridden_keys"`
	Editable       []string        `json:"editable_prefixes"`
	BaselineValues *settingsValues `json:"baseline_values,omitempty"`
}

type settingsValues struct {
	Scoring       scoringDTO        `json:"scoring"`
	Polling       pollingDTO        `json:"polling"`
	Volume        volumeSettingsDTO `json:"volume"`
	Notifications notificationsDTO  `json:"notifications"`
}

type notificationsDTO struct {
	Telegram telegramDTO `json:"telegram"`
}

// telegramDTO carries the Telegram provider settings. bot_token is sent
// verbatim (not redacted) because the operator opted into editing it from the
// dashboard — the field is rendered as a password input client-side.
type telegramDTO struct {
	Enabled  bool   `json:"enabled"`
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

type scoringDTO struct {
	HnRWindowDays int        `json:"hnr_window_days"`
	Weights       weightsDTO `json:"weights"`
}

type weightsDTO struct {
	RatioObligationMet float64 `json:"ratio_obligation_met"`
	UploadVelocityInv  float64 `json:"upload_velocity_inv"`
	AgeDays            float64 `json:"age_days"`
	SeedersLowGuard    float64 `json:"seeders_low_guard"`
	SwarmHealthBonus   float64 `json:"swarm_health_bonus"`
	TrackerDeadBonus   float64 `json:"tracker_dead_bonus"`
}

type pollingDTO struct {
	TorrentClientInterval string `json:"torrent_client_interval"`
	ArrInterval           string `json:"arr_interval"`
	ArrFileMinInterval    string `json:"arr_file_min_interval"`
	TrackerInterval       string `json:"tracker_interval"`
	DiskInterval          string `json:"disk_interval"`
	MaintainerrInterval   string `json:"maintainerr_interval"`
	DownsampleCron        string `json:"downsample_cron"`
}

type volumeSettingsDTO struct {
	Name         string          `json:"name"`
	DiskPressure diskPressureDTO `json:"disk_pressure"`
}

type diskPressureDTO struct {
	Enabled              bool    `json:"enabled"`
	ThresholdFreePercent float64 `json:"threshold_free_percent"`
	TargetFreePercent    float64 `json:"target_free_percent"`
	MaxRunSizeGB         int     `json:"max_run_size_gb"`
}

func scoringToDTO(s config.ScoringConfig) scoringDTO {
	return scoringDTO{
		HnRWindowDays: s.HnRWindowDays,
		Weights: weightsDTO{
			RatioObligationMet: s.Weights.RatioObligationMet,
			UploadVelocityInv:  s.Weights.UploadVelocityInv,
			AgeDays:            s.Weights.AgeDays,
			SeedersLowGuard:    s.Weights.SeedersLowGuard,
			SwarmHealthBonus:   s.Weights.SwarmHealthBonus,
			TrackerDeadBonus:   s.Weights.TrackerDeadBonus,
		},
	}
}

func pollingToDTO(p config.PollingConfig) pollingDTO {
	return pollingDTO{
		TorrentClientInterval: p.TorrentClientInterval.String(),
		ArrInterval:           p.ArrInterval.String(),
		ArrFileMinInterval:    p.ArrFileMinInterval.String(),
		TrackerInterval:       p.TrackerInterval.String(),
		DiskInterval:          p.DiskInterval.String(),
		MaintainerrInterval:   p.MaintainerrInterval.String(),
		DownsampleCron:        p.DownsampleCron,
	}
}

func notificationsToDTO(n config.NotificationsConfig) notificationsDTO {
	return notificationsDTO{
		Telegram: telegramDTO{
			Enabled:  n.Telegram.Enabled,
			BotToken: n.Telegram.BotToken,
			ChatID:   n.Telegram.ChatID,
		},
	}
}

func volumeToDTO(v config.VolumeConfig) volumeSettingsDTO {
	return volumeSettingsDTO{
		Name: v.Name,
		DiskPressure: diskPressureDTO{
			Enabled:              v.DiskPressure.Enabled,
			ThresholdFreePercent: v.DiskPressure.ThresholdFreePercent,
			TargetFreePercent:    v.DiskPressure.TargetFreePercent,
			MaxRunSizeGB:         v.DiskPressure.MaxRunSizeGB,
		},
	}
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	if s.opts.Config == nil {
		writeError(w, http.StatusServiceUnavailable, "config not wired into server")
		return
	}
	overrides, err := s.opts.Store.ListSettingsOverrides(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading overrides: "+err.Error())
		return
	}
	keys := make([]string, 0, len(overrides))
	for _, o := range overrides {
		keys = append(keys, o.Key)
	}

	view := settingsView{
		Values: settingsValues{
			Scoring:       scoringToDTO(s.opts.Config.Scoring),
			Polling:       pollingToDTO(s.opts.Config.Polling),
			Volume:        volumeToDTO(s.opts.Config.Volume),
			Notifications: notificationsToDTO(s.opts.Config.Notifications),
		},
		OverriddenKeys: keys,
		Editable:       config.EditableKeys(),
	}

	// When overrides are active and we know the config file path, load the
	// YAML baseline (no overrides) so the UI can show "what YAML says" on hover.
	if len(overrides) > 0 && s.opts.ConfigPath != "" {
		baseline, err := config.LoadWithOverrides(s.opts.ConfigPath, nil)
		if err != nil {
			// Non-fatal: the effective config is still returned. The UI falls
			// back to showing only the override badge without a hover value.
			slog.Warn("could not load baseline config for settings view", "err", err)
		} else {
			bv := settingsValues{
				Scoring:       scoringToDTO(baseline.Scoring),
				Polling:       pollingToDTO(baseline.Polling),
				Volume:        volumeToDTO(baseline.Volume),
				Notifications: notificationsToDTO(baseline.Notifications),
			}
			view.BaselineValues = &bv
		}
	}

	writeJSON(w, http.StatusOK, view)
}

// settingsPutRequest is the body shape for PUT /api/v1/settings. Each entry
// upserts one override; passing a null value (or omitting Value) deletes
// the override and reverts the key to the YAML default.
type settingsPutRequest struct {
	Overrides []settingsPutEntry `json:"overrides"`
}

type settingsPutEntry struct {
	Key   string           `json:"key"`
	Value *json.RawMessage `json:"value,omitempty"`
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	if s.opts.Config == nil {
		writeError(w, http.StatusServiceUnavailable, "config not wired into server")
		return
	}
	if s.opts.ReloadValidate == nil {
		writeError(w, http.StatusServiceUnavailable, "settings reload not wired into server")
		return
	}
	var body settingsPutRequest
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if len(body.Overrides) == 0 {
		writeError(w, http.StatusBadRequest, "no overrides supplied")
		return
	}

	// Enforce the editable whitelist first so we never write a forbidden key
	// to the store even if the merged config would later validate.
	for _, e := range body.Overrides {
		if e.Key == "" {
			writeError(w, http.StatusBadRequest, "empty key")
			return
		}
		if !config.IsEditableKey(e.Key) {
			writeError(w, http.StatusForbidden, fmt.Sprintf("key %q is not editable via the API", e.Key))
			return
		}
	}

	// Compute the prospective override set (existing rows + this request's
	// upserts/deletes) and try to load the config from it. This catches
	// schema-validation failures BEFORE we mutate the store.
	current, err := s.opts.Store.ListSettingsOverrides(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading overrides: "+err.Error())
		return
	}
	merged := mergeOverrides(current, body.Overrides)
	if err := s.opts.ReloadValidate(merged); err != nil {
		writeError(w, http.StatusBadRequest, "config validation failed: "+err.Error())
		return
	}

	// Validation passed — persist (upsert or delete each entry).
	for _, e := range body.Overrides {
		if e.Value == nil {
			if err := s.opts.Store.DeleteSettingsOverride(r.Context(), e.Key); err != nil {
				writeError(w, http.StatusInternalServerError, "deleting override: "+err.Error())
				return
			}
			continue
		}
		if err := s.opts.Store.UpsertSettingsOverride(r.Context(), e.Key, string(*e.Value)); err != nil {
			writeError(w, http.StatusInternalServerError, "persisting override: "+err.Error())
			return
		}
	}

	// Trigger the daemon-side reload so pollers and the HTTP server pick up
	// the new values. The Server itself is reconstructed by the SIGHUP loop —
	// this handler returns 202 immediately and the client should re-fetch
	// after a short delay.
	if s.opts.Reload != nil {
		s.opts.Reload()
	} else {
		slog.Warn("settings updated but no Reload hook is wired — daemon will not reload until next manual SIGHUP")
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleDeleteSetting(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "key required")
		return
	}
	if !config.IsEditableKey(key) {
		writeError(w, http.StatusForbidden, fmt.Sprintf("key %q is not editable", key))
		return
	}
	if err := s.opts.Store.DeleteSettingsOverride(r.Context(), key); err != nil {
		writeError(w, http.StatusInternalServerError, "deleting override: "+err.Error())
		return
	}
	if s.opts.Reload != nil {
		s.opts.Reload()
	}
	w.WriteHeader(http.StatusAccepted)
}

// handleTestNotification delivers a synthetic notification through every
// configured provider so the operator can verify credentials from the
// dashboard. Unlike the run-time dispatch this surfaces provider failures.
// It tests the currently-loaded config, so unsaved edits must be saved first.
func (s *Server) handleTestNotification(w http.ResponseWriter, r *http.Request) {
	if s.opts.Notifier == nil || s.opts.Notifier.Empty() {
		writeError(w, http.StatusBadRequest,
			"no notification provider is enabled — enable one and save before testing")
		return
	}
	if err := s.opts.Notifier.SendTest(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, "test notification failed: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// mergeOverrides folds a PUT request onto the existing rows: upserts replace
// matching keys, entries with nil Value remove them.
func mergeOverrides(existing []store.SettingsOverride, incoming []settingsPutEntry) []config.Override {
	index := make(map[string]string, len(existing))
	for _, e := range existing {
		index[e.Key] = e.ValueJSON
	}
	for _, e := range incoming {
		if e.Value == nil {
			delete(index, e.Key)
			continue
		}
		index[e.Key] = string(*e.Value)
	}
	out := make([]config.Override, 0, len(index))
	for k, v := range index {
		out = append(out, config.Override{Key: k, ValueJSON: v})
	}
	return out
}
