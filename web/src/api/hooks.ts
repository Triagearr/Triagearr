import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { z } from "zod";
import { apiFetch, apiFetchVoid } from "./client";
import {
  ActionDetail,
  ActionList,
  ArrConnection,
  ArrConnectionList,
  ArrList,
  TorrentClientConnection,
  TorrentClientConnectionList,
  AuthChangePasswordResponse,
  AuthEnableResponse,
  ConfigShape,
  SettingsView,
  NotificationCatalogue,
  NotificationDeliveries,
  RunActionList,
  RunList,
  RunPreview,
  RunResponse,
  ScoreList,
  ScoringDefaults,
  ScoringSimResultList,
  TrackerPolicy,
  TrackerHostStatList,
  SessionStatus,
  SimpleStatus,
  Summary,
  SnapshotList,
  TorrentCategories,
  TorrentDetail,
  TorrentList,
  Version,
  VolumeHistory,
  VolumeResponse,
} from "./schemas";

export const queryKeys = {
  session: ["session"] as const,
  version: ["version"] as const,
  summary: ["summary"] as const,
  volume: ["volume"] as const,
  volumeHistory: (since: string) => ["volume", "history", since] as const,
  torrents: (params: Record<string, string | number | boolean>) => ["torrents", params] as const,
  torrent: (hash: string) => ["torrent", hash] as const,
  snapshots: (hash: string, since: string) => ["torrent", hash, "snapshots", since] as const,
  scores: ["scores"] as const,
  runs: ["runs"] as const,
  runPreview: ["runs", "preview"] as const,
  run: (id: number) => ["run", id] as const,
  runActions: (id: number) => ["run", id, "actions"] as const,
  actions: (limit: number, offset: number) => ["actions", limit, offset] as const,
  action: (id: number) => ["action", id] as const,
  arrs: ["arrs"] as const,
  arrConnections: ["arr-connections"] as const,
  torrentClientConnections: ["torrent-client-connections"] as const,
  scoringDefaults: ["scoring", "defaults"] as const,
  trackerPolicies: ["scoring", "tracker-policies"] as const,
  config: ["config"] as const,
  settings: ["settings"] as const,
  notificationCatalogue: ["notifications", "catalogue"] as const,
  notificationDeliveries: ["notifications", "deliveries"] as const,
};

export function useSession() {
  return useQuery({
    queryKey: queryKeys.session,
    queryFn: () => apiFetch("/api/v1/session", SessionStatus),
    staleTime: 30_000,
  });
}

export function useLogin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ username, password }: { username: string; password: string }) =>
      apiFetch("/api/v1/session", SessionStatus, {
        method: "POST",
        body: JSON.stringify({ username, password }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.session });
    },
  });
}

export function useLogout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () =>
      apiFetch("/api/v1/session", SimpleStatus, {
        method: "DELETE",
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.session });
    },
  });
}

export function useEnableAuth() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ username, password }: { username: string; password?: string }) =>
      apiFetch("/api/v1/auth/enable", AuthEnableResponse, {
        method: "POST",
        body: JSON.stringify(password ? { username, password } : { username }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.session });
    },
  });
}

export function useDisableAuth() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ password }: { password: string }) =>
      apiFetch("/api/v1/auth/disable", SimpleStatus, {
        method: "POST",
        body: JSON.stringify({ password }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.session });
    },
  });
}

export function useChangePassword() {
  return useMutation({
    mutationFn: ({ current, newPassword }: { current: string; newPassword?: string }) =>
      apiFetch("/api/v1/auth/password", AuthChangePasswordResponse, {
        method: "POST",
        body: JSON.stringify(newPassword ? { current, new: newPassword } : { current }),
      }),
  });
}

export function useVersion() {
  return useQuery({
    queryKey: queryKeys.version,
    queryFn: () => apiFetch("/api/v1/version", Version),
    staleTime: 5 * 60 * 1000,
  });
}

