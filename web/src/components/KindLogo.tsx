// KindLogo renders the SVG logo for a kind from /public/logos/, or a coloured
// monogram badge as fallback. ArrLogo and TorrentClientLogo are thin wrappers
// over this primitive — same visual contract, different catalogs.

type Fallback = { bg: string; letter: string };

type Props = {
  kind: string;
  size?: number;
  greyscale?: boolean;
  /** Names that should resolve to /logos/<resolved>.svg. */
  knownKinds: readonly string[];
  fallback: Record<string, Fallback>;
  /** Optional transform from kind to logo basename (default: identity). */
  logoBasename?: (kind: string) => string;
};

export function KindLogo({
  kind,
  size = 36,
  greyscale = false,
  knownKinds,
  fallback,
  logoBasename,
}: Props) {
  const isKnown = knownKinds.includes(kind);

  if (isKnown) {
    const basename = logoBasename ? logoBasename(kind) : kind;
    return (
      <img
        src={`/logos/${basename}.svg`}
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

  const { bg, letter } = fallback[kind] ?? {
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
