import { z } from "zod";

export const SessionStatus = z.object({
  auth_enabled: z.boolean(),
  authenticated: z.boolean(),
  username: z.string().optional(),
});
export type SessionStatusT = z.infer<typeof SessionStatus>;

export const AuthEnableResponse = z.object({
  username: z.string(),
  password: z.string().optional(),
});
export type AuthEnableResponseT = z.infer<typeof AuthEnableResponse>;

export const AuthChangePasswordResponse = z.object({
  password: z.string().optional(),
});
export type AuthChangePasswordResponseT = z.infer<typeof AuthChangePasswordResponse>;

export const SimpleStatus = z.object({ status: z.string() });

export const Version = z.object({
  version: z.string(),
  commit: z.string(),
  date: z.string(),
});

const ts = z.string();

export const TorrentListItem = z.object({
  hash: z.string(),
  name: z.string(),
  category: z.string(),
  size: z.number(),
  added_on: ts,
  last_seen: ts,
  private: z.boolean(),
  ratio: z.number().nullable().optional(),
  seeders: z.number().nullable().optional(),
  leechers: z.number().nullable().optional(),
  state: z.string().nullable().optional(),
  snapshot_at: ts.nullable().optional(),
  score: z.number().nullable().optional(),
  excluded: z.boolean().nullable().optional(),
  any_tracker_alive: z.boolean().nullable().optional(),
});
export type TorrentListItemT = z.infer<typeof TorrentListItem>;

export const TorrentList = z.object({
  torrents: z.array(TorrentListItem),
  total: z.number(),
  limit: z.number(),
  offset: z.number(),
});

export const TorrentCategories = z.object({ categories: z.array(z.string()) });

export const TrackerStatus = z.enum([
  "working",
  "not_contacted",
  "updating",
  "not_working",
  "disabled",
  "unknown",
]);
export type TrackerStatusT = z.infer<typeof TrackerStatus>;

export const TrackerView = z.object({
  host: z.string(),
  url: z.string(),
  // Server may add new states; fall through to "unknown" rather than fail validation.
  status: TrackerStatus.catch("unknown"),
  message: z.string(),
  last_checked: ts,
});

export const LinkView = z.object({
  arr_type: z.string(),
  arr_url: z.string(),
  title_slug: z.string(),
  file_id: z.number(),
  size: z.number(),
  live_path: z.string(),
  dropped_path: z.string(),
  imported_path: z.string(),
});

export const ScoreView = z.object({
  score: z.number(),
  private: z.boolean(),
  any_tracker_alive: z.boolean(),
  excluded: z.boolean(),
  exclusion_reasons: z.string().optional(),
  factors: z.unknown().optional(),
  computed_at: ts,
});

export const TorrentDetail = z.object({
  hash: z.string(),
  name: z.string(),
  category: z.string(),
  save_path: z.string(),
  size: z.number(),
  added_on: ts,
  completion_on: ts.nullable().optional(),
  private: z.boolean(),
  tags: z.string(),
  last_seen: ts,
  protected: z.boolean(),
  protected_at: ts.nullable().optional(),
  latest: z
    .object({
      ratio: z.number().nullable().optional(),
      uploaded: z.number().nullable().optional(),
      seeders: z.number().nullable().optional(),
      leechers: z.number().nullable().optional(),
      state: z.string().nullable().optional(),
      snapshot_at: ts.nullable().optional(),
    })
    .optional(),
  trackers: z.array(TrackerView),
  links: z.array(LinkView),
  score: ScoreView.optional(),
});
export type TorrentDetailT = z.infer<typeof TorrentDetail>;

export const SnapshotPoint = z.object({
  ts: ts,
  ratio: z.number(),
  uploaded: z.number(),
  seeders: z.number(),
  leechers: z.number(),
  state: z.string(),
});
export const SnapshotList = z.object({ snapshots: z.array(SnapshotPoint) });

export const VolumeView = z.object({
  name: z.string(),
  path: z.string(),
  target_free_percent: z.number().optional(),
  threshold_free_percent: z.number().optional(),
  max_run_size_gb: z.number().optional(),
  total_bytes: z.number().optional(),
  used_bytes: z.number().optional(),
  free_bytes: z.number().optional(),
  free_percent: z.number().optional(),
  measured_at: ts.nullable().optional(),
});
export type VolumeViewT = z.infer<typeof VolumeView>;
export const VolumeResponse = z.object({ volume: VolumeView });

