"use client";

import { useCallback, useEffect, useState } from "react";
import ConfigTable from "@/components/ConfigTable";
import Header from "@/components/Header";
import ServiceCard from "@/components/ServiceCard";
import ConfirmModal from "@/components/common/ConfirmModal";
import Spinner from "@/components/common/Spinner";
import { getConfig, revertConfig, setConfig, type ConfigEntry, type ConfigList } from "@/lib/api/config";
import { errorMessage } from "@/lib/api/envelope";
import { getStatus, type ServiceName, type StatusPayload } from "@/lib/api/status";

// SERVICES fixes the order and copy of the service cards (docs/ux/06). The daemon
// returns an object, so the order is the page's decision, not the payload's.
const SERVICES: {
  name: ServiceName;
  title: string;
  role: string;
  hint: string;
}[] = [
  {
    name: "opensearch",
    title: "OpenSearch",
    role: "Knowledge store: embeddings, indexes, and search.",
    hint: "Check the service is running (`snap services rag-cli`) and that `knowledge.http.host` and `knowledge.http.port` are right.",
  },
  {
    name: "inference",
    title: "Inference server",
    role: "Chat backend: generates answers.",
    hint: "Check the inference server is running and that `chat.http.host` and `chat.http.port` are right.",
  },
  {
    name: "tika",
    title: "Tika",
    role: "Text extraction from ingested documents.",
    hint: "Check the service is running (`snap services rag-cli.tika`) and that `tika.http.host` and `tika.http.port` are right.",
  },
  {
    name: "ragd",
    title: "ragd",
    role: "The local daemon serving this page and the API.",
    hint: "Check the daemon is running (`snap services rag-cli.ragd`).",
  },
];

// CONNECTION_KEY_PATTERN matches the config keys that feed a service connection.
// Changing one of these may need the service to reconnect, which the user cannot
// tell from the row alone — so the save confirmation points back at the Status zone.
const CONNECTION_KEY_PATTERN = /^(chat|knowledge|tika)\.http\./;

// GENERATION_KEYS are the keys the daemon resolves when a chat session or batch
// run *starts*. Work already in flight keeps the value it began with, so the save
// message says when the new value applies — the same promise the Prompts page makes.
const GENERATION_KEYS = ["chat.model"];

// saveNotice states what a saved key actually changes. A bare "Saved." leaves the
// user to guess whether a running chat just changed model underneath them.
function saveNotice(key: string): Notice {
  if (CONNECTION_KEY_PATTERN.test(key)) {
    return {
      kind: "caution",
      message: `Saved ${key}. This changes a service connection — check Status above.`,
    };
  }
  if (GENERATION_KEYS.includes(key)) {
    return {
      kind: "positive",
      message: `Saved ${key}. New chats and batch runs will use it.`,
    };
  }
  return { kind: "positive", message: `Saved ${key}.` };
}

// Notice is the transient banner shown after a mutation.
interface Notice {
  kind: "positive" | "negative" | "caution";
  message: string;
}

