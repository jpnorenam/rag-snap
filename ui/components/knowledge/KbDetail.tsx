"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import type { Notice } from "@/components/KnowledgeScreen";
import IngestModal from "@/components/knowledge/IngestModal";
import BatchIngestModal from "@/components/knowledge/BatchIngestModal";
import MetadataModal from "@/components/knowledge/MetadataModal";
import ConfirmModal from "@/components/common/ConfirmModal";
import EmptyState from "@/components/common/EmptyState";
import Spinner from "@/components/common/Spinner";
import { errorMessage } from "@/lib/api/envelope";
import { absoluteTime, relativeTime } from "@/lib/relativeTime";
import { statusOf, type OperationView } from "@/lib/api/operations";
import {
  downloadExportArchive,
  exportKnowledge,
  forgetSource,
  getKnowledge,
  listSources,
  type KnowledgeBaseDetail,
  type SourceMetadata,
} from "@/lib/api/knowledge";
import { useOperations } from "@/lib/useOperations";
import { useCompletedOps } from "@/lib/useCompletedOps";

interface Props {
  name: string;
  notify: (n: Notice) => void;
}

// sourceType infers a coarse type label for a source from its stored path.
function sourceType(src: SourceMetadata): string {
  const path = src.file_path ?? "";
  if (/^https?:\/\//.test(path)) return "url";
  return "file";
}

// KbDetail shows one knowledge base's ingested sources with ingest, batch,
// export, metadata, and forget actions.
export default function KbDetail({ name, notify }: Props) {
  const { operations, track } = useOperations();
  const [detail, setDetail] = useState<KnowledgeBaseDetail | null>(null);
  const [sources, setSources] = useState<SourceMetadata[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [showIngest, setShowIngest] = useState(false);
  const [showBatch, setShowBatch] = useState(false);
  const [metaTarget, setMetaTarget] = useState<SourceMetadata | null>(null);
  const [forgetTarget, setForgetTarget] = useState<SourceMetadata | null>(null);
  const [forgetting, setForgetting] = useState(false);

  const exportOps = useRef<Set<string>>(new Set());

  const load = useCallback(async () => {
    setLoadError(null);
    try {
      const [d, s] = await Promise.all([getKnowledge(name), listSources(name)]);
      setDetail(d);
      setSources(s);
    } catch (e) {
      setSources(null);
      setLoadError(errorMessage(e));
    }
  }, [name]);

  useEffect(() => {
    void load();
  }, [load]);

  // Running ingest operations targeting this base drive the in-progress hint.
  const resourcePath = `/1.0/knowledge/${name}`;
  const runningIngests = operations.filter(
    (op) =>
      statusOf(op) === "running" &&
      op.description.startsWith("Ingesting") &&
      (op.resources.knowledge ?? []).includes(resourcePath)
  ).length;

  const onComplete = useCallback(
    (op: OperationView) => {
      const succeeded = statusOf(op) === "succeeded";
      if (exportOps.current.has(op.id)) {
        exportOps.current.delete(op.id);
        if (!succeeded) {
          notify({ kind: "negative", message: op.err || "Export failed." });
          return;
        }
        void downloadExportArchive(name, op.id, `${name}.tar.gz`)
          .then(() => notify({ kind: "positive", message: `Export ready — downloading ${name}.tar.gz.` }))
          .catch((e) => notify({ kind: "negative", message: errorMessage(e) }));
        return;
      }
      // Ingest/batch operations targeting this base: refresh the sources list.
      if (op.description.startsWith("Ingesting") && (op.resources.knowledge ?? []).includes(resourcePath)) {
        if (succeeded) {
          notify({ kind: "positive", message: "Ingestion complete." });
        } else {
          notify({ kind: "negative", message: op.err || "Ingestion failed." });
        }
        void load();
      }
    },
    [name, notify, load, resourcePath]
  );

  useCompletedOps(onComplete);

  const onExport = async () => {
    try {
      const op = await exportKnowledge(name);
      exportOps.current.add(op.id);
      track(op);
      notify({ kind: "positive", message: `Exporting “${name}” — the download starts when it completes.` });
    } catch (e) {
      notify({ kind: "negative", message: errorMessage(e) });
    }
  };

  const onForget = async () => {
    if (!forgetTarget) return;
    setForgetting(true);
    try {
      await forgetSource(name, forgetTarget.source_id);
      // Remove the row optimistically: the metadata index is search-eventually-
      // consistent, so an immediate re-fetch would still return the just-forgotten
      // source. Keep local state authoritative for the removal.
      const forgottenId = forgetTarget.source_id;
      setSources((list) => (list ? list.filter((s) => s.source_id !== forgottenId) : list));
      setDetail((d) => (d ? { ...d, source_count: Math.max(0, d.source_count - 1) } : d));
      notify({ kind: "positive", message: `Forgot source “${forgottenId}”.` });
      setForgetTarget(null);
    } catch (e) {
      notify({ kind: "negative", message: errorMessage(e) });
    } finally {
      setForgetting(false);
    }
  };

  const count = detail?.source_count ?? sources?.length ?? 0;

  return (
    <>
      <div className="kb-detail__breadcrumb">
        <Link className="p-button--base u-no-margin--bottom" href="/knowledge/">
          ← Knowledge bases
        </Link>
      </div>

      <div className="kb__header">
        <div>
          <h2 className="u-no-margin--bottom">{name}</h2>
          <p className="u-text--muted p-text--small">
            {count} source{count === 1 ? "" : "s"} · <code>rag-cli.rag k ingest {name} &lt;id&gt; --file &lt;path&gt;</code>
          </p>
        </div>
        <div className="kb__actions">
          <button
            type="button"
            className="p-button--positive u-no-margin--bottom"
            onClick={() => setShowIngest(true)}
          >
            Ingest document
          </button>
          <button type="button" className="p-button u-no-margin--bottom" onClick={() => setShowBatch(true)}>
            Batch ingest
          </button>
          <button type="button" className="p-button u-no-margin--bottom" onClick={() => void onExport()}>
            Export
          </button>
        </div>
      </div>

      {loadError && (
        <div className="p-notification--negative" role="alert">
          <div className="p-notification__content">
            <p className="p-notification__message">{loadError}</p>
            <button type="button" className="p-button u-no-margin--bottom" onClick={() => void load()}>
              Retry
            </button>
          </div>
        </div>
      )}

      {!sources && !loadError && <Spinner label="Loading sources…" />}

      {runningIngests > 0 && (
        <p className="kb-detail__progress u-text--muted p-text--small" aria-live="polite">
          <i className="p-icon--spinner u-animation--spin" aria-hidden="true" /> {runningIngests} ingest
          {runningIngests === 1 ? "" : "s"} in progress…
        </p>
      )}

      {sources && sources.length === 0 && !loadError && (
        <EmptyState
          headline="No sources ingested yet."
          guidance="Ingest a document by upload or URL to start building this base."
          command={`rag-cli.rag k ingest ${name} <id> --file <path>`}
          action={
            <button
              type="button"
              className="p-button--positive u-no-margin--bottom"
              onClick={() => setShowIngest(true)}
            >
              Ingest document
            </button>
          }
        />
      )}

      {sources && sources.length > 0 && (
        <div className="kb__table-wrap">
          <table aria-label={`Sources in ${name}`}>
            <thead>
              <tr>
                <th>Source ID</th>
                <th>Title / filename</th>
                <th>Type</th>
                <th>Ingested</th>
                <th className="u-align--right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {sources.map((src) => (
                <tr key={src.source_id}>
                  <td>{src.source_id}</td>
                  <td>{src.title || src.file_name || "—"}</td>
                  <td>{sourceType(src)}</td>
                  <td title={src.ingested_at ? absoluteTime(src.ingested_at) : undefined}>
                    {src.ingested_at ? relativeTime(src.ingested_at) : "—"}
                  </td>
                  <td className="u-align--right">
                    <div className="kb-actions-stack">
                      <button
                        type="button"
                        className="p-button--base kb-action u-no-margin--bottom"
                        onClick={() => setMetaTarget(src)}
                      >
                        Metadata
                      </button>
                      <button
                        type="button"
                        className="p-button--base kb-action u-no-margin--bottom"
                        onClick={() => setForgetTarget(src)}
                      >
                        Forget
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {showIngest && (
        <IngestModal
          name={name}
          onStarted={(op) => {
            setShowIngest(false);
            track(op);
            notify({ kind: "positive", message: "Ingestion started…" });
          }}
          onClose={() => setShowIngest(false)}
        />
      )}

      {showBatch && (
        <BatchIngestModal
          name={name}
          onStarted={(op) => {
            setShowBatch(false);
            track(op);
            notify({ kind: "positive", message: "Batch ingestion started…" });
          }}
          onClose={() => setShowBatch(false)}
        />
      )}

      {metaTarget && <MetadataModal source={metaTarget} onClose={() => setMetaTarget(null)} />}

      {forgetTarget && (
        <ConfirmModal
          title="Forget source"
          confirmLabel="Forget"
          destructive
          busy={forgetting}
          onConfirm={() => void onForget()}
          onClose={() => setForgetTarget(null)}
        >
          <p>
            Removes all chunks and the metadata record for{" "}
            <strong>{forgetTarget.source_id}</strong> from <strong>{name}</strong>.
          </p>
        </ConfirmModal>
      )}
    </>
  );
}
