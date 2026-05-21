import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "./client";
import {
  ActionDetail,
  ActionList,
  ArrList,
  AuthChangePasswordResponse,
  AuthEnableResponse,
  ConfigShape,
  RunActionList,
  RunList,
  RunResponse,
  ScoreList,
  SessionStatus,
  SimpleStatus,
  Summary,
  SnapshotList,
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
  config: ["config"] as const,
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
  sort?: string;
  limit?: number;
  offset?: number;
};

export function useTorrents(params: TorrentsQuery) {
  const search = new URLSearchParams();
  if (params.q) search.set("q", params.q);
  if (params.category) search.set("category", params.category);
  if (params.privateOnly) search.set("private", "1");
  if (params.sort) search.set("sort", params.sort);
  if (params.limit != null) search.set("limit", String(params.limit));
  if (params.offset != null) search.set("offset", String(params.offset));
  return useQuery({
    queryKey: queryKeys.torrents({ ...params }),
    queryFn: () => apiFetch(`/api/v1/torrents?${search.toString()}`, TorrentList),
    refetchInterval: 30_000,
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
    queryKey: queryKeys.runActions(id ?? 0),
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
    queryKey: queryKeys.action(id ?? 0),
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
      qc.invalidateQueries({ queryKey: queryKeys.actions(50, 0) });
    },
  });
}
