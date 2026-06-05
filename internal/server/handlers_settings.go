package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/notify"
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
	Mode          string            `json:"mode"`
	Scoring       scoringDTO        `json:"scoring"`
	Polling       pollingDTO        `json:"polling"`
	Volume        volumeSettingsDTO `json:"volume"`
	Notifications notificationsDTO  `json:"notifications"`
}

type notificationsDTO struct {
	Telegram          telegramDTO          `json:"telegram"`
	Discord           discordDTO           `json:"discord"`
	Ntfy              ntfyDTO              `json:"ntfy"`
	Email             emailDTO             `json:"email"`
	Slack             slackDTO             `json:"slack"`
	Webhook           webhookDTO           `json:"webhook"`
	TargetUnreachable targetUnreachableDTO `json:"target_unreachable"`
}

// targetUnreachableDTO carries the recurring target-unreachable alert cadence
// (ADR-0032). reminder_interval is a duration string (e.g. "24h0m0s"), matching
// how polling intervals are exposed.
type targetUnreachableDTO struct {
	ReminderInterval string `json:"reminder_interval"`
}

// routingDTO is the per-provider severity-threshold routing (ADR-0033), shared
// across every provider DTO.
type routingDTO struct {
	MinSeverity string   `json:"min_severity"`
	Mute        []string `json:"mute"`
}

// Provider secrets (bot_token, password, webhook URLs, …) are sent verbatim
// rather than redacted: the operator opted into editing them from the dashboard
// and they are rendered as password inputs client-side.

type telegramDTO struct {
	Enabled  bool   `json:"enabled"`
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
	routingDTO
}

type discordDTO struct {
	Enabled    bool   `json:"enabled"`
	WebhookURL string `json:"webhook_url"`
	routingDTO
}

type ntfyDTO struct {
	Enabled  bool   `json:"enabled"`
	Server   string `json:"server"`
	Topic    string `json:"topic"`
	Username string `json:"username"`
	Password string `json:"password"`
	routingDTO
}

type emailDTO struct {
	Enabled     bool     `json:"enabled"`
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	From        string   `json:"from"`
	To          []string `json:"to"`
	UseStartTLS bool     `json:"use_starttls"`
	routingDTO
}

type slackDTO struct {
	Enabled    bool   `json:"enabled"`
	WebhookURL string `json:"webhook_url"`
	routingDTO
}

type webhookDTO struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	Secret  string `json:"secret"`
	routingDTO
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
	HealthInterval        string `json:"health_interval"`
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
		HealthInterval:        p.HealthInterval.String(),
		MaintainerrInterval:   p.MaintainerrInterval.String(),
		DownsampleCron:        p.DownsampleCron,
	}
}

func routingToDTO(r config.ProviderRouting) routingDTO {
	return routingDTO{MinSeverity: r.MinSeverity, Mute: r.Mute}
}

