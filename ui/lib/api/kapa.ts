import { getSync } from "./envelope";

// KapaSourceGroup is a kapa.ai source group, matching the daemon's
// kapaSourceGroup: the id a manifest selects it by (kapa_source_groups) and the
// name shown to the user.
export interface KapaSourceGroup {
  id: string;
  name: string;
}

// KapaSourceGroups is the GET /1.0/kapa/source-groups view. configured is false
// when kapa.ai is disabled or the daemon has no project id or API key, which is
// distinct from a configured project that simply has no groups.
export interface KapaSourceGroups {
  configured: boolean;
  groups: KapaSourceGroup[];
}

// listKapaSourceGroups lists the kapa.ai project's source groups. A failing
// upstream request rejects with an ApiError (502) rather than resolving to an
// empty list.
export async function listKapaSourceGroups(): Promise<KapaSourceGroups> {
  const view = await getSync<{ configured?: boolean; groups?: KapaSourceGroup[] | null } | null>(
    "/1.0/kapa/source-groups"
  );
  return { configured: view?.configured ?? false, groups: view?.groups ?? [] };
}