export const VolumeHistoryPoint = z.object({
  ts: ts,
  total_bytes: z.number(),
  used_bytes: z.number(),
  free_bytes: z.number(),
  free_percent: z.number(),
});
export const VolumeHistory = z.object({ history: z.array(VolumeHistoryPoint) });

export const ArrView = z.object({
  name: z.string(),
  type: z.string(),
  url: z.string(),
  healthy: z.boolean(),
  last_health_check: ts.nullable().optional(),
  last_error: z.string().optional(),
});
export type ArrViewT = z.infer<typeof ArrView>;
export const ArrList = z.object({ arrs: z.array(ArrView) });

export const RunMode = z.enum(["live", "dry-run"]).catch("dry-run");
export type RunModeT = z.infer<typeof RunMode>;

export const RunResponse = z.object({
  run_id: z.number(),
  triggered_by: z.string(),
  triggered_at: ts,
  mode: RunMode,
  free_pct_at_fire: z.number().optional(),
  target_free_pct: z.number().optional(),
  estimated_freed_bytes: z.number(),
  stop_reason: z.string(),
  status: z.string(),
  candidates: z
    .array(
      z.object({
        rank: z.number(),
        torrent_hash: z.string(),
        score: z.number(),
        size_bytes: z.number(),
        would_free_bytes: z.number(),
      }),
    )
    .optional(),
});
export type RunResponseT = z.infer<typeof RunResponse>;
export const RunList = z.object({ runs: z.array(RunResponse) });

// Keep aligned with internal/triagearr/types.go ActionStatus consts.
// .catch("pending") preserves forward compatibility — newly added Go variants
// degrade to a muted badge instead of breaking the page — but values listed
// here render with their dedicated tone in actions.tsx.
export const ActionStatus = z
  .enum([
    "succeeded",
    "pending",
    "running",
    "failed_qbit",
    "aborted_arr_fail",
    "aborted_nlink_check",
    "skipped_cross_seed",
  ])
  .catch("pending");
export type ActionStatusT = z.infer<typeof ActionStatus>;

export const ActionView = z.object({
  id: z.number(),
  run_id: z.number(),
  rank: z.number(),
  torrent_hash: z.string(),
  status: ActionStatus,
  started_at: ts,
  finished_at: ts.nullable().optional(),
  freed_bytes: z.number(),
});
export type ActionViewT = z.infer<typeof ActionView>;
export const ActionList = z.object({
  actions: z.array(ActionView),
  total: z.number(),
  limit: z.number(),
  offset: z.number(),
});

export const AuditOutcome = z
  .enum(["ok", "failed", "skipped", "not_attempted"])
  .catch("ok");
export type AuditOutcomeT = z.infer<typeof AuditOutcome>;

export const AuditView = z.object({
  id: z.number(),
  ts: ts,
  step: z.string(),
  outcome: AuditOutcome,
  arr_name: z.string().optional(),
  arr_file_id: z.number().optional(),
  detail: z.string().optional(),
});

export const ActionDetail = z.object({
  action: ActionView,
  audit: z.array(AuditView),
});

export const RunActionList = z.object({ actions: z.array(ActionView) });

export const ScoreListItem = ScoreView.extend({ hash: z.string(), name: z.string() });
export const ScoreList = z.object({ scores: z.array(ScoreListItem) });

export const Summary = z.object({
  volume: VolumeView,
  arrs: z.array(ArrView).nullable(),
  counts: z.object({
    torrents: z.number(),
    scored: z.number(),
    actions: z.number(),
  }),
  last_runs: z.array(RunResponse).nullable(),
  top_score: z.array(ScoreListItem).nullable(),
});
export type SummaryT = z.infer<typeof Summary>;

export const ConfigShape = z.unknown();

