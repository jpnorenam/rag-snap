"use client";

// Runtime token handling for the loopback auth path. The token is NEVER baked
// into the build: `rag ui` sets a daemon cookie scoped to the loopback origin,
// so same-origin fetches and the chat websocket carry it automatically and no
// JS handling is required in the common case.
//
// As a fallback (and to support handoff mechanisms that pass the token in the
// URL fragment), we also accept a token captured at load time into
// sessionStorage and surface it as a Bearer header. When a token arrives in the
// URL fragment we consume it and immediately strip it from the address bar so
// it does not leak via history/referrer.

const STORAGE_KEY = "ragApiToken";

// captureTokenFromUrl reads a `#token=...` fragment, stores it in sessionStorage,
// and scrubs it from the location bar. Safe to call repeatedly; a no-op when no
// fragment token is present.
export function captureTokenFromUrl(): void {
  if (typeof window === "undefined") return;
  const hash = window.location.hash;
  if (!hash.includes("token=")) return;
  const params = new URLSearchParams(hash.replace(/^#/, ""));
  const token = params.get("token");
  if (token) {
    sessionStorage.setItem(STORAGE_KEY, token);
  }
  // Remove the fragment from the URL without reloading the page.
  history.replaceState(null, "", window.location.pathname + window.location.search);
}

// getToken returns the runtime token, if one was captured. Returns null when the
// cookie-based flow is in use (the common case).
export function getToken(): string | null {
  if (typeof window === "undefined") return null;
  return sessionStorage.getItem(STORAGE_KEY);
}

// authHeaders builds the request headers carrying the token, if present.
export function authHeaders(): Record<string, string> {
  const token = getToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}