export function useSummary() {
  return useQuery({
    queryKey: queryKeys.summary,
    queryFn: () => apiFetch("/api/v1/summary", Summary),
    refetchInterval: 15_000,
  });
}

export function useVolume() {
  return useQuery({
    queryKey: queryKeys.volume,
    queryFn: () => apiFetch("/api/v1/volume", VolumeResponse),
    refetchInterval: 15_000,
  });
}

export function useVolumeHistory(since = "24h") {
  return useQuery({
    queryKey: queryKeys.volumeHistory(since),
    queryFn: () => apiFetch(`/api/v1/volume/history?since=${since}`, VolumeHistory),
  });
}

export type TorrentsQuery = {
  q?: string;
  category?: string;
  privateOnly?: boolean;
  excludedOnly?: boolean;
  sort?: string;
  order?: "asc" | "desc";
  limit?: number;
  offset?: number;
};

export function useTorrents(params: TorrentsQuery) {
  const search = new URLSearchParams();
  if (params.q) search.set("q", params.q);
  if (params.category) search.set("category", params.category);
  if (params.privateOnly) search.set("private", "1");
  if (params.excludedOnly) search.set("excluded", "1");
  if (params.sort) search.set("sort", params.sort);
  if (params.order) search.set("order", params.order);
  if (params.limit != null) search.set("limit", String(params.limit));
  if (params.offset != null) search.set("offset", String(params.offset));
  return useQuery({
    queryKey: queryKeys.torrents({ ...params }),
    queryFn: () => apiFetch(`/api/v1/torrents?${search.toString()}`, TorrentList),
    refetchInterval: 30_000,
    refetchIntervalInBackground: false,
  });
}

export function useTorrentCategories() {
  return useQuery({
    queryKey: ["torrent-categories"],
    queryFn: () => apiFetch("/api/v1/torrents/categories", TorrentCategories),
    staleTime: 5 * 60 * 1000,
  });
}

export function useTorrent(hash: string) {
  return useQuery({
    queryKey: queryKeys.torrent(hash),
    queryFn: () => apiFetch(`/api/v1/torrents/${hash}`, TorrentDetail),
    enabled: Boolean(hash),
  });
}

// PUT /api/v1/torrents/{hash}/protected — toggles the user-driven protection
// flag. Server triggers a single-hash rescore so the excluded badge updates
// immediately. We patch the cached list rows optimistically (flip `excluded`
// to match `protected`) instead of invalidating ["torrents"], which would
// refetch every cached pagination. The 30s polling reconciles any drift.
export function useSetTorrentProtected() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ hash, protected: prot }: { hash: string; protected: boolean }) =>
      apiFetchVoid(`/api/v1/torrents/${hash}/protected`, {
        method: "PUT",
        body: JSON.stringify({ protected: prot }),
      }),
    onSuccess: (_data, { hash, protected: prot }) => {
      qc.setQueriesData<z.infer<typeof TorrentList>>(
        { queryKey: ["torrents"] },
        (old) => {
          if (!old) return old;
          let touched = false;
          const torrents = old.torrents.map((t) => {
            if (t.hash !== hash || t.excluded === prot) return t;
            touched = true;
            return { ...t, excluded: prot };
          });
          return touched ? { ...old, torrents } : old;
        },
      );
      qc.invalidateQueries({ queryKey: queryKeys.torrent(hash) });
      qc.invalidateQueries({ queryKey: queryKeys.scores });
    },
  });
}

