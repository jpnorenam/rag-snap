import type { ParsedQAFile } from "./types";

// Cross-app handoff (sender side): pushes the answer-batch results to the
// deployed Web UI over window.postMessage. See docs/handoff-postmessage-contract.md
// for the full contract. The daemon cannot be fetched from a deployed HTTPS
// site (CORS + SameSite=Strict cookie + Private Network Access), so the local
// UI is the sender and pushes the payload out.

// Contract constants (both apps hardcode these).
export const WEB_UI_ORIGIN = "https://canonical-req-8605.web.app";
export const PROTOCOL = "rag-answer-batch-handoff";
export const PROTOCOL_VERSION = 1;
// The receiver must announce READY within this window or the sender falls back
// to a plain download (contract "Timeouts", option (a) — never speculative-post).
export const READY_TIMEOUT_MS = 10000;

// The receive-mode route on the Web UI. The path is illustrative per the
// contract; only the query contract (handoff, nonce, v) is binding. No trailing
// slash: the Web UI's Firebase hosting uses cleanUrls, which maps /import to
// import.html only for the no-slash form. The /import/ form falls through to the
// SPA rewrite and serves the root app instead of the receiver, so the slash
// must be omitted. Query params attach directly: /import?handoff=1&....
const RECEIVE_PATH = "/import";

// HandoffResultsFile is the exported results file — byte-for-byte the CLI's
// BatchOutput and the Export JSON payload. Reused, not a new variant.
export interface HandoffResultsFile {
  generated_at: string;
  model: string;
  results: { id: string; question: string; answer: string }[];
}

// HandoffPayload is the `payload` field of a HANDOFF_PAYLOAD message.
export interface HandoffPayload {
  kind: "answer-batch-results";
  source: "rag-cli-local-ui";
  manifest_name?: string;
  knowledge_bases?: string[];
  results_file: HandoffResultsFile;
}

// Envelope fields shared by every message.
interface HandoffEnvelope {
  protocol: typeof PROTOCOL;
  version: typeof PROTOCOL_VERSION;
  type: "HANDOFF_READY" | "HANDOFF_PAYLOAD" | "HANDOFF_ACK" | "HANDOFF_ERROR";
  nonce: string;
}

// FallbackReason distinguishes the two ways the handoff can fall back to a
// download so the UI can tell the user which happened.
export type FallbackReason = "popup-blocked" | "timeout";

// CollaborateHandlers are the sender's callbacks. onAck fires when the receiver
// confirms it accepted (and, per contract, persisted) the payload; onFallback
// fires when we could not hand off and the caller should download instead.
export interface CollaborateHandlers {
  onAck?: () => void;
  onFallback: (reason: FallbackReason) => void;
}

// buildPayload assembles the HANDOFF_PAYLOAD `payload` from a parsed results
// file plus its display metadata. `results_file` mirrors the Export JSON shape.
export function buildPayload(
  parsed: ParsedQAFile,
  manifestName?: string,
  knowledgeBases?: string[]
): HandoffPayload {
  return {
    kind: "answer-batch-results",
    source: "rag-cli-local-ui",
    ...(manifestName ? { manifest_name: manifestName } : {}),
    ...(knowledgeBases && knowledgeBases.length > 0 ? { knowledge_bases: knowledgeBases } : {}),
    results_file: {
      generated_at: parsed.generated_at,
      model: parsed.model,
      results: parsed.items,
    },
  };
}

// isEnvelope narrows an untrusted message-event payload to a handoff envelope
// from the Web UI: it must be the right protocol/version and echo our nonce.
// Origin is checked separately by the caller (exact WEB_UI_ORIGIN match).
function isEnvelope(data: unknown, nonce: string): data is HandoffEnvelope {
  if (typeof data !== "object" || data === null) return false;
  const m = data as Record<string, unknown>;
  return (
    m.protocol === PROTOCOL &&
    m.version === PROTOCOL_VERSION &&
    typeof m.type === "string" &&
    m.nonce === nonce
  );
}

// collaborateInWebUI runs the sender side of the handshake:
//   1. install the message listener FIRST (before opening the tab),
//   2. open the Web UI in receive-mode with a fresh nonce,
//   3. on a validated HANDOFF_READY, post HANDOFF_PAYLOAD with targetOrigin
//      pinned to WEB_UI_ORIGIN,
//   4. resolve onAck on HANDOFF_ACK.
// Falls back via onFallback when the popup is blocked (null window) or no READY
// arrives within READY_TIMEOUT_MS. Returns a canceller the caller invokes on
// unmount to tear down the listener/timer.
export function collaborateInWebUI(
  payload: HandoffPayload,
  handlers: CollaborateHandlers
): () => void {
  // crypto.randomUUID is available in all browsers targeted by the static export
  // and on the secure/loopback origins this UI is served from.
  const nonce = crypto.randomUUID();

  let settled = false;
  let readyTimer: ReturnType<typeof setTimeout> | null = null;

  function cleanup() {
    window.removeEventListener("message", onMessage);
    if (readyTimer !== null) {
      clearTimeout(readyTimer);
      readyTimer = null;
    }
  }

  function fallback(reason: FallbackReason) {
    if (settled) return;
    settled = true;
    cleanup();
    handlers.onFallback(reason);
  }

  function onMessage(event: MessageEvent) {
    // Validate the receiver by exact origin, then protocol/version/nonce.
    if (event.origin !== WEB_UI_ORIGIN) return;
    if (!isEnvelope(event.data, nonce)) return;

    if (event.data.type === "HANDOFF_READY") {
      if (settled || readyTimer === null) return; // ignore late/duplicate READY
      clearTimeout(readyTimer);
      readyTimer = null;
      // Post the payload only to the intended app, even if the tab navigated.
      const message = {
        protocol: PROTOCOL,
        version: PROTOCOL_VERSION,
        type: "HANDOFF_PAYLOAD" as const,
        nonce,
        payload,
      };
      // event.source is the receiver window; prefer it, fall back to the opened
      // handle. targetOrigin is always pinned — never "*".
      const target = event.source as Window | null;
      if (target) target.postMessage(message, WEB_UI_ORIGIN);
      else if (win) win.postMessage(message, WEB_UI_ORIGIN);
      return;
    }

    if (event.data.type === "HANDOFF_ACK") {
      if (settled) return;
      settled = true;
      cleanup();
      handlers.onAck?.();
      return;
    }

    // HANDOFF_ERROR (or anything else): treat as a failed handoff so the user
    // is never left without their results.
    if (event.data.type === "HANDOFF_ERROR") {
      fallback("timeout");
    }
  }

  // 1. Listener first, so we cannot miss a fast READY.
  window.addEventListener("message", onMessage);

  // 2. Open the receiver in receive-mode.
  const url = `${WEB_UI_ORIGIN}${RECEIVE_PATH}?handoff=1&nonce=${encodeURIComponent(
    nonce
  )}&v=${PROTOCOL_VERSION}`;
  const win = window.open(url);

  // Popup blocked: there is no window to hand off to.
  if (win === null) {
    fallback("popup-blocked");
    return cleanup;
  }

  // 3/4. Wait for READY; fall back to a download if it never arrives.
  readyTimer = setTimeout(() => fallback("timeout"), READY_TIMEOUT_MS);

  return cleanup;
}
