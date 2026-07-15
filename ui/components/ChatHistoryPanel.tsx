"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import ConfirmModal from "@/components/common/ConfirmModal";
import EmptyState from "@/components/common/EmptyState";
import Spinner from "@/components/common/Spinner";
import { errorMessage } from "@/lib/api/envelope";
import { listChats, deleteChat, type ChatSummary } from "@/lib/api/chats";

interface Props {
  // Resume the chosen chat; the parent starts a resumed session.
  onResume: (id: string) => void;
  // Close the panel (Escape / outside click / after resume).
  onClose: () => void;
}

type LoadState = "loading" | "ready" | "error";

// relativeTime renders a coarse "N units ago" label for an ISO timestamp.
function relativeTime(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "unknown";
  const secs = Math.max(0, Math.round((Date.now() - then) / 1000));
  if (secs < 60) return "just now";
  const mins = Math.round(secs / 60);
  if (mins < 60) return `${mins} min ago`;
  const hrs = Math.round(mins / 60);
  if (hrs < 24) return `${hrs} hr ago`;
  const days = Math.round(hrs / 24);
  return `${days} day${days === 1 ? "" : "s"} ago`;
}

// ChatHistoryPanel lists saved chats with search, resume, and delete. It is an
// anchored panel on the chat screen: Escape and outside click close it, matching
// the operations panel interaction contract.
export default function ChatHistoryPanel({ onResume, onClose }: Props) {
  const [search, setSearch] = useState("");
  const [chats, setChats] = useState<ChatSummary[]>([]);
  const [state, setState] = useState<LoadState>("loading");
  const [error, setError] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<ChatSummary | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const panelRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);

  const load = useCallback((term: string) => {
    setState("loading");
    listChats(term)
      .then((list) => {
        setChats(list);
        setState("ready");
      })
      .catch((e) => {
        setError(errorMessage(e));
        setState("error");
      });
  }, []);

  // Initial load and focus into the search box.
  useEffect(() => {
    load("");
    searchRef.current?.focus();
  }, [load]);

  // Debounce the search into GET /1.0/chats?search=.
  useEffect(() => {
    const id = window.setTimeout(() => load(search.trim()), 200);
    return () => window.clearTimeout(id);
  }, [search, load]);

  // Escape closes; outside click closes.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape" && !pendingDelete) onClose();
    }
    function onClickOutside(e: MouseEvent) {
      if (panelRef.current && !panelRef.current.contains(e.target as Node)) onClose();
    }
    document.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onClickOutside);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onClickOutside);
    };
  }, [onClose, pendingDelete]);

  const confirmDelete = useCallback(() => {
    if (!pendingDelete) return;
    setDeleteBusy(true);
    setDeleteError(null);
    deleteChat(pendingDelete.id)
      .then(() => {
        setChats((prev) => prev.filter((c) => c.id !== pendingDelete.id));
        setPendingDelete(null);
      })
      .catch((e) => setDeleteError(errorMessage(e)))
      .finally(() => setDeleteBusy(false));
  }, [pendingDelete]);

  return (
    <div
      className="chat-history"
      role="dialog"
      aria-label="Saved chats"
      ref={panelRef}
    >
      <div className="chat-history__header">
        <h2 className="p-heading--5 u-no-margin--bottom">Saved chats</h2>
        <button
          type="button"
          className="p-button--base u-no-margin--bottom chat-history__close"
          aria-label="Close saved chats"
          onClick={onClose}
        >
          <i className="p-icon--close">Close</i>
        </button>
      </div>

      <div className="p-search-box chat-history__search" role="search">
        <input
          ref={searchRef}
          type="search"
          className="p-search-box__input"
          aria-label="Search saved chats"
          placeholder="Search saved chats"
          autoComplete="off"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <button
          type="reset"
          className="p-search-box__reset"
          onClick={() => {
            setSearch("");
            searchRef.current?.focus();
          }}
        >
          <i className="p-icon--close">Clear</i>
        </button>
        <button type="submit" className="p-search-box__button" onClick={(e) => e.preventDefault()}>
          <i className="p-icon--search">Search</i>
        </button>
      </div>

      <div className="chat-history__body" aria-live="polite">
        {state === "loading" && <Spinner label="Loading saved chats…" />}

        {state === "error" && (
          <div className="p-notification--negative" role="alert">
            <div className="p-notification__content">
              <p className="p-notification__message">{error}</p>
            </div>
          </div>
        )}

        {state === "ready" && chats.length === 0 && (
          <EmptyState
            headline={search.trim() ? "No matching chats" : "No saved chats yet"}
            guidance={
              search.trim()
                ? "No saved chat matches your search."
                : "Save the current conversation from the chat box to see it here."
            }
            command={search.trim() ? undefined : "/save [title]"}
          />
        )}

        {state === "ready" && chats.length > 0 && (
          <ul className="chat-history__list u-no-margin--bottom">
            {chats.map((c) => (
              <li key={c.id} className="chat-history__row">
                <button
                  type="button"
                  className="chat-history__resume"
                  onClick={() => onResume(c.id)}
                >
                  <span className="chat-history__title">{c.title}</span>
                  <span className="chat-history__meta u-text--muted p-text--small">
                    {relativeTime(c.updated_at)} · {c.turn_count} turn
                    {c.turn_count === 1 ? "" : "s"}
                    {c.model ? ` · ${c.model}` : ""}
                  </span>
                </button>
                <button
                  type="button"
                  className="p-button--base u-no-margin--bottom chat-history__delete"
                  aria-label={`Delete ${c.title}`}
                  onClick={() => {
                    setDeleteError(null);
                    setPendingDelete(c);
                  }}
                >
                  <i className="p-icon--delete">Delete</i>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>

      {pendingDelete && (
        <ConfirmModal
          title="Delete saved chat"
          confirmLabel="Delete"
          destructive
          busy={deleteBusy}
          onConfirm={confirmDelete}
          onClose={() => {
            if (!deleteBusy) setPendingDelete(null);
          }}
        >
          <p>
            Delete <strong>{pendingDelete.title}</strong>? This cannot be undone.
          </p>
          {deleteError && (
            <div className="p-notification--negative" role="alert">
              <div className="p-notification__content">
                <p className="p-notification__message">{deleteError}</p>
              </div>
            </div>
          )}
        </ConfirmModal>
      )}
    </div>
  );
}
