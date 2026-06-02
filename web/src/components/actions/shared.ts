import type { RunResponseT } from "@/api/schemas";
import { shortHash } from "@/lib/format";

// Full torrent name for display; falls back to a short hash when unnamed.
// Truncation is left to CSS (.name-cell/.name-text) so the full name shows
// whenever the column has room, with ellipsis only when it genuinely overflows.
export function torrentLabel(name: string | undefined, hash: string): string {
  return name ? name : shortHash(hash, 12);
}

export function isInFlight(run: RunResponseT) {
  return run.status === "pending" || run.status === "running";
}
