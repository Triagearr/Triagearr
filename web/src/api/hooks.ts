import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "./client";
import {
  ActionDetail,
  ActionList,
  ArrConnection,
  ArrConnectionList,
  ArrList,
  AuthChangePasswordResponse,
  AuthEnableResponse,
  ConfigShape,
  SettingsView,
  RunActionList,
  RunList,
  RunResponse,
  ScoreList,
  SessionStatus,
  SimpleStatus,
  Summary,
  SnapshotList,
  TorrentCategories,
  TorrentDetail,
  TorrentList,
  Version,
  VolumeHistory,
  VolumeList,
} from "./schemas";

export const queryKeys = {
  session: ["session"] as const,
  version: ["version"] as const,
  summary: ["summary"] as const,
  volumes: ["volumes"] as const,
  volumeHistory: (name: string, since: string) => ["volumes", name, "history", since] as const,
  torrents: (params: Record<string, string | number | boolean>) => ["torrents", params] as const,
  torrent: (hash: string) => ["torrent", hash] as const,
  snapshots: (hash: string, since: string) => ["torrent", hash, "snapshots", since] as const,
  scores: ["scores"] as const,
  runs: ["runs"] as const,
  runActions: (id: number) => ["run", id, "actions"] as const,
  actions: (limit: number, offset: number) => ["actions", limit, offset] as const,
  action: (id: number) => ["action", id] as const,
  arrs: ["arrs"] as const,
  arrConnections: ["arr-connections"] as const,
  config: ["config"] as const,
  settings: ["settings"] as const,
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

export function useVolumes() {
  return useQuery({
    queryKey: queryKeys.volumes,
    queryFn: () => apiFetch("/api/v1/volumes", VolumeList),
    refetchInterval: 15_000,
  });
}

export function useVolumeHistory(name: string, since = "24h") {
  return useQuery({
    queryKey: queryKeys.volumeHistory(name, since),
    queryFn: () => apiFetch(`/api/v1/volumes/${encodeURIComponent(name)}/history?since=${since}`, VolumeHistory),
    enabled: Boolean(name),
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
    refetchInterval: 15_000,
  });
}

export function useRunActions(id: number | undefined) {
  return useQuery({
    queryKey: id != null ? queryKeys.runActions(id) : ["run", "noop"],
    queryFn: () => apiFetch(`/api/v1/runs/${id}/actions`, RunActionList),
    enabled: Boolean(id),
  });
}

export function useActions(limit = 50, offset = 0) {
  return useQuery({
    queryKey: queryKeys.actions(limit, offset),
    queryFn: () => apiFetch(`/api/v1/actions?limit=${limit}&offset=${offset}`, ActionList),
    refetchInterval: 30_000,
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
// value:null deletes the key (reverts to YAML default). The server returns
// 202 and triggers a self-SIGHUP; callers should wait ~1s and re-fetch.
export function useUpdateSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (overrides: SettingsOverrideInput[]) => {
      const res = await fetch("/api/v1/settings", {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ overrides }),
      });
      if (!res.ok) {
        const text = await res.text();
        throw new Error(text || res.statusText);
      }
    },
    onSuccess: () => {
      // Refresh after the daemon's reload window. 1.5s is generous on the
      // local sqlite + in-process restart path.
      setTimeout(() => {
        qc.invalidateQueries({ queryKey: queryKeys.settings });
        qc.invalidateQueries({ queryKey: queryKeys.config });
        qc.invalidateQueries({ queryKey: queryKeys.summary });
      }, 1500);
    },
  });
}

// POST /api/v1/notifications/test — delivers a synthetic notification through
// the saved provider config so the operator can verify credentials. Tests the
// currently-loaded config, so unsaved edits must be saved first.
export function useTestNotification() {
  return useMutation({
    mutationFn: async () => {
      const res = await fetch("/api/v1/notifications/test", {
        method: "POST",
        credentials: "include",
      });
      if (!res.ok) {
        let msg = res.statusText;
        try {
          const body = (await res.json()) as { error?: string };
          if (body?.error) msg = body.error;
        } catch {
          // non-JSON body — keep statusText
        }
        throw new Error(msg);
      }
    },
  });
}

// --- *arr connections (ADR-0022) ----------------------------------------

export type ArrConnectionInput = {
  kind: string;
  name: string;
  url: string;
  api_key: string;
  enabled: boolean;
  poll: boolean;
  act: boolean;
  tags_exclude: string[];
  categories_only: string[];
  timeout_seconds: number;
};

export function useArrConnections() {
  return useQuery({
    queryKey: queryKeys.arrConnections,
    queryFn: () => apiFetch("/api/v1/arr-connections", ArrConnectionList),
  });
}

// A connection change triggers a daemon self-SIGHUP; the registry rebuilds
// after a short window, so we re-invalidate the arr views once it settles.
function invalidateArrConnections(qc: ReturnType<typeof useQueryClient>) {
  qc.invalidateQueries({ queryKey: queryKeys.arrConnections });
  setTimeout(() => {
    qc.invalidateQueries({ queryKey: queryKeys.arrConnections });
    qc.invalidateQueries({ queryKey: queryKeys.arrs });
    qc.invalidateQueries({ queryKey: queryKeys.summary });
  }, 1500);
}

export function useCreateArrConnection() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: ArrConnectionInput) =>
      apiFetch("/api/v1/arr-connections", ArrConnection, {
        method: "POST",
        body: JSON.stringify(input),
      }),
    onSuccess: () => invalidateArrConnections(qc),
  });
}

export function useUpdateArrConnection() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: ArrConnectionInput }) =>
      apiFetch(`/api/v1/arr-connections/${id}`, ArrConnection, {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    onSuccess: () => invalidateArrConnections(qc),
  });
}

export function useDeleteArrConnection() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: number) => {
      const res = await fetch(`/api/v1/arr-connections/${id}`, {
        method: "DELETE",
        credentials: "include",
      });
      if (!res.ok) {
        const text = await res.text();
        throw new Error(text || res.statusText);
      }
    },
    onSuccess: () => invalidateArrConnections(qc),
  });
}

// POST /api/v1/arr-connections/test — pings the posted credentials so the
// operator can verify a connection before saving it.
export function useTestArrConnection() {
  return useMutation({
    mutationFn: async (input: {
      kind: string;
      url: string;
      api_key: string;
      timeout_seconds: number;
    }) => {
      const res = await fetch("/api/v1/arr-connections/test", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(input),
      });
      if (!res.ok) {
        let msg = res.statusText;
        try {
          const body = (await res.json()) as { error?: string };
          if (body?.error) msg = body.error;
        } catch {
          // non-JSON body — keep statusText
        }
        throw new Error(msg);
      }
    },
  });
}

export function useTriggerRun() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ volume, mode }: { volume?: string; mode?: "live" | "dry-run" }) =>
      apiFetch("/api/v1/runs", RunResponse, {
        method: "POST",
        body: JSON.stringify({ volume, mode }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.runs });
      qc.invalidateQueries({ queryKey: queryKeys.summary });
      qc.invalidateQueries({ queryKey: ["actions"] });
    },
  });
}
