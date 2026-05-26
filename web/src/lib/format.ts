import { getLocale } from "@/paraglide/runtime";

const numberFmtCache = new Map<string, Intl.NumberFormat>();
const relativeFmtCache = new Map<string, Intl.RelativeTimeFormat>();

function numberFmt(opts: Intl.NumberFormatOptions): Intl.NumberFormat {
  const locale = getLocale();
  const key = locale + JSON.stringify(opts);
  let fmt = numberFmtCache.get(key);
  if (!fmt) {
    fmt = new Intl.NumberFormat(locale, opts);
    numberFmtCache.set(key, fmt);
  }
  return fmt;
}

function relativeFmt(): Intl.RelativeTimeFormat {
  const locale = getLocale();
  let fmt = relativeFmtCache.get(locale);
  if (!fmt) {
    fmt = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
    relativeFmtCache.set(locale, fmt);
  }
  return fmt;
}

const byteUnits = ["byte", "kilobyte", "megabyte", "gigabyte", "terabyte", "petabyte"] as const;

export function humanBytes(bytes: number | undefined | null): string {
  if (bytes == null || !isFinite(bytes)) return "—";
  let n = bytes;
  let i = 0;
  while (n >= 1024 && i < byteUnits.length - 1) {
    n /= 1024;
    i++;
  }
  const digits = i === 0 ? 0 : n < 10 ? 2 : 1;
  return numberFmt({
    style: "unit",
    unit: byteUnits[i],
    unitDisplay: "short",
    maximumFractionDigits: digits,
  }).format(n);
}

export function pct(v: number | undefined | null, digits = 1): string {
  if (v == null || !isFinite(v)) return "—";
  return numberFmt({
    style: "percent",
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(v / 100);
}

export function relativeTime(input: string | Date | undefined | null): string {
  if (!input) return "—";
  const d = typeof input === "string" ? new Date(input) : input;
  if (isNaN(d.getTime())) return "—";
  const diffSec = Math.round((d.getTime() - Date.now()) / 1000);
  const abs = Math.abs(diffSec);
  const fmt = relativeFmt();
  if (abs < 60) return fmt.format(diffSec, "second");
  const min = Math.round(diffSec / 60);
  if (Math.abs(min) < 60) return fmt.format(min, "minute");
  const hours = Math.round(min / 60);
  if (Math.abs(hours) < 24) return fmt.format(hours, "hour");
  const days = Math.round(hours / 24);
  if (Math.abs(days) < 30) return fmt.format(days, "day");
  const months = Math.round(days / 30);
  if (Math.abs(months) < 12) return fmt.format(months, "month");
  return fmt.format(Math.round(months / 12), "year");
}

export function shortHash(hash: string, n = 12): string {
  if (!hash) return "—";
  return hash.length <= n ? hash : `${hash.slice(0, n)}…`;
}