export default function StatusScreen() {
  const [status, setStatus] = useState<StatusPayload | null>(null);
  const [statusError, setStatusError] = useState<string | null>(null);
  const [checkedAt, setCheckedAt] = useState<Date | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  const [config, setConfigList] = useState<ConfigList | null>(null);
  const [configError, setConfigError] = useState<string | null>(null);
  const [filter, setFilter] = useState("");
  const [saving, setSaving] = useState<string | null>(null);
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({});
  const [notice, setNotice] = useState<Notice | null>(null);
  const [reverting, setReverting] = useState<ConfigEntry | null>(null);

  // The two zones load and fail independently: an unreachable backend must not
  // take the configuration table down with it, and vice versa.
  const loadStatus = useCallback(async () => {
    setRefreshing(true);
    setStatusError(null);
    try {
      setStatus(await getStatus());
      setCheckedAt(new Date());
    } catch (e) {
      setStatus(null);
      setStatusError(errorMessage(e));
    } finally {
      setRefreshing(false);
    }
  }, []);

  const loadConfig = useCallback(async () => {
    setConfigError(null);
    try {
      setConfigList(await getConfig());
    } catch (e) {
      setConfigList(null);
      setConfigError(errorMessage(e));
    }
  }, []);

  useEffect(() => {
    void loadStatus();
    void loadConfig();
  }, [loadStatus, loadConfig]);

  const onSave = async (key: string, value: string) => {
    setSaving(key);
    setNotice(null);
    setRowErrors((errs) => {
      const next = { ...errs };
      delete next[key];
      return next;
    });

    try {
      const updated = await setConfig(key, value);
      setConfigList((list) =>
        list
          ? { ...list, keys: list.keys.map((e) => (e.key === updated.key ? updated : e)) }
          : list
      );
      setNotice(saveNotice(key));
    } catch (e) {
      // The error stays on the row and the draft is preserved: a rejected save
      // must never cost the user their input.
      setRowErrors((errs) => ({ ...errs, [key]: errorMessage(e) }));
    } finally {
      setSaving(null);
    }
  };

  const onRevert = async (entry: ConfigEntry) => {
    setSaving(entry.key);
    setNotice(null);
    try {
      const updated = await revertConfig(entry.key);
      setConfigList((list) =>
        list
          ? { ...list, keys: list.keys.map((e) => (e.key === updated.key ? updated : e)) }
          : list
      );
      setNotice({ kind: "positive", message: `Reverted ${entry.key} to its package value.` });
    } catch (e) {
      setNotice({ kind: "negative", message: errorMessage(e) });
    } finally {
      setSaving(null);
      setReverting(null);
    }
  };

  const visible =
    config?.keys.filter((e) => e.key.toLowerCase().includes(filter.trim().toLowerCase())) ?? [];

  return (
    <>
      <Header title="Status" />
      <main className="app-main status">
        {/* --- Status zone --- */}
        <section className="status__zone" aria-labelledby="status-services">
          <div className="status__zone-head">
            <h2 id="status-services" className="status__zone-title">
              Services
            </h2>
            <div className="status__refresh">
              {checkedAt && (
                <span className="p-text--small u-text--muted" title={checkedAt.toLocaleString()}>
                  Checked {relativeTime(checkedAt)}
                </span>
              )}
              <button
                type="button"
                className="p-button--base u-no-margin--bottom"
                disabled={refreshing}
                onClick={() => void loadStatus()}
              >
                {refreshing ? "Refreshing…" : "Refresh"}
              </button>
            </div>
          </div>

          {/* The announcement of a completed refresh, for assistive tech. */}
          <div className="u-off-screen" aria-live="polite">
            {status && !refreshing ? "Status updated" : ""}
          </div>

          {statusError && (
            <div className="p-notification--negative" role="alert">
              <div className="p-notification__content">
                <p className="p-notification__message">{statusError}</p>
                <button
                  type="button"
                  className="p-button u-no-margin--bottom"
                  onClick={() => void loadStatus()}
                >
                  Retry
                </button>
              </div>
            </div>
          )}

          {!status && !statusError && <Spinner label="Checking services…" />}

          {status && (
            <ul className="status__services">
              {SERVICES.map((service) => (
                <ServiceCard
                  key={service.name}
                  name={service.name}
                  title={service.title}
                  role={service.role}
                  hint={service.hint}
                  status={status[service.name] ?? { state: "not configured" }}
                />
              ))}
            </ul>
          )}
        </section>

        {/* --- Configuration zone --- */}
        <section className="status__zone" aria-labelledby="status-config">
          <h2 id="status-config" className="status__zone-title">
            Configuration
          </h2>

          {notice && (
            <div
              className={`p-notification--${notice.kind}`}
              role={notice.kind === "negative" ? "alert" : "status"}
            >
              <div className="p-notification__content">
                <p className="p-notification__message">{notice.message}</p>
              </div>
            </div>
          )}

          {config && !config.writable && (
            <div className="p-notification--information">
              <div className="p-notification__content">
                <p className="p-notification__message">
                  Configuration is read-only for this session. Edit it from the CLI:{" "}
                  <code>sudo rag-cli.rag set &lt;key&gt;=&lt;value&gt;</code>
                </p>
              </div>
            </div>
          )}

          {configError && (
            <div className="p-notification--negative" role="alert">
              <div className="p-notification__content">
                <p className="p-notification__message">{configError}</p>
                <p className="p-notification__message p-text--small">
                  You can still read the configuration with <code>rag-cli.rag get</code>.
                </p>
                <button
                  type="button"
                  className="p-button u-no-margin--bottom"
                  onClick={() => void loadConfig()}
                >
                  Retry
                </button>
              </div>
            </div>
          )}

          {!config && !configError && <Spinner label="Loading configuration…" />}

          {config && (
            <>
              <div className="p-search-box status__filter">
                <label className="u-off-screen" htmlFor="config-filter">
                  Filter configuration keys
                </label>
                <input
                  type="search"
                  id="config-filter"
                  className="p-search-box__input"
                  placeholder="Filter keys"
                  value={filter}
                  onChange={(e) => setFilter(e.target.value)}
                />
                <button type="reset" className="p-search-box__reset" onClick={() => setFilter("")}>
                  <i className="p-icon--close">Clear</i>
                </button>
                <button type="submit" className="p-search-box__button" onClick={(e) => e.preventDefault()}>
                  <i className="p-icon--search">Search</i>
                </button>
              </div>

              {config.keys.length === 0 ? (
                // Empty is not an error: a store with no keys is a broken install,
                // not a filter miss, and it gets the CLI command to check with.
                <p className="u-text--muted">
                  No configuration is set. Check the install with{" "}
                  <code>rag-cli.rag get</code>.
                </p>
              ) : visible.length === 0 ? (
                <p className="u-text--muted">No configuration keys match “{filter}”.</p>
              ) : (
                <ConfigTable
                  entries={visible}
                  writable={config.writable}
                  errors={rowErrors}
                  saving={saving}
                  onSave={(key, value) => void onSave(key, value)}
                  onRevert={setReverting}
                />
              )}
            </>
          )}
        </section>
      </main>

      {reverting && (
        <ConfirmModal
          title="Revert to package value"
          confirmLabel="Revert"
          destructive
          busy={saving === reverting.key}
          onConfirm={() => void onRevert(reverting)}
          onClose={() => setReverting(null)}
        >
          <p>
            Drops your value for <code>{reverting.key}</code> so the value shipped with the snap
            applies again.
          </p>
          <p className="p-text--small">
            <span className="u-text--muted">Your value: </span>
            <code>{reverting.value || "—"}</code>
          </p>
          <p className="p-text--small">
            <span className="u-text--muted">Package value: </span>
            <code>{reverting.package_value || "—"}</code>
          </p>
        </ConfirmModal>
      )}
    </>
  );
}

// relativeTime renders how long ago a check ran. The absolute time stays in the
// element's title (microcopy: relative in lists, absolute on hover).
function relativeTime(at: Date): string {
  const seconds = Math.round((Date.now() - at.getTime()) / 1000);
  if (seconds < 60) return "just now";
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes} minute${minutes === 1 ? "" : "s"} ago`;
  const hours = Math.round(minutes / 60);
  return `${hours} hour${hours === 1 ? "" : "s"} ago`;
}
