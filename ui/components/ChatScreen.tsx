"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Header from "@/components/Header";
import ChatHistoryPanel from "@/components/ChatHistoryPanel";
import { ApiError, errorMessage } from "@/lib/api/envelope";
import { startChat, ChatConnection, type ChatFrame, type ChatStartOptions } from "@/lib/api/chat";
import { listKnowledge, type KnowledgeBase } from "@/lib/api/knowledge";

type ConnState = "idle" | "connecting" | "connected" | "closed" | "error";

// The slash commands the composer recognizes, mirroring the REPL registry.
const SLASH_COMMANDS: { name: string; syntax: string }[] = [
  { name: "/save", syntax: "[title]" },
  { name: "/history", syntax: "" },
];

// A transient banner shown for save results and command feedback (distinct from
// the fatal `error` connection banner).
interface Notice {
  type: "positive" | "information";
  message: string;
}

// A turn in the transcript. Assistant turns accumulate streamed token/think
// content incrementally as frames arrive.
interface Message {
  role: "user" | "assistant";
  content: string;
  think: string;
  streaming?: boolean;
}

export default function ChatScreen() {
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState("");
  const [connState, setConnState] = useState<ConnState>("idle");
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [bases, setBases] = useState<KnowledgeBase[]>([]);
  const [kbState, setKbState] = useState<"loading" | "connected" | "unavailable">("loading");
  const [activeBases, setActiveBases] = useState<string[]>([]);
  const [model, setModel] = useState<string>("");
  const [historyOpen, setHistoryOpen] = useState(false);
  // Highlighted index in the composer's slash-command hint list (-1 = none).
  const [hintIndex, setHintIndex] = useState(-1);

  const connRef = useRef<ChatConnection | null>(null);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  // Whether the current assistant turn is still streaming (awaiting `done`).
  const awaitingDone = useRef(false);

  // Matching slash commands while the user is still typing the command (before a
  // space starts the arguments), for the composer hint list.
  const hintMatches =
    input.startsWith("/") && !input.includes(" ")
      ? SLASH_COMMANDS.filter((c) => c.name.startsWith(input))
      : [];

  // Load the available knowledge bases for the selector. A successful call means
  // OpenSearch is reachable; a non-zero-code error means the daemon is up but the
  // knowledge store is not, which we surface as an "Unavailable" indicator rather
  // than a fatal error.
  useEffect(() => {
    listKnowledge()
      .then((b) => {
        setBases(b);
        setKbState("connected");
      })
      .catch((e) => {
        // An unreachable daemon is fatal, so it gets the standard connection
        // error. An unreachable knowledge store is not — chat still works.
        if (e instanceof ApiError && e.code === 0) setError(errorMessage(e));
        setKbState("unavailable");
      });
  }, []);

  // Append streamed content to the in-flight assistant turn.
  const appendToAssistant = useCallback((field: "content" | "think", chunk: string) => {
    setMessages((prev) => {
      const next = [...prev];
      const last = next[next.length - 1];
      if (last && last.role === "assistant" && last.streaming) {
        next[next.length - 1] = { ...last, [field]: last[field] + chunk };
      }
      return next;
    });
  }, []);

  const handleFrame = useCallback(
    (frame: ChatFrame) => {
      switch (frame.type) {
        case "token":
          appendToAssistant("content", frame.content ?? "");
          break;
        case "think":
          appendToAssistant("think", frame.content ?? "");
          break;
        case "done":
          awaitingDone.current = false;
          setMessages((prev) => {
            const next = [...prev];
            const last = next[next.length - 1];
            if (last && last.role === "assistant") next[next.length - 1] = { ...last, streaming: false };
            return next;
          });
          break;
        case "active-kbs":
          setActiveBases(frame.bases ?? []);
          break;
        case "saved":
          setNotice({ type: "positive", message: `Saved chat as “${frame.title ?? "Untitled chat"}”.` });
          break;
        case "error":
          setError(frame.error ?? "chat error");
          awaitingDone.current = false;
          setMessages((prev) => {
            const next = [...prev];
            const last = next[next.length - 1];
            if (last && last.role === "assistant" && last.streaming)
              next[next.length - 1] = { ...last, streaming: false };
            return next;
          });
          break;
      }
    },
    [appendToAssistant]
  );

  // openConnection starts a session (fresh or resumed) and opens its websocket,
  // resolving once connected. Callers own connRef and the connState transitions.
  const openConnection = useCallback(
    async (opts: ChatStartOptions) => {
      const session = await startChat(opts);
      const conn = await new Promise<ChatConnection>((resolve, reject) => {
        const c = new ChatConnection(session.websocketUrl, {
          onFrame: handleFrame,
          onOpen: () => resolve(c),
          onClose: () => {
            if (connRef.current === c) {
              connRef.current = null;
              setConnState("closed");
            }
          },
          onError: () => reject(new Error("websocket connection failed")),
        });
      });
      return { conn, session };
    },
    [handleFrame]
  );

  // ensureConnection opens a session if one is not already connected. Multi-turn
  // reuses the open socket; a closed/errored socket triggers a fresh session.
  const ensureConnection = useCallback(async (): Promise<ChatConnection> => {
    if (connRef.current && connState === "connected") return connRef.current;

    setConnState("connecting");
    setError(null);
    const { conn, session } = await openConnection({ bases: activeBases });
    connRef.current = conn;
    setModel(session.model);
    setConnState("connected");
    return conn;
  }, [activeBases, connState, openConnection]);

  // resumeChat starts a new session seeded from a saved chat, replacing the
  // transcript and active-base selection with the restored state.
  const resumeChat = useCallback(
    async (id: string) => {
      setHistoryOpen(false);
      connRef.current?.close();
      connRef.current = null;
      awaitingDone.current = false;
      setConnState("connecting");
      setError(null);
      try {
        const { conn, session } = await openConnection({ resume: id });
        connRef.current = conn;
        setModel(session.model);
        if (session.restored) {
          setMessages(
            session.restored.turns.map((t) => ({ role: t.role, content: t.content, think: "" }))
          );
          setActiveBases(session.restored.bases ?? []);
          const dropped = session.restored.dropped_bases ?? [];
          if (dropped.length > 0) {
            setNotice({
              type: "information",
              message: `Some saved knowledge bases no longer exist and were skipped: ${dropped.join(", ")}.`,
            });
          }
        }
        setConnState("connected");
      } catch (e) {
        setError(errorMessage(e));
        setConnState("error");
      }
    },
    [openConnection]
  );

  // handleSlashInput routes a slash command entered in the composer. `/save`
  // persists over the open socket; `/history` opens the panel; anything else
  // reports the available commands without contacting the daemon.
  const handleSlashInput = useCallback(
    (text: string) => {
      const [verb, ...rest] = text.split(/\s+/);
      switch (verb) {
        case "/save":
          if (connRef.current && connState === "connected") {
            connRef.current.save(rest.join(" "));
          } else {
            setNotice({
              type: "information",
              message: "There's nothing to save yet — ask a question first.",
            });
          }
          break;
        case "/history":
          setHistoryOpen(true);
          break;
        default:
          setNotice({
            type: "information",
            message: `Unknown command ${verb}. Available: ${SLASH_COMMANDS.map((c) => c.name).join(", ")}.`,
          });
      }
    },
    [connState]
  );

  const send = useCallback(async () => {
    const text = input.trim();
    if (!text || awaitingDone.current) return;

    // A slash command is handled locally and never sent as a prompt.
    if (text.startsWith("/")) {
      setInput("");
      setHintIndex(-1);
      handleSlashInput(text);
      return;
    }

    setInput("");
    setMessages((prev) => [
      ...prev,
      { role: "user", content: text, think: "" },
      { role: "assistant", content: "", think: "", streaming: true },
    ]);
    awaitingDone.current = true;
    try {
      const conn = await ensureConnection();
      conn.prompt(text);
    } catch (e) {
      awaitingDone.current = false;
      setError(errorMessage(e));
      setMessages((prev) => {
        const next = [...prev];
        const last = next[next.length - 1];
        if (last && last.role === "assistant") next[next.length - 1] = { ...last, streaming: false };
        return next;
      });
    }
  }, [input, ensureConnection, handleSlashInput]);

  // acceptHint fills the composer with the chosen command name, ready for args.
  const acceptHint = useCallback((name: string) => {
    setInput(name + " ");
    setHintIndex(-1);
  }, []);

  // Toggle a knowledge base in the active selection and, if a session is live,
  // push the change over the open socket mid-session.
  const toggleBase = useCallback(
    (name: string) => {
      setActiveBases((prev) => {
        const next = prev.includes(name) ? prev.filter((b) => b !== name) : [...prev, name];
        if (connRef.current && connState === "connected") connRef.current.setActiveBases(next);
        return next;
      });
    },
    [connState]
  );

  // Auto-scroll to the latest message.
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  // Close the socket on unmount.
  useEffect(() => () => connRef.current?.close(), []);

  function onKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (hintMatches.length > 0) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setHintIndex((i) => (i + 1) % hintMatches.length);
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setHintIndex((i) => (i <= 0 ? hintMatches.length - 1 : i - 1));
        return;
      }
      if (e.key === "Tab") {
        e.preventDefault();
        acceptHint(hintMatches[hintIndex < 0 ? 0 : hintIndex].name);
        return;
      }
      if (e.key === "Enter" && !e.shiftKey && hintIndex >= 0) {
        // A highlighted hint is accepted rather than submitted.
        e.preventDefault();
        acceptHint(hintMatches[hintIndex].name);
        return;
      }
    }
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      void send();
    }
  }

  return (
    <>
      <Header title="Chat">
        <div className="chat__status">
          <button
            type="button"
            className="p-button--base u-no-margin--bottom chat__history-toggle"
            onClick={() => setHistoryOpen(true)}
          >
            Saved chats
          </button>
          <span
            className={`app-status-dot ${
              connState === "connected" ? "is-connected" : connState === "error" ? "is-error" : ""
            }`}
          />
          <span className="u-text--muted p-text--small u-no-margin--bottom">
            {connState === "connected"
              ? model
                ? `Connected · ${model}`
                : "Connected"
              : connState === "connecting"
                ? "Connecting…"
                : connState === "error"
                  ? "Connection error"
                  : "Ready"}
          </span>
        </div>
      </Header>

      <main className="app-main chat">
        <div className="kb-selector">
          <span className="kb-selector__label">
            <span
              className={`app-status-dot ${
                kbState === "connected" ? "is-connected" : kbState === "unavailable" ? "is-error" : ""
              }`}
            />
            {kbState === "connected"
              ? "Connected · Knowledge bases:"
              : kbState === "unavailable"
                ? "Unavailable · Knowledge bases:"
                : "Knowledge bases:"}
          </span>
          {kbState === "connected" && bases.length === 0 && (
            <span className="u-text--muted p-text--small u-no-margin--bottom">
              None yet — create one with <code>rag-cli.rag k create &lt;name&gt;</code>
            </span>
          )}
          {kbState === "unavailable" && (
            <span className="u-text--muted p-text--small u-no-margin--bottom">
              OpenSearch is not reachable. Check that the knowledge store is running.
            </span>
          )}
          {bases.map((b) => (
            <button
              key={b.name}
              onClick={() => toggleBase(b.name)}
              className={`p-chip u-no-margin--bottom ${
                activeBases.includes(b.name) ? "p-chip--positive" : ""
              }`}
            >
              <span className="p-chip__value">{b.name}</span>
            </button>
          ))}
        </div>

        {notice && (
          <div className={`p-notification--${notice.type}`} role="status">
            <div className="p-notification__content">
              <p className="p-notification__message">{notice.message}</p>
              <button type="button" className="p-button u-no-margin--bottom" onClick={() => setNotice(null)}>
                Dismiss
              </button>
            </div>
          </div>
        )}

        {error && (
          <div className="p-notification--negative" role="alert">
            <div className="p-notification__content">
              <p className="p-notification__message">{error}</p>
            </div>
          </div>
        )}

        <div className="chat__messages">
          {messages.length === 0 && (
            <p className="u-text--muted">
              Ask a question to start chatting with your knowledge bases.
            </p>
          )}
          {messages.map((m, i) => (
            <div key={i} className={`chat-message chat-message--${m.role}`}>
              <span className="chat-message__role">{m.role === "user" ? "You" : "Assistant"}</span>
              {m.think && <div className="chat-message__think">{m.think}</div>}
              <div className="chat-message__bubble">
                {m.content || (m.streaming ? "…" : "")}
              </div>
            </div>
          ))}
          <div ref={messagesEndRef} />
        </div>

        <div className="chat__composer-wrap">
          {hintMatches.length > 0 && (
            <ul className="chat-hints" role="listbox" aria-label="Slash commands">
              {hintMatches.map((c, i) => (
                <li
                  key={c.name}
                  role="option"
                  aria-selected={i === hintIndex}
                  className={`chat-hints__item ${i === hintIndex ? "is-active" : ""}`}
                  onMouseDown={(e) => {
                    // Keep textarea focus; fill the command on click.
                    e.preventDefault();
                    acceptHint(c.name);
                  }}
                >
                  <span className="chat-hints__name">{c.name}</span>
                  {c.syntax && <span className="chat-hints__syntax u-text--muted">{c.syntax}</span>}
                </li>
              ))}
            </ul>
          )}
          <div className="chat__composer">
            <textarea
              value={input}
              onChange={(e) => {
                setInput(e.target.value);
                setHintIndex(-1);
              }}
              onKeyDown={onKeyDown}
              placeholder="Ask a question, or type / for commands (Enter to send, Shift+Enter for newline)"
              rows={2}
              aria-label="Prompt"
            />
            <button
              className="p-button--positive u-no-margin--bottom"
              onClick={() => void send()}
              disabled={awaitingDone.current || !input.trim()}
            >
              Send
            </button>
          </div>
        </div>
      </main>

      {historyOpen && (
        <ChatHistoryPanel onResume={resumeChat} onClose={() => setHistoryOpen(false)} />
      )}
    </>
  );
}
