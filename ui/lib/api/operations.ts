import { deleteSync, getSync } from "./envelope";

// OperationView mirrors the daemon's operationView (internal/api/operations.go):
// the JSON representation of a background operation.
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

// Operation status codes, copied from internal/api/response.go. Clients use the
// numeric code rather than the text status; anything >= SUCCESS is terminal.
export const OP_STATUS = {
  operationCreated: 100,
  started: 101,
  running: 103,
  cancelling: 104,
  pending: 105,
  success: 200,
  failure: 400,
  cancelled: 401,
} as const;

export function isTerminal(op: OperationView): boolean {
  return op.status_code >= OP_STATUS.success;
}

export function isRunning(op: OperationView): boolean {
  return !isTerminal(op);
}

export function isSucceeded(op: OperationView): boolean {
  return op.status_code === OP_STATUS.success;
}

export function isFailed(op: OperationView): boolean {
  return op.status_code === OP_STATUS.failure;
}

export function isCancelled(op: OperationView): boolean {
  return op.status_code === OP_STATUS.cancelled;
}

// progressPercent derives a completion percentage from the operation's progress
// metadata. The daemon reports progress as `<thing>_total` / `<thing>_done`
// pairs (sources_total/sources_done, questions_total/questions_done); returns
// null when the operation reports no such pair.
export function progressPercent(op: OperationView): number | null {
  for (const [key, value] of Object.entries(op.metadata)) {
    if (!key.endsWith("_total") || typeof value !== "number" || value <= 0) continue;
    const done = op.metadata[`${key.slice(0, -"_total".length)}_done`];
    if (typeof done !== "number") continue;
    return Math.max(0, Math.min(100, Math.round((done / value) * 100)));
  }
  return null;
}

// normalize fills in the fields the daemon may omit or send as null, so callers
// never have to null-check the maps.
function normalize(op: OperationView): OperationView {
  return { ...op, resources: op.resources ?? {}, metadata: op.metadata ?? {} };
}

// listOperations returns the daemon's current operations.
export async function listOperations(): Promise<OperationView[]> {
  const ops = await getSync<OperationView[] | null>("/1.0/operations");
  return (ops ?? []).map(normalize);
}

// getOperation fetches a single operation by id.
export async function getOperation(id: string): Promise<OperationView> {
  return normalize(await getSync<OperationView>(`/1.0/operations/${id}`));
}

// cancelOperation requests cooperative cancellation. It fails when the
// operation is not cancellable or is already complete.
export async function cancelOperation(id: string): Promise<void> {
  await deleteSync<unknown>(`/1.0/operations/${id}`);
}