// PUT /api/v1/torrents/{hash}/candidate_boost — the inverse of protect: a
// user-driven "prioritize deletion" flag (ADR-0030). We optimistically patch the
// cached list row's `candidate_boost` (so the badge flips immediately) and, when
// boosting, its `excluded` to false (boost clears protect server-side). The big
// score change is reflected by invalidating the detail + scores queries; the 30s
// poll reconciles the list's `score`.
export function useSetTorrentCandidateBoost() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ hash, boost }: { hash: string; boost: boolean }) =>
      apiFetchVoid(`/api/v1/torrents/${hash}/candidate_boost`, {
        method: "PUT",
        body: JSON.stringify({ candidate_boost: boost }),
      }),
    onSuccess: (_data, { hash, boost }) => {
      qc.setQueriesData<z.infer<typeof TorrentList>>(
        { queryKey: ["torrents"] },
        (old) => {
          if (!old) return old;
          let touched = false;
          const torrents = old.torrents.map((t) => {
            if (t.hash !== hash) return t;
            const next = { ...t, candidate_boost: boost };
            // Boosting clears protect, so the row is no longer excluded.
            if (boost && t.excluded) next.excluded = false;
            touched = true;
            return next;
          });
          return touched ? { ...old, torrents } : old;
        },
      );
      qc.invalidateQueries({ queryKey: queryKeys.torrent(hash) });
      qc.invalidateQueries({ queryKey: queryKeys.scores });
      // The dashboard's "Top reap candidates" card reads top_score from /summary.
      qc.invalidateQueries({ queryKey: queryKeys.summary });
    },
  });
}

export function useSnapshots(hash: string, since = "720h") {
  return useQuery({
    queryKey: queryKeys.snapshots(hash, since),
    queryFn: () => apiFetch(`/api/v1/torrents/${hash}/snapshots?since=${since}`, SnapshotList),
    enabled: Boolean(hash),
  });
}

export function useScores() {
  return useQuery({
    queryKey: queryKeys.scores,
    queryFn: () => apiFetch("/api/v1/scores?limit=50", ScoreList),
  });
}

export function useRuns() {
  return useQuery({
    queryKey: queryKeys.runs,
    queryFn: () => apiFetch("/api/v1/runs?limit=50", RunList),
    refetchInterval: 5_000,
    refetchIntervalInBackground: false,
  });
}

// usePreviewRun fetches the plan a live run would execute right now, without
// persisting it. Enabled only while the confirm modal is open so we don't poll
// the Decider in the background. staleTime is short: the plan moves with disk
// state, and the modal should reflect a recent view.
export function usePreviewRun(enabled: boolean, refetchInterval?: number) {
  return useQuery({
    queryKey: queryKeys.runPreview,
    queryFn: () => apiFetch("/api/v1/runs/preview", RunPreview),
    enabled,
    staleTime: 5_000,
    refetchInterval,
  });
}

// useRun fetches a single run with its full candidate items. The /runs list
// strips candidates (handlers_runs.go), so the detail panel must call this
// when the user selects a run — relying on useRuns().find() leaves
// run.candidates undefined and the dry-run plan invisible.
export function useRun(id: number | undefined, refetchInterval?: number) {
  return useQuery({
    queryKey: id != null ? queryKeys.run(id) : ["run", "noop"],
    queryFn: () => apiFetch(`/api/v1/runs/${id}`, RunResponse),
    enabled: Boolean(id),
    refetchInterval,
  });
}

export function useRunActions(id: number | undefined, refetchInterval?: number) {
  return useQuery({
    queryKey: id != null ? queryKeys.runActions(id) : ["run", "noop"],
    queryFn: () => apiFetch(`/api/v1/runs/${id}/actions`, RunActionList),
    enabled: Boolean(id),
    refetchInterval,
  });
}

export function useActions(limit = 50, offset = 0) {
  return useQuery({
    queryKey: queryKeys.actions(limit, offset),
    queryFn: () => apiFetch(`/api/v1/actions?limit=${limit}&offset=${offset}`, ActionList),
    refetchInterval: 30_000,
    refetchIntervalInBackground: false,
  });
}

export function useAction(id: number | undefined) {
  return useQuery({
    queryKey: id != null ? queryKeys.action(id) : ["action", "noop"],
    queryFn: () => apiFetch(`/api/v1/actions/${id}`, ActionDetail),
    enabled: Boolean(id),
  });
}