func notificationsToDTO(n config.NotificationsConfig) notificationsDTO {
	return notificationsDTO{
		Telegram: telegramDTO{
			Enabled:    n.Telegram.Enabled,
			BotToken:   n.Telegram.BotToken,
			ChatID:     n.Telegram.ChatID,
			routingDTO: routingToDTO(n.Telegram.ProviderRouting),
		},
		Discord: discordDTO{
			Enabled:    n.Discord.Enabled,
			WebhookURL: n.Discord.WebhookURL,
			routingDTO: routingToDTO(n.Discord.ProviderRouting),
		},
		Ntfy: ntfyDTO{
			Enabled:    n.Ntfy.Enabled,
			Server:     n.Ntfy.Server,
			Topic:      n.Ntfy.Topic,
			Username:   n.Ntfy.Username,
			Password:   n.Ntfy.Password,
			routingDTO: routingToDTO(n.Ntfy.ProviderRouting),
		},
		Email: emailDTO{
			Enabled:     n.Email.Enabled,
			Host:        n.Email.Host,
			Port:        n.Email.Port,
			Username:    n.Email.Username,
			Password:    n.Email.Password,
			From:        n.Email.From,
			To:          n.Email.To,
			UseStartTLS: n.Email.UseStartTLS,
			routingDTO:  routingToDTO(n.Email.ProviderRouting),
		},
		Slack: slackDTO{
			Enabled:    n.Slack.Enabled,
			WebhookURL: n.Slack.WebhookURL,
			routingDTO: routingToDTO(n.Slack.ProviderRouting),
		},
		Webhook: webhookDTO{
			Enabled:    n.Webhook.Enabled,
			URL:        n.Webhook.URL,
			Secret:     n.Webhook.Secret,
			routingDTO: routingToDTO(n.Webhook.ProviderRouting),
		},
		TargetUnreachable: targetUnreachableDTO{
			ReminderInterval: n.TargetUnreachable.ReminderInterval.String(),
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
		},
	}
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	cfg := s.engine().Config
	if cfg == nil {
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
			Mode:          string(cfg.Mode),
			Scoring:       scoringToDTO(cfg.Scoring),
			Polling:       pollingToDTO(cfg.Polling),
			Volume:        volumeToDTO(cfg.Volume),
			Notifications: notificationsToDTO(cfg.Notifications),
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
				Mode:          string(baseline.Mode),
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
	if s.engine().Config == nil {
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

	// Rebuild the Engine + pollers from the freshly persisted overrides and
	// swap them in. This blocks until the new config is live, so a 200 means
	// the client can re-fetch immediately — no arbitrary settle delay. The
	// listener survives (Option B); only config-derived subsystems are rebuilt.
	if err := s.reload(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "reloading config: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
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
	if err := s.reload(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "reloading config: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

// testNotificationRequest narrows a test send (ADR-0033). Both fields are
// optional: an empty body tests every provider with the generic event; provider
// targets one channel; kind sends a representative sample of that event type.
type testNotificationRequest struct {
	Provider string `json:"provider"`
	Kind     string `json:"kind"`
}

// handleTestNotification delivers a synthetic notification through the targeted
// provider(s) so the operator can verify credentials from the dashboard. Unlike
// the run-time dispatch this surfaces provider failures. It tests the
// currently-loaded config, so unsaved edits must be saved first.
func (s *Server) handleTestNotification(w http.ResponseWriter, r *http.Request) {
	notifier := s.engine().Notifier
	if notifier == nil || notifier.Empty() {
		writeError(w, http.StatusBadRequest,
			"no notification provider is enabled — enable one and save before testing")
		return
	}
	// An empty body is valid (test everything, generic event).
	var req testNotificationRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			writeError(w, http.StatusBadRequest, "decoding request: "+err.Error())
			return
		}
	}
	if err := notifier.SendTestEvent(r.Context(), notify.TestOptions{
		Provider: req.Provider,
		Kind:     notify.EventKind(req.Kind),
	}); err != nil {
		writeError(w, http.StatusBadGateway, "test notification failed: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// catalogueEntryDTO is one row of the event catalogue: a stable event kind and
// its fixed severity (ADR-0033). Drives the UI catalogue and mute pickers.
type catalogueEntryDTO struct {
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
}

// handleNotificationCatalogue returns the static event taxonomy.
func (s *Server) handleNotificationCatalogue(w http.ResponseWriter, _ *http.Request) {
	cat := notify.Catalogue()
	out := make([]catalogueEntryDTO, 0, len(cat))
	for _, e := range cat {
		out = append(out, catalogueEntryDTO{Kind: string(e.Kind), Severity: e.Severity.String()})
	}
	writeJSON(w, http.StatusOK, out)
}

// deliveryDTO is one recent fan-out attempt for the dashboard deliveries panel.
type deliveryDTO struct {
	Provider string `json:"provider"`
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	At       string `json:"at"`
}

// handleNotificationDeliveries returns the in-memory recent-deliveries ring,
// newest first. The ring does not survive a restart (advisory data, ADR-0033).
func (s *Server) handleNotificationDeliveries(w http.ResponseWriter, _ *http.Request) {
	notifier := s.engine().Notifier
	dels := notifier.Deliveries()
	out := make([]deliveryDTO, 0, len(dels))
	for _, d := range dels {
		out = append(out, deliveryDTO{
			Provider: d.Provider,
			Kind:     string(d.Kind),
			Severity: d.Severity.String(),
			OK:       d.OK,
			Error:    d.Err,
			At:       d.At.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, out)
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