// Settings (editable overrides). The handler returns the effective values
// for the whitelisted sections, the list of currently overridden keys, and
// the editable-prefix whitelist (so the UI doesn't need to hardcode it).
export const ScoringWeights = z.object({
  ratio_obligation_met: z.number().optional(),
  upload_velocity_inv: z.number().optional(),
  age_days: z.number().optional(),
  seeders_low_guard: z.number().optional(),
  swarm_health_bonus: z.number().optional(),
});
export const ScoringSettings = z.object({
  weights: ScoringWeights.optional(),
  hnr_window_days: z.number().optional(),
});
export const PollingSettings = z.object({
  torrent_client_interval: z.string().optional(),
  arr_interval: z.string().optional(),
  arr_file_min_interval: z.string().optional(),
  tracker_interval: z.string().optional(),
  disk_interval: z.string().optional(),
  maintainerr_interval: z.string().optional(),
  downsample_cron: z.string().optional(),
});
export const VolumeDiskPressure = z.object({
  enabled: z.boolean().optional(),
  threshold_free_percent: z.number().optional(),
  target_free_percent: z.number().optional(),
  max_run_size_gb: z.number().optional(),
});
export const VolumeSettings = z.object({
  name: z.string(),
  disk_pressure: VolumeDiskPressure,
});
export const TelegramSettings = z.object({
  enabled: z.boolean().optional(),
  bot_token: z.string().optional(),
  chat_id: z.string().optional(),
});
export const NotificationSettings = z.object({
  telegram: TelegramSettings,
});
const SettingsValues = z.object({
  scoring: ScoringSettings,
  polling: PollingSettings,
  volume: VolumeSettings,
  notifications: NotificationSettings,
});

export const SettingsView = z.object({
  values: SettingsValues,
  overridden_keys: z.array(z.string()).nullable(),
  editable_prefixes: z.array(z.string()).nullable(),
  baseline_values: SettingsValues.optional(),
});
export type SettingsViewT = z.infer<typeof SettingsView>;

// ArrConnection mirrors the arr_connections table (ADR-0022). api_key is sent
// verbatim — the endpoint is behind auth and the UI renders it as a password
// field.
export const ArrConnection = z.object({
  id: z.number(),
  kind: z.string(),
  url: z.string(),
  api_key: z.string(),
  enabled: z.boolean(),
  poll: z.boolean(),
  act: z.boolean(),
  tags_exclude: z.array(z.string()).nullable(),
  categories_only: z.array(z.string()).nullable(),
  timeout_seconds: z.number(),
});
export const ArrConnectionList = z.object({
  connections: z.array(ArrConnection).nullable(),
});
export type ArrConnectionT = z.infer<typeof ArrConnection>;

// TorrentClientConnection mirrors the torrent_client_connections table
// (ADR-0025). password is sent verbatim — the endpoint is behind auth and
// the UI renders it as a password field. Only kind="qbittorrent" is
// implemented today; transmission/deluge/rtorrent are scaffolded tiles in
// the UI and rejected by the HTTP layer.
export const TorrentClientConnection = z.object({
  id: z.number(),
  kind: z.string(),
  url: z.string(),
  username: z.string(),
  password: z.string(),
  enabled: z.boolean(),
  category_exclude: z.array(z.string()).nullable(),
  tags_exclude: z.array(z.string()).nullable(),
  delete_with_files: z.boolean(),
  timeout_seconds: z.number(),
});
export const TorrentClientConnectionList = z.object({
  connections: z.array(TorrentClientConnection).nullable(),
});
export type TorrentClientConnectionT = z.infer<typeof TorrentClientConnection>;

// --- Scoring defaults + tracker policies (ADR-0026) ----------------------

export const ScoringDefaults = z.object({
  min_ratio: z.number(),
  min_seed_days: z.number(),
  rare_threshold: z.number(),
});
export type ScoringDefaultsT = z.infer<typeof ScoringDefaults>;

export const TrackerPolicy = z.object({
  tracker_host: z.string(),
  min_ratio: z.number(),
  min_seed_days: z.number(),
  rare_threshold: z.number().nullable(),
  enabled: z.boolean(),
});
export type TrackerPolicyT = z.infer<typeof TrackerPolicy>;

export const TrackerHostStat = z.object({
  tracker_host: z.string(),
  torrent_count: z.number(),
  any_alive: z.boolean(),
  all_dead: z.boolean(),
  policy: TrackerPolicy.nullable().optional(),
});
export type TrackerHostStatT = z.infer<typeof TrackerHostStat>;

export const TrackerHostStatList = z.array(TrackerHostStat);
