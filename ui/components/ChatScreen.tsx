"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Header from "@/components/Header";
import { ApiError, errorMessage } from "@/lib/api/envelope";
import { startChat, ChatConnection, type ChatFrame } from "@/lib/api/chat";
import { listKnowledge, type KnowledgeBase } from "@/lib/api/knowledge";

type ConnState = "idle" | "connecting" | "connected" | "closed" | "error";

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
  const [bases, setBases] = useState<KnowledgeBase[]>([]);
  const [activeBases, setActiveBases] = useState<string[]>([]);
  const [model, setModel] = useState<string>("");

  const connRef = useRef<ChatConnection | null>(null);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  // Whether the current assistant turn is still streaming (awaiting `done`).
  const awaitingDone = useRef(false);

  // Load the available knowledge bases for the selector.
  useEffect(() => {
    listKnowledge()
      .then(setBases)
      .catch((e) => {
        // A missing knowledge backend is not fatal to chat; surface softly. An
        // unreachable daemon is, so it gets the standard connection error.
        if (e instanceof ApiError && e.code === 0) setError(errorMessage(e));
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

  // ensureConnection starts a session and opens the websocket if one is not
  // already open, resolving once connected. Multi-turn reuses the open socket;
  // a closed/errored socket triggers a fresh session on the next send.
  const ensureConnection = useCallback(async (): Promise<ChatConnection> => {
    if (connRef.current && connState === "connected") return connRef.current;

    setConnState("connecting");
    setError(null);
    const session = await startChat({ bases: activeBases });
    setModel(session.model);

    return await new Promise<ChatConnection>((resolve, reject) => {
      const conn = new ChatConnection(session.websocketUrl, {
        onFrame: handleFrame,
        onOpen: () => {
          connRef.current = conn;
          setConnState("connected");
          resolve(conn);
        },
        onClose: () => {
          if (connRef.current === conn) {
            connRef.current = null;
            setConnState("closed");
          }
        },
        onError: () => {
          setConnState("error");
          reject(new Error("websocket connection failed"));
        },
      });
    });
  }, [activeBases, connState, handleFrame]);

  const send = useCallback(async () => {
    const text = input.trim();
    if (!text || awaitingDone.current) return;
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
  }, [input, ensureConnection]);

  // Toggle a knowledge base in the active selection and, if a session is live,
  // push the change over the open socket mid-session.
  const toggleBase = useCallback((name: string) => {
    setActiveBases((prev) => {
      const next = prev.includes(name) ? prev.filter((b) => b !== name) : [...prev, name];
      if (connRef.current && connState === "connected") connRef.current.setActiveBases(next);
      return next;
    });
  }, [connState]);

  // Auto-scroll to the latest message.
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  // Close the socket on unmount.
  useEffect(() => () => connRef.current?.close(), []);

  function onKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      void send();
    }
  }

  return (
    <>
      <Header title="Chat">
        <div className="chat__status">
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
        {bases.length > 0 && (
          <div className="kb-selector">
            <span className="kb-selector__label">Knowledge bases:</span>
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

        <div className="chat__composer">
          <textarea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={onKeyDown}
            placeholder="Ask a question… (Enter to send, Shift+Enter for newline)"
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
      </main>
    </>
  );
}