export function useArrs() {
  return useQuery({
    queryKey: queryKeys.arrs,
    queryFn: () => apiFetch("/api/v1/arrs", ArrList),
    refetchInterval: 60_000,
  });
}

export function useConfig() {
  return useQuery({
    queryKey: queryKeys.config,
    queryFn: () => apiFetch("/api/v1/config", ConfigShape),
  });
}

export function useSettings() {
  return useQuery({
    queryKey: queryKeys.settings,
    queryFn: () => apiFetch("/api/v1/settings", SettingsView),
  });
}

export type SettingsOverrideInput = { key: string; value: unknown | null };

// PUT /api/v1/settings — sends one or more override changes. Passing
// value:null deletes the key (reverts to YAML default). The server rebuilds
// the daemon's config-derived engine in place and only returns 200 once the
// swap is live, so we can invalidate immediately — no settle delay.
export function useUpdateSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (overrides: SettingsOverrideInput[]) =>
      apiFetchVoid("/api/v1/settings", {
        method: "PUT",
        body: JSON.stringify({ overrides }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.settings });
      qc.invalidateQueries({ queryKey: queryKeys.config });
      qc.invalidateQueries({ queryKey: queryKeys.summary });
    },
  });
}

// POST /api/v1/notifications/test — delivers a synthetic notification through
// the saved provider config so the operator can verify credentials. Tests the
// currently-loaded config, so unsaved edits must be saved first. An empty opts
// tests every provider with the generic event; provider/kind narrow the test.
export type TestNotificationOpts = { provider?: string; kind?: string };
export function useTestNotification() {
  return useMutation({
    mutationFn: (opts: TestNotificationOpts = {}) =>
      apiFetchVoid("/api/v1/notifications/test", {
        method: "POST",
        body: JSON.stringify(opts),
      }),
  });
}

// GET /api/v1/notifications/catalogue — the static event taxonomy + severities.
export function useNotificationCatalogue() {
  return useQuery({
    queryKey: queryKeys.notificationCatalogue,
    queryFn: () => apiFetch("/api/v1/notifications/catalogue", NotificationCatalogue),
    staleTime: Infinity, // the catalogue is build-static
  });
}

// GET /api/v1/notifications/deliveries — recent fan-out attempts (in-memory).
export function useNotificationDeliveries() {
  return useQuery({
    queryKey: queryKeys.notificationDeliveries,
    queryFn: () => apiFetch("/api/v1/notifications/deliveries", NotificationDeliveries),
  });
}

// --- *arr connections (ADR-0022) ----------------------------------------

export type ArrConnectionInput = {
  kind: string;
  url: string;
  public_url: string;
  api_key: string;
  enabled: boolean;
  poll: boolean;
  act: boolean;
  tags_exclude: string[];
  categories_only: string[];
  timeout_seconds: number;
};

