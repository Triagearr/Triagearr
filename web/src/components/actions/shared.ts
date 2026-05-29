import type { RunResponseT } from "@/api/schemas";
import { shortHash } from "@/lib/format";

export function torrentLabel(name: string | undefined, hash: string, maxLen = 40): string {
  if (name) return name.length > maxLen ? name.slice(0, maxLen - 1) + "…" : name;
  return shortHash(hash, 12);
}

export function isInFlight(run: RunResponseT) {
  return run.status === "pending" || run.status === "running";
}
