// formatBuild renders the running build for the sidebar footer, e.g.
// "v0.0.9 · cd3ac8925556". A numeric snap version gets a "v" prefix; "dev"
// (ragd outside a snap) does not. The commit is left out when ragd doesn't
// know it. Returns "" when there is nothing to show.
export function formatBuild(version?: string, commit?: string): string {
  const v = version ? (/^\d/.test(version) ? `v${version}` : version) : "";
  return [v, commit].filter(Boolean).join(" · ");
}