// createConnectionHooks builds the {list, create, update, delete, test} hook
// set for a /api/v1/<basePath>-connections resource. *arr and torrent-client
// connections share the same CRUD shape and only differ in their item schema,
// query key, and which downstream views to re-invalidate on change.
function createConnectionHooks<TList, TItem, TInput extends { kind: string }, TTest>(opts: {
  basePath: string;
  listSchema: z.ZodType<TList>;
  itemSchema: z.ZodType<TItem>;
  queryKey: readonly unknown[];
  // extraInvalidateKeys are invalidated alongside the resource's own key after
  // a mutation. The server rebuilds the registry in place before responding, so
  // the re-read reflects the new connections without any delay.
  extraInvalidateKeys?: readonly (readonly unknown[])[];
}) {
  const { basePath, listSchema, itemSchema, queryKey, extraInvalidateKeys = [] } = opts;

  const invalidate = (qc: ReturnType<typeof useQueryClient>) => {
    qc.invalidateQueries({ queryKey });
    for (const k of extraInvalidateKeys) qc.invalidateQueries({ queryKey: k });
  };

  const upsert = (kind: string, body: Omit<TInput, "kind">) =>
    apiFetch(`${basePath}/${kind}`, itemSchema, {
      method: "PUT",
      body: JSON.stringify(body),
    });

  const useList = () =>
    useQuery({ queryKey, queryFn: () => apiFetch(basePath, listSchema) });

  const useCreate = () => {
    const qc = useQueryClient();
    return useMutation({
      mutationFn: ({ kind, ...body }: TInput) => upsert(kind, body as Omit<TInput, "kind">),
      onSuccess: () => invalidate(qc),
    });
  };

  const useUpdate = () => {
    const qc = useQueryClient();
    return useMutation({
      mutationFn: ({ kind, input }: { kind: string; input: TInput }) => {
        // eslint-disable-next-line @typescript-eslint/no-unused-vars
        const { kind: _k, ...body } = input;
        return upsert(kind, body as Omit<TInput, "kind">);
      },
      onSuccess: () => invalidate(qc),
    });
  };

  const useDelete = () => {
    const qc = useQueryClient();
    return useMutation({
      mutationFn: (kind: string) =>
        apiFetchVoid(`${basePath}/${kind}`, { method: "DELETE" }),
      onSuccess: () => invalidate(qc),
    });
  };

  const useTest = () =>
    useMutation({
      mutationFn: (input: TTest) =>
        apiFetchVoid(`${basePath}/test`, {
          method: "POST",
          body: JSON.stringify(input),
        }),
    });

  return { useList, useCreate, useUpdate, useDelete, useTest };
}

const arrConnHooks = createConnectionHooks<
  z.infer<typeof ArrConnectionList>,
  z.infer<typeof ArrConnection>,
  ArrConnectionInput,
  { kind: string; url: string; api_key: string; timeout_seconds: number }
>({
  basePath: "/api/v1/arr-connections",
  listSchema: ArrConnectionList,
  itemSchema: ArrConnection,
  queryKey: queryKeys.arrConnections,
  extraInvalidateKeys: [queryKeys.arrs, queryKeys.summary],
});

export const useArrConnections = arrConnHooks.useList;
export const useCreateArrConnection = arrConnHooks.useCreate;
export const useUpdateArrConnection = arrConnHooks.useUpdate;
export const useDeleteArrConnection = arrConnHooks.useDelete;
export const useTestArrConnection = arrConnHooks.useTest;

// --- Torrent client connections (ADR-0025) -------------------------------

export type TorrentClientConnectionInput = {
  kind: string;
  url: string;
  public_url: string;
  username: string;
  password: string;
  enabled: boolean;
  category_exclude: string[];
  tags_exclude: string[];
  delete_with_files: boolean;
  timeout_seconds: number;
};

const torrentConnHooks = createConnectionHooks<
  z.infer<typeof TorrentClientConnectionList>,
  z.infer<typeof TorrentClientConnection>,
  TorrentClientConnectionInput,
  { kind: string; url: string; username: string; password: string; timeout_seconds: number }
>({
  basePath: "/api/v1/torrent-client-connections",
  listSchema: TorrentClientConnectionList,
  itemSchema: TorrentClientConnection,
  queryKey: queryKeys.torrentClientConnections,
  extraInvalidateKeys: [queryKeys.summary],
});

export const useTorrentClientConnections = torrentConnHooks.useList;
export const useCreateTorrentClientConnection = torrentConnHooks.useCreate;
export const useUpdateTorrentClientConnection = torrentConnHooks.useUpdate;
export const useDeleteTorrentClientConnection = torrentConnHooks.useDelete;
export const useTestTorrentClientConnection = torrentConnHooks.useTest;

// --- Scoring defaults + tracker policies (ADR-0026) ----------------------

