import { postAsync } from "./envelope";
import { ROOT_PATH } from "./rootPath";
import { getToken } from "./token";

// chatStartMetadata is the operation metadata returned by POST /1.0/chat: the
// resolved model plus the websocket connect URL and one-time secret.
interface ChatStartMetadata {
  model?: string;
  websocket?: {
    url: string;
    secret: string;
  };
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
  const { metadata } = await postAsync<ChatStartMetadata>("/1.0/chat", opts);
  const ws = metadata.websocket;
  if (!ws?.url || !ws.secret) {
    throw new Error("chat operation did not return a websocket URL/secret");
  }
  return { model: metadata.model ?? opts.model ?? "", websocketUrl: buildWsUrl(ws.url, ws.secret) };
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
