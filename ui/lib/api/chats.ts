import { getSync, deleteSync } from "./envelope";

// ChatTurn is one exchange in a saved conversation, mirroring the daemon's
// chatstore.Turn.
export interface ChatTurn {
  role: "user" | "assistant";
  content: string;
}

// ChatSummary is the transcript-free view of a saved chat from GET /1.0/chats.
export interface ChatSummary {
  id: string;
  title: string;
  created_at: string;
  updated_at: string;
  model?: string;
  bases?: string[] | null;
  turn_count: number;
}

// SavedChat is the full saved chat from GET /1.0/chats/{id}.
export interface SavedChat extends ChatSummary {
  turns: ChatTurn[];
}

// listChats returns saved chat summaries newest-first. A non-empty search filters
// server-side by case-insensitive substring over title and transcript content.
export async function listChats(search?: string): Promise<ChatSummary[]> {
  const path = search ? `/1.0/chats?search=${encodeURIComponent(search)}` : "/1.0/chats";
  const chats = await getSync<ChatSummary[] | null>(path);
  return chats ?? [];
}

// getChat returns a full saved chat including its transcript.
export async function getChat(id: string): Promise<SavedChat> {
  return getSync<SavedChat>(`/1.0/chats/${encodeURIComponent(id)}`);
}

// deleteChat removes a saved chat.
export async function deleteChat(id: string): Promise<void> {
  await deleteSync(`/1.0/chats/${encodeURIComponent(id)}`);
}
