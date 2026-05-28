import { KindLogo } from "@/components/KindLogo";

// ArrLogo — official *arr SVG from /public/logos/ with monogram fallback.
// Used by the Dashboard health grid and the Settings *arr-connections tiles.

const KNOWN_KINDS = ["sonarr", "radarr", "lidarr", "readarr", "whisparr_v2", "whisparr_v3"] as const;

const FALLBACK = {
  sonarr:      { bg: "#35C5F0", letter: "S" },
  radarr:      { bg: "#FFC230", letter: "R" },
  lidarr:      { bg: "#00B46F", letter: "L" },
  readarr:     { bg: "#C33836", letter: "R" },
  whisparr_v2: { bg: "#7B2CBF", letter: "W" },
  whisparr_v3: { bg: "#7B2CBF", letter: "W" },
};

// whisparr_v2/_v3 → whisparr for the logo file
const arrBasename = (kind: string) => kind.replace(/_v[23]$/, "");

export function ArrLogo(props: { kind: string; size?: number; greyscale?: boolean }) {
  return (
    <KindLogo
      {...props}
      knownKinds={KNOWN_KINDS}
      fallback={FALLBACK}
      logoBasename={arrBasename}
    />
  );
}
