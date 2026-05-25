/**
 * ArrLogo — renders the official SVG logo for each *arr kind from /public/logos/.
 * Falls back to a colored monogram badge if the kind is unknown.
 *
 * Used in both the Dashboard health grid and the Settings *arr connections tiles
 * so both pages stay visually consistent.
 */

const KNOWN_KINDS = ["sonarr", "radarr", "lidarr", "readarr", "whisparr_v2", "whisparr_v3"];

// Fallback colors for unknown kinds
const FALLBACK: Record<string, { bg: string; letter: string }> = {
  sonarr:      { bg: "#35C5F0", letter: "S" },
  radarr:      { bg: "#FFC230", letter: "R" },
  lidarr:      { bg: "#00B46F", letter: "L" },
  readarr:     { bg: "#C33836", letter: "R" },
  whisparr_v2: { bg: "#7B2CBF", letter: "W" },
  whisparr_v3: { bg: "#7B2CBF", letter: "W" },
};

export function ArrLogo({
  kind,
  size = 36,
  greyscale = false,
}: {
  kind: string;
  size?: number;
  greyscale?: boolean;
}) {
  const baseKind = kind.replace(/_v[23]$/, "");   // whisparr_v2 → whisparr for logo path
  const logoSrc = `/logos/${baseKind}.svg`;
  const isKnown = KNOWN_KINDS.includes(kind);

  if (isKnown) {
    return (
      <img
        src={logoSrc}
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

  // Fallback monogram badge
  const { bg, letter } = FALLBACK[kind] ?? { bg: "#666", letter: kind[0]?.toUpperCase() ?? "?" };
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
      }}
    >
      {letter}
    </div>
  );
}