export function useScoringDefaults() {
  return useQuery({
    queryKey: queryKeys.scoringDefaults,
    queryFn: () => apiFetch("/api/v1/scoring/defaults", ScoringDefaults),
  });
}

export function useUpdateScoringDefaults() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: z.infer<typeof ScoringDefaults>) =>
      apiFetchVoid("/api/v1/scoring/defaults", {
        method: "PUT",
        body: JSON.stringify(body),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.scoringDefaults });
      qc.invalidateQueries({ queryKey: queryKeys.trackerPolicies });
      qc.invalidateQueries({ queryKey: queryKeys.scores });
    },
  });
}

export function useTrackerPolicies() {
  return useQuery({
    queryKey: queryKeys.trackerPolicies,
    queryFn: () => apiFetch("/api/v1/scoring/tracker-policies", TrackerHostStatList),
  });
}

// --- Scoring simulator (config preview) ----------------------------------

export type ScoringSimInput = {
  weights: {
    ratio_obligation_met: number;
    upload_velocity_inv: number;
    age_days: number;
    seeders_low_guard: number;
    swarm_health_bonus: number;
    tracker_dead_bonus: number;
  };
  hnr_window_days: number;
  defaults: { min_ratio: number; min_seed_days: number; rare_threshold: number };
};

// useScoringSimulation scores the built-in archetypes against the supplied
// (proposed) config. Read-only: it never persists. keepPreviousData keeps the
// last result on screen while a fresh request is in flight so the live preview
// does not flash empty as the operator drags a value. Debounce the `input`
// upstream to avoid a request per keystroke.
export function useScoringSimulation(input: ScoringSimInput) {
  return useQuery({
    queryKey: ["scoring", "simulate", input] as const,
    queryFn: () =>
      apiFetch("/api/v1/scoring/simulate", ScoringSimResultList, {
        method: "POST",
        body: JSON.stringify(input),
      }),
    placeholderData: keepPreviousData,
  });
}

export type TrackerPolicyInput = {
  min_ratio: number;
  min_seed_days: number;
  rare_threshold: number | null;
  enabled: boolean;
};

export function useUpsertTrackerPolicy() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ host, input }: { host: string; input: TrackerPolicyInput }) =>
      apiFetch(`/api/v1/scoring/tracker-policies/${encodeURIComponent(host)}`, TrackerPolicy, {
        method: "PUT",
        body: JSON.stringify({ tracker_host: host, ...input }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.trackerPolicies });
      qc.invalidateQueries({ queryKey: queryKeys.scores });
    },
  });
}

export function useDeleteTrackerPolicy() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (host: string) =>
      apiFetchVoid(`/api/v1/scoring/tracker-policies/${encodeURIComponent(host)}`, {
        method: "DELETE",
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.trackerPolicies });
      qc.invalidateQueries({ queryKey: queryKeys.scores });
    },
  });
}

export function useTriggerRun() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ mode }: { mode?: "live" | "dry-run" }) =>
      apiFetch("/api/v1/runs", RunResponse, {
        method: "POST",
        body: JSON.stringify({ mode }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.runs });
      qc.invalidateQueries({ queryKey: queryKeys.summary });
      qc.invalidateQueries({ queryKey: ["actions"] });
    },
  });
}

// useStopRun requests a clean stop of an in-flight live run. The stop is
// cooperative: the backend finishes the candidate it is on, then settles the
// run to "stopped". The detail panel keeps polling until the status lands.
export function useStopRun() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) =>
      apiFetch(`/api/v1/runs/${id}/stop`, RunResponse, { method: "POST" }),
    onSuccess: (_data, id) => {
      qc.invalidateQueries({ queryKey: queryKeys.run(id) });
      qc.invalidateQueries({ queryKey: queryKeys.runs });
      qc.invalidateQueries({ queryKey: ["actions"] });
    },
  });
}
