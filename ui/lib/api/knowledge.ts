import { getSync } from "./envelope";

// KnowledgeBase is the API view of a knowledge base from GET /1.0/knowledge,
// matching the daemon's knowledgeBaseSummary.
export interface KnowledgeBase {
  name: string;
  index: string;
  health?: string;
  status?: string;
  docs_count?: string;
  store_size?: string;
}

// listKnowledge fetches the available knowledge bases for the active-KB
// selector. Returns an empty array when the knowledge backend is unconfigured.
export async function listKnowledge(): Promise<KnowledgeBase[]> {
  const bases = await getSync<KnowledgeBase[] | null>("/1.0/knowledge");
  return bases ?? [];
}
