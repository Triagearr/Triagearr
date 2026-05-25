/**
 * TorrentClientLogo — renders the SVG logo for each torrent client kind from
 * /public/logos/. qBittorrent / Transmission / Deluge come from simpleicons
 * (colorised with the brand colour); rTorrent has a custom monogram SVG
 * because simpleicons doesn't ship one. Falls back to a coloured monogram
 * badge for any kind we don't know about.
 *
 * Mirrors ArrLogo.tsx so the *arr-connections and torrent-connections
 * settings pages stay visually consistent.
 */

const KNOWN_KINDS = ["qbittorrent", "transmission", "deluge", "rtorrent"];

const FALLBACK: Record<string, { bg: string; letter: string }> = {
  qbittorrent:  { bg: "#2F67BC", letter: "Q" },
  transmission: { bg: "#D9272B", letter: "T" },
  deluge:       { bg: "#1E4173", letter: "D" },
  rtorrent:     { bg: "#1E7A4D", letter: "R" },
};

export function TorrentClientLogo({
  kind,
  size = 36,
  greyscale = false,
}: {
  kind: string;
  size?: number;
  greyscale?: boolean;
}) {
  const isKnown = KNOWN_KINDS.includes(kind);

  if (isKnown) {
    return (
      <img
        src={`/logos/${kind}.svg`}
        alt={kind}
        width={size}
        height={size}
        draggable={false}
        style={{
          width: size,
          height: size,
          objectFit: "contain",
          flex: "none",
          filter: greyscale ? "grayscale(1) opacity(0.5)" : undefined,
        }}
      />
    );
  }

  const { bg, letter } = FALLBACK[kind] ?? {
    bg: "#666",
    letter: kind[0]?.toUpperCase() ?? "?",
  };
  return (
    <div
      style={{
        width: size,
        height: size,
        borderRadius: size * 0.22,
        background: `linear-gradient(140deg, ${bg}, color-mix(in srgb, ${bg} 70%, black))`,
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        color: "white",
        fontWeight: 600,
        fontSize: size * 0.46,
        letterSpacing: "-0.04em",
        boxShadow: `inset 0 1px 0 rgba(255,255,255,.22), 0 1px 2px rgba(0,0,0,.18)`,
        flex: "none",
        opacity: greyscale ? 0.4 : 1,
        filter: greyscale ? "grayscale(1)" : undefined,
      }}
    >
      {letter}
    </div>
  );
}
