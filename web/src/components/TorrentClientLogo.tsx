import { KindLogo } from "@/components/KindLogo";

// TorrentClientLogo — SVG logos for torrent clients (qBittorrent / Transmission
// / Deluge from simpleicons, rTorrent custom). Mirrors ArrLogo so the *arr-
// connections and torrent-connections settings pages stay visually consistent.

const KNOWN_KINDS = ["qbittorrent", "transmission", "deluge", "rtorrent"] as const;

const FALLBACK = {
  qbittorrent:  { bg: "#2F67BC", letter: "Q" },
  transmission: { bg: "#D9272B", letter: "T" },
  deluge:       { bg: "#1E4173", letter: "D" },
  rtorrent:     { bg: "#1E7A4D", letter: "R" },
};

export function TorrentClientLogo(props: { kind: string; size?: number; greyscale?: boolean }) {
  return <KindLogo {...props} knownKinds={KNOWN_KINDS} fallback={FALLBACK} />;
}
