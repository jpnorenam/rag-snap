"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import type { Notice } from "@/components/KnowledgeScreen";
import CreateKbModal from "@/components/knowledge/CreateKbModal";
import ImportModal from "@/components/knowledge/ImportModal";
import ConfirmModal from "@/components/common/ConfirmModal";
import EmptyState from "@/components/common/EmptyState";
import Spinner from "@/components/common/Spinner";
import { errorMessage } from "@/lib/api/envelope";
import { statusOf, type OperationView } from "@/lib/api/operations";
import {
  deleteKnowledge,
  downloadExportArchive,
  exportKnowledge,
  listKnowledge,
  type KnowledgeBase,
} from "@/lib/api/knowledge";
import { useOperations } from "@/lib/useOperations";
import { useCompletedOps } from "@/lib/useCompletedOps";

interface Props {
  notify: (n: Notice) => void;
  onError: (e: unknown) => void;
}

// KbList is the knowledge-bases list: a table of bases with source counts and
// per-row open/export/delete actions, plus create and import. Export and import
// run as tracked operations.
export default function KbList({ notify, onError }: Props) {
  const { track } = useOperations();
  const [bases, setBases] = useState<KnowledgeBase[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [showCreate, setShowCreate] = useState(false);
  const [showImport, setShowImport] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<KnowledgeBase | null>(null);
  const [deleting, setDeleting] = useState(false);

  // Track which operation id belongs to which flow, so completion can download
  // (export) or refresh (import) appropriately.
  const opKinds = useRef<Record<string, { kind: "export" | "import"; name: string }>>({});

  const load = useCallback(async () => {
    setLoadError(null);
    try {
      setBases(await listKnowledge());
    } catch (e) {
      setBases(null);
      setLoadError(errorMessage(e));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const onComplete = useCallback(
    (op: OperationView) => {
      const entry = opKinds.current[op.id];
      if (!entry) return;
      delete opKinds.current[op.id];
      if (statusOf(op) !== "succeeded") {
        notify({ kind: "negative", message: op.err || `${entry.kind} failed.` });
        return;
      }
      if (entry.kind === "export") {
        void downloadExportArchive(entry.name, op.id, `${entry.name}.tar.gz`)
          .then(() => notify({ kind: "positive", message: `Export ready — downloading ${entry.name}.tar.gz.` }))
          .catch((e) => notify({ kind: "negative", message: errorMessage(e) }));
      } else {
        notify({ kind: "positive", message: "Knowledge base imported." });
        void load();
      }
    },
    [notify, load]
  );

  useCompletedOps(onComplete);

  const onExport = async (name: string) => {
    try {
      const op = await exportKnowledge(name);
      opKinds.current[op.id] = { kind: "export", name };
      track(op);
      notify({ kind: "positive", message: `Exporting “${name}” — the download starts when it completes.` });
    } catch (e) {
      onError(e);
    }
  };

  const onDelete = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await deleteKnowledge(deleteTarget.name);
      notify({ kind: "positive", message: `Knowledge base “${deleteTarget.name}” deleted.` });
      setDeleteTarget(null);
      await load();
    } catch (e) {
      notify({ kind: "negative", message: errorMessage(e) });
    } finally {
      setDeleting(false);
    }
  };

  return (
    <>
      <div className="kb__header">
        <p className="kb__intro u-text--muted">
          Build local knowledge bases and chat with them. Create one here or with{" "}
          <code>rag-cli.rag k create &lt;name&gt;</code>.
        </p>
        <div className="kb__actions">
          <button
            type="button"
            className="p-button--positive u-no-margin--bottom"
            onClick={() => setShowCreate(true)}
          >
            Create knowledge base
          </button>
          <button
            type="button"
            className="p-button u-no-margin--bottom"
            onClick={() => setShowImport(true)}
          >
            Import
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

      {!bases && !loadError && <Spinner label="Loading knowledge bases…" />}

      {bases && bases.length === 0 && !loadError && (
        <EmptyState
          headline="No knowledge bases yet."
          guidance="Create one to start ingesting documents and chatting with them."
          command="rag-cli.rag k create <name>"
          action={
            <button
              type="button"
              className="p-button--positive u-no-margin--bottom"
              onClick={() => setShowCreate(true)}
            >
              Create knowledge base
            </button>
          }
        />
      )}

      {bases && bases.length > 0 && (
        <div className="kb__table-wrap">
          <table aria-label="Knowledge bases">
            <thead>
              <tr>
                <th>Name</th>
                <th className="u-align--right">Sources</th>
                <th className="u-align--right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {bases.map((base) => (
                <tr key={base.name}>
                  <td>
                    <Link href={{ pathname: "/knowledge/", query: { kb: base.name } }}>{base.name}</Link>
                  </td>
                  <td className="u-align--right">{base.source_count}</td>
                  <td className="u-align--right">
                    <div className="kb-actions-cell">
                      <Link
                        className="p-button--base kb-action u-no-margin--bottom"
                        href={{ pathname: "/knowledge/", query: { kb: base.name } }}
                      >
                        Open
                      </Link>
                      <button
                        type="button"
                        className="p-button--base kb-action u-no-margin--bottom"
                        onClick={() => void onExport(base.name)}
                      >
                        Export
                      </button>
                      <button
                        type="button"
                        className="p-button--base kb-action u-no-margin--bottom"
                        onClick={() => setDeleteTarget(base)}
                      >
                        Delete
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {showCreate && (
        <CreateKbModal
          onCreated={(name) => {
            setShowCreate(false);
            notify({ kind: "positive", message: `Knowledge base “${name}” created.` });
            void load();
          }}
          onClose={() => setShowCreate(false)}
        />
      )}

      {showImport && (
        <ImportModal
          onStarted={(op) => {
            setShowImport(false);
            opKinds.current[op.id] = { kind: "import", name: "" };
            track(op);
            notify({ kind: "positive", message: "Importing knowledge base…" });
          }}
          onClose={() => setShowImport(false)}
        />
      )}

      {deleteTarget && (
        <ConfirmModal
          title="Delete knowledge base"
          confirmLabel="Delete"
          confirmPhrase={deleteTarget.name}
          destructive
          busy={deleting}
          onConfirm={() => void onDelete()}
          onClose={() => setDeleteTarget(null)}
        >
          <p>
            Deletes the index and all {deleteTarget.source_count} ingested source
            {deleteTarget.source_count === 1 ? "" : "s"}. This cannot be undone.
          </p>
        </ConfirmModal>
      )}
    </>
  );
}
