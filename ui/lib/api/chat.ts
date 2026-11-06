import { postAsync } from "./envelope";
import { ROOT_PATH } from "./rootPath";
import { getToken } from "./token";
import type { ChatTurn } from "./chats";

// RestoredChat is the transcript and knowledge-base context recovered when a
// session is started by resuming a saved chat (POST /1.0/chat with `resume`).
export interface RestoredChat {
  id: string;
  title: string;
  turns: ChatTurn[];
  bases?: string[] | null;
  dropped_bases?: string[] | null;
  // The restored kapa.ai selection, and whether a saved one could not be
  // applied because kapa.ai is not configured on the daemon.
  kapa_groups?: string[] | null;
  kapa_unavailable?: boolean;
}

// chatStartMetadata is the operation's own metadata: the resolved model, the
// websocket connect URL and one-time secret, and (on resume) the restored chat.
interface ChatStartMetadata {
  model?: string;
  websocket?: {
    url: string;
    secret: string;
  };
  chat?: RestoredChat;
  kapa_groups?: string[] | null;
  kapa_unavailable?: boolean;
}

// chatStartOperation is the operation view carried in the async envelope's
// metadata. POST /1.0/chat returns an async (operation) response, so the
// connect details live in the operation view's *own* nested `metadata` field.
interface ChatStartOperation {
  metadata?: ChatStartMetadata;
}

// ChatSession holds the resolved connect details for a started chat session.
// restored is set only when the session was started by resuming a saved chat.
export interface ChatSession {
  model: string;
  websocketUrl: string;
  restored?: RestoredChat;
  // The effective kapa.ai selection, and whether a requested one could not be
  // applied because kapa.ai is not configured on the daemon.
  kapaGroups: string[];
  kapaUnavailable: boolean;
}

// ChatStartOptions mirror the optional POST /1.0/chat request body. resume seeds
// the session from the saved chat with that id; prompt selects a named
// chat_system_prompt variant for this session (the reserved name "default"
// forces the built-in default), overriding the slot's active pointer.
export interface ChatStartOptions {
  model?: string;
  bases?: string[];
  temperature?: number;
  resume?: string;
  prompt?: string;
  // kapa_source_groups is the initial kapa.ai selection (group ids).
  kapa_source_groups?: string[];
}

// startChat issues POST /1.0/chat and resolves the websocket URL (with the
// one-time secret applied) from the returned operation metadata.
export async function startChat(opts: ChatStartOptions = {}): Promise<ChatSession> {
  const { metadata: op } = await postAsync<ChatStartOperation>("/1.0/chat", opts);
  const meta = op.metadata;
  const ws = meta?.websocket;
  if (!ws?.url || !ws.secret) {
    throw new Error("chat operation did not return a websocket URL/secret");
  }
  return {
    model: meta?.model ?? opts.model ?? "",
    websocketUrl: buildWsUrl(ws.url, ws.secret),
    restored: meta?.chat,
    kapaGroups: meta?.kapa_groups ?? [],
    kapaUnavailable: meta?.kapa_unavailable ?? false,
  };
}

// buildWsUrl turns the operation's websocket path + secret into an absolute
// ws(s):// URL on the current origin. The daemon returns a same-origin path
// like /1.0/operations/<id>/websocket; we resolve it against the page origin so
// the socket is reachable directly (no CORS), then append the secret.
function buildWsUrl(opPath: string, secret: string): string {
  // opPath may already be absolute (http/ws) or a root-relative path.
  const origin = window.location.origin.replace(/^http/, "ws");
  const base = /^(wss?|https?):/.test(opPath)
    ? opPath.replace(/^http/, "ws")
    : `${origin}${ROOT_PATH}${opPath.startsWith("/") ? opPath : `/${opPath}`}`;
  const sep = base.includes("?") ? "&" : "?";
  return `${base}${sep}secret=${encodeURIComponent(secret)}`;
}

// Server→client frame types on the chat websocket. A "saved" frame acknowledges
// a save control message, carrying the saved chat's id and title. An
// "active-kapa-groups" frame acknowledges a kapa.ai selection (with an error
// when kapa.ai is not configured); a "warning" is a non-fatal notice about the
// current turn, such as a failed kapa.ai request — the turn still completes.
export type ChatFrameType =
  | "token"
  | "think"
  | "done"
  | "active-kbs"
  | "active-kapa-groups"
  | "warning"
  | "saved"
  | "error";

export interface ChatFrame {
  type: ChatFrameType;
  content?: string;
  bases?: string[];
  kapa_groups?: string[];
  error?: string;
  id?: string;
  title?: string;
}

// ChatConnection wraps the websocket with typed send helpers for the control
// frames the daemon understands (`prompt`, `set-active-kbs`).
export class ChatConnection {
  private ws: WebSocket;

  constructor(
    url: string,
    handlers: {
      onFrame: (frame: ChatFrame) => void;
      onOpen?: () => void;
      onClose?: (ev: CloseEvent) => void;
      onError?: () => void;
    }
  ) {
    // The token cookie travels with the websocket upgrade automatically
    // (same-origin). When a fragment token is in use we cannot set a header on
    // a browser WebSocket, so cookie-based handoff is the supported path; the
    // secret query param already authorizes the specific operation.
    void getToken();
    this.ws = new WebSocket(url);
    this.ws.onmessage = (ev) => {
      try {
        handlers.onFrame(JSON.parse(ev.data) as ChatFrame);
      } catch {
        handlers.onFrame({ type: "error", error: "malformed frame from server" });
      }
    };
    if (handlers.onOpen) this.ws.onopen = handlers.onOpen;
    if (handlers.onClose) this.ws.onclose = handlers.onClose;
    if (handlers.onError) this.ws.onerror = handlers.onError;
  }

  // prompt submits a question over the open connection.
  prompt(content: string): void {
    this.ws.send(JSON.stringify({ type: "prompt", content }));
  }

  // setActiveBases changes the active knowledge bases mid-session.
  setActiveBases(bases: string[]): void {
    this.ws.send(JSON.stringify({ type: "set-active-kbs", bases }));
  }

  // setActiveKapaGroups changes the kapa.ai source-group selection (ids)
  // mid-session, independently of the knowledge bases.
  setActiveKapaGroups(ids: string[]): void {
    this.ws.send(JSON.stringify({ type: "set-active-kapa-groups", kapa_groups: ids }));
  }

  // save persists the running conversation; the daemon replies with a "saved" or
  // "error" frame.
  save(title: string): void {
    this.ws.send(JSON.stringify({ type: "save", title }));
  }

  close(): void {
    this.ws.close();
  }

  get readyState(): number {
    return this.ws.readyState;
  }
}
