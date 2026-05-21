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
  ratio: z.number().nullable().optional(),
  seeders: z.number().nullable().optional(),
  leechers: z.number().nullable().optional(),
  state: z.string().nullable().optional(),
  snapshot_at: ts.nullable().optional(),
});
export type TorrentListItemT = z.infer<typeof TorrentListItem>;

export const TorrentList = z.object({
  torrents: z.array(TorrentListItem),
  total: z.number(),
  limit: z.number(),
  offset: z.number(),
});

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
  arr_name: z.string(),
  arr_type: z.string(),
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
export const VolumeList = z.object({ volumes: z.array(VolumeView) });

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
  volume: z.string().optional(),
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

export const ActionStatus = z
  .enum(["succeeded", "pending", "running", "failed_qbit", "aborted_arr_fail"])
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

export const ScoreListItem = ScoreView.extend({ hash: z.string() });
export const ScoreList = z.object({ scores: z.array(ScoreListItem) });

export const Summary = z.object({
  volumes: z.array(VolumeView).nullable(),
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
