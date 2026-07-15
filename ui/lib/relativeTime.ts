// parseTimestamp parses both timestamp shapes the daemon emits: RFC3339 (with a
// `T` and timezone, used by operations) and the source-metadata format
// "YYYY-MM-DD HH:MM:SS" which is UTC but carries no timezone marker. The latter
// would otherwise be read as *local* time by the browser and appear hours off,
// so we pin it to UTC.
export function parseTimestamp(s: string): Date {
  if (/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(s)) {
    return new Date(s.replace(" ", "T") + "Z");
  }
  return new Date(s);
}

// absoluteTime renders a timestamp as a locale string for a `title` attribute,
// parsing the daemon formats correctly.
export function absoluteTime(s: string): string {
  const d = parseTimestamp(s);
  return Number.isNaN(d.getTime()) ? s : d.toLocaleString();
}

// relativeTime renders a coarse "N units ago" label for a timestamp. Lists
// show this while carrying the absolute time in a `title` (foundation §10).
export function relativeTime(iso: string): string {
  const then = parseTimestamp(iso).getTime();
  if (Number.isNaN(then)) return "";
  const secs = Math.max(0, Math.round((Date.now() - then) / 1000));
  if (secs < 45) return "just now";
  const mins = Math.round(secs / 60);
  if (mins < 60) return `${mins} min ago`;
  const hrs = Math.round(mins / 60);
  if (hrs < 24) return `${hrs} hr ago`;
  const days = Math.round(hrs / 24);
  return `${days} day${days === 1 ? "" : "s"} ago`;
}
