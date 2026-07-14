import { deleteSync, getSync } from "./envelope";
import { ROOT_PATH } from "./rootPath";
import { getToken } from "./token";

// OperationView mirrors the daemon's operationView (internal/api/operations.go):
// every field the events websocket and the REST endpoints return for a single
// background operation. Clients switch on the numeric `status_code`, never the
// human `status` text.
export interface OperationView {
  id: string;
  class: string;
  description: string;
  created_at: string;
  updated_at: string;
  status: string;
  status_code: number;
  resources: Record<string, string[]>;
  metadata: Record<string, unknown>;
  may_cancel: boolean;
  err: string;
}

// Numeric status codes from internal/api/response.go. 100–199 are
// running/intermediate, 200–399 success, 400+ failure; 401 is specifically a
// cancelled operation.
export const STATUS_RUNNING = 103;
export const STATUS_SUCCESS = 200;
export const STATUS_FAILURE = 400;
export const STATUS_CANCELLED = 401;

// OpStatus is the coarse UI-facing status derived from the numeric code.
export type OpStatus = "running" | "succeeded" | "failed" | "cancelled";

// statusOf maps an operation's numeric status_code to a coarse UI status,
// following the daemon's ranges (terminal is >= 200; cancelled is exactly 401).
export function statusOf(op: OperationView): OpStatus {
  const code = op.status_code;
  if (code === STATUS_CANCELLED) return "cancelled";
  if (code >= STATUS_FAILURE) return "failed";
  if (code >= STATUS_SUCCESS) return "succeeded";
  return "running";
}

// isTerminal reports whether an operation has reached a final state.
export function isTerminal(op: OperationView): boolean {
  return op.status_code >= STATUS_SUCCESS;
}

// normalize fills in the collection fields the daemon may serialize as null so
// callers can index them without guards.
function normalize(op: OperationView): OperationView {
  return {
    ...op,
    resources: op.resources ?? {},
    metadata: op.metadata ?? {},
  };
}

// listOperations returns a snapshot of every current operation (GET
// /1.0/operations). A null array normalizes to [].
export async function listOperations(): Promise<OperationView[]> {
  const ops = await getSync<OperationView[] | null>("/1.0/operations");
  return (ops ?? []).map(normalize);
}

// getOperation fetches a single operation by id (GET /1.0/operations/{id}).
export async function getOperation(id: string): Promise<OperationView> {
  const op = await getSync<OperationView>(`/1.0/operations/${encodeURIComponent(id)}`);
  return normalize(op);
}

// cancelOperation requests cooperative cancellation (DELETE
// /1.0/operations/{id}) and returns the updated view. The daemon rejects the
// request (throwing ApiError) when the operation cannot be cancelled.
export async function cancelOperation(id: string): Promise<OperationView> {
  const op = await deleteSync<OperationView>(`/1.0/operations/${encodeURIComponent(id)}`);
  return normalize(op);
}

// An events envelope as published on the /1.0/events websocket. For operation
// events, `metadata` is a full OperationView.
export interface OperationEvent {
  type: string;
  timestamp: string;
  metadata: OperationView;
}

// eventsSocketUrl builds the absolute ws(s):// URL for the operation events
// stream on the current origin, using the same origin-rewrite logic as
// buildWsUrl in chat.ts. Cookie auth rides the upgrade (a browser WebSocket
// cannot set an Authorization header); a fragment token, if any, is captured
// but only usable on same-origin cookie handoff.
export function eventsSocketUrl(): string {
  void getToken();
  const origin = window.location.origin.replace(/^http/, "ws");
  return `${origin}${ROOT_PATH}/1.0/events?type=operation`;
}

// connectOperationEvents opens the operation events websocket and invokes
// onOperation for each operation event's view. Returns the raw WebSocket so the
// caller owns its lifecycle (backoff/reconnect, close on unmount).
export function connectOperationEvents(handlers: {
  onOperation: (op: OperationView) => void;
  onOpen?: () => void;
  onClose?: () => void;
  onError?: () => void;
}): WebSocket {
  const ws = new WebSocket(eventsSocketUrl());
  ws.onmessage = (ev) => {
    try {
      const msg = JSON.parse(ev.data) as OperationEvent;
      if (msg.type === "operation" && msg.metadata) {
        handlers.onOperation(normalize(msg.metadata));
      }
    } catch {
      // Ignore malformed frames; the seed/poll paths are the source of truth.
    }
  };
  if (handlers.onOpen) ws.onopen = handlers.onOpen;
  if (handlers.onClose) ws.onclose = handlers.onClose;
  if (handlers.onError) ws.onerror = handlers.onError;
  return ws;
}
