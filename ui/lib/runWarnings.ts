// runWarnings reads the problems a batch run hit without failing — for example
// kapa.ai requested but not configured on the daemon, or a kapa.ai request
// failing. The daemon publishes them as a string list under the run
// operation's "warnings" metadata key (internal/api/handlers_answer.go).
// Anything else under that key is ignored rather than shown as garbage.
export function runWarnings(metadata: Record<string, unknown> | null | undefined): string[] {
  const raw = metadata?.warnings;
  if (!Array.isArray(raw)) return [];
  return raw.filter((w): w is string => typeof w === "string" && w.trim() !== "");
}
