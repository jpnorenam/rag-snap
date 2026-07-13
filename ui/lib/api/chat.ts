import { postAsync, wsUrl } from "./envelope";
import { getToken } from "./token";

// chatStartMetadata is the operation's own metadata: the resolved model plus
// the websocket connect URL and one-time secret.
interface ChatStartMetadata {
  model?: string;
  websocket?: {
    url: string;
    secret: string;
  };
}

// chatStartOperation is the operation view carried in the async envelope's
// metadata. POST /1.0/chat returns an async (operation) response, so the
// connect details live in the operation view's *own* nested `metadata` field.
interface ChatStartOperation {
  metadata?: ChatStartMetadata;
}

// ChatSession holds the resolved connect details for a started chat session.
export interface ChatSession {
  model: string;
  websocketUrl: string;
}

// ChatStartOptions mirror the optional POST /1.0/chat request body.
export interface ChatStartOptions {
  model?: string;
  bases?: string[];
  temperature?: number;
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
  return { model: meta?.model ?? opts.model ?? "", websocketUrl: buildWsUrl(ws.url, ws.secret) };
}

// buildWsUrl turns the operation's websocket path + secret into an absolute
// ws(s):// URL on the current origin, then appends the one-time secret that
// authorizes the connect.
function buildWsUrl(opPath: string, secret: string): string {
  const base = wsUrl(opPath);
  const sep = base.includes("?") ? "&" : "?";
  return `${base}${sep}secret=${encodeURIComponent(secret)}`;
}

// Server→client frame types on the chat websocket.
export type ChatFrameType = "token" | "think" | "done" | "active-kbs" | "error";

export interface ChatFrame {
  type: ChatFrameType;
  content?: string;
  bases?: string[];
  error?: string;
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

  close(): void {
    this.ws.close();
  }

  get readyState(): number {
    return this.ws.readyState;
  }
}
