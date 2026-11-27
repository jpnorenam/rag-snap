import type { ChatFrame } from "@/lib/api/chat";
import type { CitedSource } from "@/lib/types";

// A turn in the chat transcript. Assistant turns accumulate streamed
// token/think content incrementally as frames arrive. The daemon streams the
// answer with its final, per-answer marker numbers, so content renders as is.
// `citedSoFar` holds the documents the latest `sources` frame announced (so
// markers render as chips while streaming); `sources` holds the ones the
// finished answer cited.
export interface Message {
  role: "user" | "assistant";
  content: string;
  think: string;
  streaming?: boolean;
  citedSoFar?: CitedSource[];
  sources?: CitedSource[];
  unsourced?: boolean;
}

// Shown under an answer that cited none of the documents retrieved for it.
export const NO_SOURCES_NOTICE = "No relevant sources: this answer cites none of the retrieved documents.";

// applyAnswerFrame folds a turn's citation-bearing frames into the transcript:
// a "sources" frame records the documents the streaming answer has cited so far;
// a "done" frame ends it, keeping the cited sources, or flagging the answer
// unsourced when it cited none of them.
export function applyAnswerFrame(prev: Message[], frame: ChatFrame): Message[] {
  const last = prev[prev.length - 1];
  if (!last || last.role !== "assistant") return prev;
  let updated: Message;
  switch (frame.type) {
    case "sources":
      if (!last.streaming) return prev;
      updated = { ...last, citedSoFar: frame.sources ?? [] };
      break;
    case "done":
      updated = {
        ...last,
        streaming: false,
        sources: frame.unsourced ? [] : (frame.sources ?? []),
        unsourced: frame.unsourced || undefined,
      };
      break;
    default:
      return prev;
  }
  return [...prev.slice(0, -1), updated];
}
