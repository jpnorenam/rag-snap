// Timestamp helpers. Lists show relative time with the absolute time in `title`.

const RELATIVE = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });

const UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ["day", 86400],
  ["hour", 3600],
  ["minute", 60],
  ["second", 1],
];

// relativeTime renders an ISO timestamp as "3 min ago" / "just now".
export function relativeTime(iso: string, now: number = Date.now()): string {
  const seconds = (new Date(iso).getTime() - now) / 1000;
  if (Number.isNaN(seconds)) return "";
  const magnitude = Math.abs(seconds);
  if (magnitude < 10) return "just now";
  for (const [unit, size] of UNITS) {
    if (magnitude >= size) return RELATIVE.format(Math.round(seconds / size), unit);
  }
  return "just now";
}

// absoluteTime renders an ISO timestamp in full, for the `title` attribute.
export function absoluteTime(iso: string): string {
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? "" : date.toLocaleString();
}
