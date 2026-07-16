"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Spinner from "@/components/common/Spinner";
import ConfirmModal from "@/components/common/ConfirmModal";
import { errorMessage } from "@/lib/api/envelope";
import { useOperations } from "@/lib/useOperations";
import { isTerminal, statusOf, type OperationView } from "@/lib/api/operations";
import {
  buildFromDocument,
  buildExtract,
  needsColumn,
  parseInspectMetadata,
  BuildMetadataError,
  runBatch,
  type BatchManifest,
  type BuildQuestion,
  type BuildTable,
} from "@/lib/api/answer";
import { serializeManifest } from "@/lib/manifest";
import { listKnowledge, type KnowledgeBase } from "@/lib/api/knowledge";

interface Props {
  onRun: (op: OperationView, manifest: BatchManifest, name?: string) => void;
  onCancel: () => void;
  onError: (message: string) => void;
}

// Wizard steps. Free-text (PDF/DOCX): upload → inspecting → review → configure.
// Tabular (XLSX/CSV): upload → inspecting → columns → extracting → review →
// configure. "inspecting" is pass 1 (document read); "extracting" is pass 2
// (chosen-column extraction).
type WizardStep = "upload" | "inspecting" | "columns" | "extracting" | "review" | "configure";

// DEFAULT_MIN_LENGTH matches the daemon's default minimum cell length.
const DEFAULT_MIN_LENGTH = 20;

// EditableQuestion is a candidate question in the review step: its text plus
// whether it is selected for the manifest.
interface EditableQuestion {
  id: string;
  question: string;
  source?: string;
  selected: boolean;
}

const DEFAULT_TEMPERATURE = 0.1;
const ACCEPT = ".pdf,.docx,.xlsx,.csv,application/pdf,text/csv";

// BuildWizard is Flow 2: upload a document, extract candidate questions server-
// side (POST /1.0/answer/build, tracked operation), review/edit them, then
// download or run the resulting manifest. Wizard state is in-memory; an unsaved
// built manifest warns on navigation away.
export default function BuildWizard({ onRun, onCancel, onError }: Props) {
  const ops = useOperations();
  const [step, setStep] = useState<WizardStep>("upload");
  const [refine, setRefine] = useState(true);
  const [fileName, setFileName] = useState("");
  const [buildOpId, setBuildOpId] = useState<string | null>(null);
  const [questions, setQuestions] = useState<EditableQuestion[]>([]);
  // Column-selection state (tabular formats only): the parsed tables, the staged
  // build token, and the current table/column/min-length choice.
  const [buildToken, setBuildToken] = useState("");
  const [tables, setTables] = useState<BuildTable[]>([]);
  const [selectedTable, setSelectedTable] = useState(0);
  const [selectedColumn, setSelectedColumn] = useState(0);
  const [minLength, setMinLength] = useState(DEFAULT_MIN_LENGTH);

  // Configure step state.
  const [bases, setBases] = useState<KnowledgeBase[]>([]);
  const [activeBases, setActiveBases] = useState<string[]>([]);
  const [temperature, setTemperature] = useState(DEFAULT_TEMPERATURE);
  // Defaults to the uploaded document's name (extension stripped) once a file is
  // chosen; the user can overwrite it in the configure step. Empty until upload.
  const [manifestName, setManifestName] = useState("");
  const [saved, setSaved] = useState(false);
  const [running, setRunning] = useState(false);
  // Cancel-run confirmation for the extraction step.
  const [confirmCancel, setConfirmCancel] = useState(false);
  const [cancelBusy, setCancelBusy] = useState(false);

  const buildOp = useMemo<OperationView | null>(
    () => (buildOpId ? ops.operations.find((o) => o.id === buildOpId) ?? null : null),
    [ops.operations, buildOpId]
  );

  // Track whether there is unsaved wizard progress worth warning about.
  const dirtyRef = useRef(false);
  dirtyRef.current = questions.length > 0 && !saved && step !== "upload";

  // Warn before unloading the page with an unsaved built manifest.
  useEffect(() => {
    const handler = (e: BeforeUnloadEvent) => {
      if (dirtyRef.current) {
        e.preventDefault();
        e.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", handler);
    return () => window.removeEventListener("beforeunload", handler);
  }, []);

  // Load KBs for the configure step's selector.
  useEffect(() => {
    listKnowledge()
      .then(setBases)
      .catch(() => setBases([]));
  }, []);

  // Build-operation completion. Both passes are one-shot operations whose result
  // the wizard consumes, so each is dismissed from the indicator on completion —
  // neither is a resumable batch run (the wizard's step state is local).
  //   inspecting → columns (tabular) or review (free-text, questions inline)
  //   extracting → review (chosen-column questions)
  useEffect(() => {
    if (!buildOp || !isTerminal(buildOp)) return;
    if (step !== "inspecting" && step !== "extracting") return;
    const status = statusOf(buildOp);
    const id = buildOp.id;
    const finish = (next: WizardStep) => {
      ops.dismiss(id);
      setBuildOpId(null);
      setStep(next);
    };

    if (status !== "succeeded") {
      onError(buildOp.err || `${step === "inspecting" ? "document read" : "extraction"} ${status}`);
      finish("upload");
      return;
    }

    if (step === "inspecting" && needsColumn(buildOp.metadata)) {
      // Tabular: validate + normalize the parsed tables (samples/headers may be
      // null in a real response) before entering the column step. A malformed
      // shape becomes a handled error, never a render crash.
      let meta;
      try {
        meta = parseInspectMetadata(buildOp.metadata);
      } catch (e) {
        onError(e instanceof BuildMetadataError ? e.message : "could not read the parsed document");
        finish("upload");
        return;
      }
      setBuildToken(meta.build_token);
      setTables(meta.tables);
      setSelectedTable(meta.suggested.table_index);
      setSelectedColumn(meta.suggested.column_index);
      setMinLength(DEFAULT_MIN_LENGTH);
      finish("columns");
      return;
    }

    // Free-text inspect, or a completed column extraction: questions are inline.
    const extracted = readQuestions(buildOp.metadata);
    if (extracted.length === 0) {
      onError("No questions could be extracted from the document.");
      finish(step === "extracting" ? "columns" : "upload");
      return;
    }
    setQuestions(
      extracted.map((q) => ({ id: q.id, question: q.question, source: q.source, selected: true }))
    );
    finish("review");
  }, [step, buildOp, ops, onError]);

  // extractColumn runs pass 2 for the chosen table/column/min-length.
  const extractColumn = useCallback(async () => {
    setStep("extracting");
    try {
      const { view } = await buildExtract({
        buildToken,
        tableIndex: selectedTable,
        columnIndex: selectedColumn,
        minLength,
        refine,
      });
      ops.track(view, "/answer/");
      setBuildOpId(view.id);
    } catch (e) {
      onError(errorMessage(e));
      setStep("columns");
    }
  }, [buildToken, selectedTable, selectedColumn, minLength, refine, ops, onError]);

  // cancelBuild cancels the in-flight build operation (inspect or extract) and
  // returns to the appropriate prior step. Copy is phase-appropriate — nothing
  // meaningful is lost, so it does not use the batch-run "progress lost" warning.
  const cancelBuild = useCallback(async () => {
    const returnStep: WizardStep = step === "extracting" ? "columns" : "upload";
    setCancelBusy(true);
    try {
      if (buildOpId) {
        await ops.cancel(buildOpId);
        ops.dismiss(buildOpId);
      }
    } catch {
      // Daemon may refuse if it already finished; drop the local view regardless.
    } finally {
      setCancelBusy(false);
      setConfirmCancel(false);
      setBuildOpId(null);
      setStep(returnStep);
    }
  }, [ops, buildOpId, step]);

  const onFile = useCallback(
    async (file: File | undefined) => {
      if (!file) return;
      setFileName(file.name);
      // Default the manifest name to the document's name with the extension
      // stripped (e.g. "vendor-rfp.pdf" → "vendor-rfp"); the user can still edit
      // it in the configure step.
      setManifestName(file.name.replace(/\.[^.]+$/, ""));
      setStep("inspecting");
      try {
        const { view } = await buildFromDocument(file, { refine });
        // Record the originating route so the indicator can link back to the
        // Answer tab while the build runs.
        ops.track(view, "/answer/");
        setBuildOpId(view.id);
      } catch (e) {
        onError(errorMessage(e));
        setStep("upload");
      }
    },
    [refine, ops, onError]
  );

  const selectedCount = questions.filter((q) => q.selected).length;

  const buildManifest = useCallback((): BatchManifest => {
    const chosen = questions.filter((q) => q.selected && q.question.trim());
    return {
      // version is the manifest schema version, not the display name — the name
      // travels separately (Download filename / onRun name), so keep this "1.0".
      version: "1.0",
      knowledge_bases: activeBases.length > 0 ? activeBases : undefined,
      temperature,
      questions: chosen.map((q, i) => ({ id: q.id || String(i + 1), question: q.question.trim() })),
    };
  }, [questions, activeBases, temperature]);

  const downloadManifest = useCallback(() => {
    const yaml = serializeManifest(buildManifest());
    const blob = new Blob([yaml], { type: "application/x-yaml" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${manifestName.trim() || "manifest"}.yaml`;
    a.click();
    URL.revokeObjectURL(url);
    setSaved(true);
  }, [buildManifest, manifestName]);

  const doRun = useCallback(async () => {
    setRunning(true);
    try {
      const manifest = buildManifest();
      const { view } = await runBatch(manifest);
      setSaved(true);
      // Name the run by the manifest name the user entered in this step.
      onRun(view, manifest, manifestName.trim() || undefined);
    } catch (e) {
      onError(errorMessage(e));
      setRunning(false);
    }
  }, [buildManifest, manifestName, onRun, onError]);

  return (
    <>
    <section className="answer-flow">
      <div className="answer-flow__head">
        <h2 className="p-heading--4 u-no-margin--bottom">Build from a document</h2>
        {step === "inspecting" || step === "extracting" ? (
          // While a build op runs, the in-place exit is Cancel (confirmed).
          // Navigating away leaves it in the indicator.
          <button
            type="button"
            className="p-button--base u-no-margin--bottom"
            onClick={() => setConfirmCancel(true)}
          >
            Cancel
          </button>
        ) : (
          <button type="button" className="p-button--base u-no-margin--bottom" onClick={onCancel}>
            Back
          </button>
        )}
      </div>

      <WizardSteps step={step} tabular={tables.length > 0} />

      {step === "upload" && (
        <div className="answer-wizard__upload">
          <div className="p-form__group">
            <label className="p-form__label" htmlFor="build-file">
              RFP document (PDF, DOCX, XLSX, CSV)
            </label>
            <input
              id="build-file"
              type="file"
              accept={ACCEPT}
              onChange={(e) => void onFile(e.target.files?.[0])}
            />
          </div>
          <label className="p-checkbox">
            <input
              type="checkbox"
              className="p-checkbox__input"
              checked={refine}
              onChange={(e) => setRefine(e.target.checked)}
            />
            <span className="p-checkbox__label">Refine questions with the model</span>
          </label>
          <p className="u-text--muted p-text--small">
            Uses the chat model to clean up extracted questions.
          </p>
        </div>
      )}

      {step === "inspecting" && (
        <div className="answer-running" aria-live="polite">
          <Spinner label={`Reading ${fileName}…`} />
          <p className="u-text--muted p-text--small">
            This runs as a background operation — you can track or cancel it from the operations
            indicator in the top bar.
          </p>
        </div>
      )}

      {step === "columns" && tables.length > 0 && (
        <ColumnStep
          tables={tables}
          selectedTable={selectedTable}
          setSelectedTable={setSelectedTable}
          selectedColumn={selectedColumn}
          setSelectedColumn={setSelectedColumn}
          minLength={minLength}
          setMinLength={setMinLength}
          onBack={onCancel}
          onContinue={() => void extractColumn()}
        />
      )}

      {step === "extracting" && (
        <div className="answer-running" aria-live="polite">
          <Spinner label="Extracting questions from the chosen column…" />
          <p className="u-text--muted p-text--small">
            This runs as a background operation — you can track or cancel it from the operations
            indicator in the top bar.
          </p>
        </div>
      )}

      {step === "review" && (
        <ReviewStep
          questions={questions}
          setQuestions={setQuestions}
          selectedCount={selectedCount}
          onBack={() => setStep(tables.length > 0 ? "columns" : "upload")}
          onContinue={() => setStep("configure")}
        />
      )}

      {step === "configure" && (
        <div className="answer-wizard__configure">
          {bases.length > 0 && (
            <div className="kb-selector">
              <span className="kb-selector__label">Knowledge bases:</span>
              {bases.map((b) => (
                <button
                  key={b.name}
                  type="button"
                  className={`p-chip u-no-margin--bottom ${
                    activeBases.includes(b.name) ? "p-chip--positive" : ""
                  }`}
                  onClick={() =>
                    setActiveBases((prev) =>
                      prev.includes(b.name) ? prev.filter((x) => x !== b.name) : [...prev, b.name]
                    )
                  }
                >
                  <span className="p-chip__value">{b.name}</span>
                </button>
              ))}
            </div>
          )}

          <div className="p-form__group answer-flow__temp">
            <label htmlFor="build-temp">Temperature</label>
            <input
              id="build-temp"
              type="number"
              min={0}
              max={2}
              step={0.1}
              value={temperature}
              onChange={(e) => setTemperature(Number(e.target.value))}
            />
          </div>

          <div className="p-form__group">
            <label htmlFor="manifest-name">Manifest name</label>
            <input
              id="manifest-name"
              type="text"
              value={manifestName}
              onChange={(e) => setManifestName(e.target.value)}
            />
          </div>

          <div className="answer-wizard__actions">
            <button type="button" className="p-button--base u-no-margin--bottom" onClick={() => setStep("review")}>
              Back
            </button>
            <button type="button" className="p-button u-no-margin--bottom" onClick={downloadManifest}>
              Download manifest
            </button>
            <button
              type="button"
              className="p-button--positive u-no-margin--bottom"
              onClick={() => void doRun()}
              disabled={running || selectedCount === 0}
            >
              {running ? (
                <>
                  <i className="p-icon--spinner u-animation--spin" aria-hidden="true" /> Running…
                </>
              ) : (
                "Run batch"
              )}
            </button>
          </div>
        </div>
      )}
    </section>

    {confirmCancel && (
      <ConfirmModal
        title={step === "extracting" ? "Cancel extraction" : "Cancel"}
        confirmLabel={step === "extracting" ? "Cancel extraction" : "Cancel reading"}
        busy={cancelBusy}
        onConfirm={() => void cancelBuild()}
        onClose={() => setConfirmCancel(false)}
      >
        {/* Phase-appropriate copy: no batch has run, so nothing meaningful is
            lost — do not use the batch-run "progress will be lost" warning. */}
        {step === "extracting" ? (
          <p>Stop extracting questions from this column? You can pick a column again.</p>
        ) : (
          <p>Stop reading this document? You can upload it again.</p>
        )}
      </ConfirmModal>
    )}
    </>
  );
}

// WizardSteps is the slim step indicator. The current step is bold via text
// emphasis only — no brand-orange accent in content (foundation §1).
function WizardSteps({ step, tabular }: { step: WizardStep; tabular: boolean }) {
  // Free-text: Upload → Review → Configure. Tabular inserts "Choose column".
  const items: { key: WizardStep; label: string }[] = tabular
    ? [
        { key: "upload", label: "1. Upload" },
        { key: "columns", label: "2. Choose column" },
        { key: "review", label: "3. Review questions" },
        { key: "configure", label: "4. Configure & run" },
      ]
    : [
        { key: "upload", label: "1. Upload" },
        { key: "review", label: "2. Review questions" },
        { key: "configure", label: "3. Configure & run" },
      ];
  // Map in-flight phases onto the nearest visible step.
  const currentKey: WizardStep =
    step === "inspecting" ? "upload" : step === "extracting" ? "columns" : step;
  return (
    <ol className="answer-steps" aria-label="Build steps">
      {items.map((it) => (
        <li
          key={it.key}
          className={`answer-steps__item ${it.key === currentKey ? "is-current" : ""}`}
          aria-current={it.key === currentKey ? "step" : undefined}
        >
          {it.label}
        </li>
      ))}
    </ol>
  );
}

// ReviewStep renders the editable candidate-question list with a sticky footer
// showing the selection count.
function ReviewStep({
  questions,
  setQuestions,
  selectedCount,
  onBack,
  onContinue,
}: {
  questions: EditableQuestion[];
  setQuestions: React.Dispatch<React.SetStateAction<EditableQuestion[]>>;
  selectedCount: number;
  onBack: () => void;
  onContinue: () => void;
}) {
  const toggle = (idx: number) =>
    setQuestions((prev) => prev.map((q, i) => (i === idx ? { ...q, selected: !q.selected } : q)));
  const edit = (idx: number, text: string) =>
    setQuestions((prev) => prev.map((q, i) => (i === idx ? { ...q, question: text } : q)));
  const add = () =>
    setQuestions((prev) => [
      ...prev,
      { id: String(prev.length + 1), question: "", selected: true },
    ]);

  return (
    <div className="answer-wizard__review">
      <ul className="answer-questions">
        {questions.map((q, i) => (
          <li key={i} className="answer-questions__row">
            <label className="p-checkbox answer-questions__check">
              <input
                type="checkbox"
                className="p-checkbox__input"
                checked={q.selected}
                onChange={() => toggle(i)}
                aria-label={`Include question ${i + 1}`}
              />
              <span className="p-checkbox__label" />
            </label>
            <textarea
              className="answer-questions__text"
              value={q.question}
              rows={Math.max(1, Math.ceil((q.question.length || 1) / 80))}
              onChange={(e) => edit(i, e.target.value)}
              aria-label={`Question ${i + 1}`}
            />
          </li>
        ))}
      </ul>

      <button type="button" className="p-button u-no-margin--bottom" onClick={add}>
        Add a question
      </button>

      <div className="answer-questions__footer">
        <span className="u-text--muted p-text--small">
          {selectedCount} of {questions.length} question{questions.length === 1 ? "" : "s"} selected
        </span>
        <div className="answer-wizard__actions">
          <button type="button" className="p-button--base u-no-margin--bottom" onClick={onBack}>
            Back
          </button>
          <button
            type="button"
            className="p-button--positive u-no-margin--bottom"
            onClick={onContinue}
            disabled={selectedCount === 0}
          >
            Continue
          </button>
        </div>
      </div>
    </div>
  );
}

// ColumnStep lets the user choose which column holds the questions (with the
// heuristic's suggestion preselected), the table when there is more than one,
// and a minimum cell length. It runs pass 2 on continue.
function ColumnStep({
  tables,
  selectedTable,
  setSelectedTable,
  selectedColumn,
  setSelectedColumn,
  minLength,
  setMinLength,
  onBack,
  onContinue,
}: {
  tables: BuildTable[];
  selectedTable: number;
  setSelectedTable: (i: number) => void;
  selectedColumn: number;
  setSelectedColumn: (i: number) => void;
  minLength: number;
  setMinLength: (n: number) => void;
  onBack: () => void;
  onContinue: () => void;
}) {
  const table = tables[selectedTable] ?? tables[0];

  return (
    <div className="answer-wizard__columns">
      {tables.length > 1 && (
        <div className="p-form__group">
          <label htmlFor="build-table">Sheet / table</label>
          <select
            id="build-table"
            value={selectedTable}
            onChange={(e) => {
              const t = Number(e.target.value);
              setSelectedTable(t);
              // Reset the column to the new table's suggested column.
              const sug = tables[t]?.columns.find((c) => c.suggested);
              setSelectedColumn(sug ? sug.index : 0);
            }}
          >
            {tables.map((t, i) => (
              <option key={i} value={i}>
                {t.name} ({t.row_count} row{t.row_count === 1 ? "" : "s"})
              </option>
            ))}
          </select>
        </div>
      )}

      <fieldset className="answer-columns">
        <legend className="p-form__label">Which column holds the questions?</legend>
        <p className="u-text--muted p-text--small">
          The first row is treated as a header. Pick the column with the question text.
        </p>
        {table.columns.map((c) => {
          const header = table.header[c.index] || `Column ${c.index + 1}`;
          return (
            <label key={c.index} className="answer-columns__option">
              <input
                type="radio"
                name="question-column"
                className="p-radio__input"
                checked={selectedColumn === c.index}
                onChange={() => setSelectedColumn(c.index)}
              />
              <span className="answer-columns__body">
                <span className="answer-columns__header">
                  {header}
                  {c.suggested && <span className="answer-columns__suggested"> · suggested</span>}
                </span>
                <span className="answer-columns__samples u-text--muted p-text--small">
                  {c.sample && c.sample.length > 0
                    ? c.sample.join(" · ")
                    : "(no sample values)"}
                </span>
              </span>
            </label>
          );
        })}
      </fieldset>

      <div className="p-form__group answer-flow__temp">
        <label htmlFor="min-length">Minimum characters</label>
        <input
          id="min-length"
          type="number"
          min={0}
          step={1}
          value={minLength}
          onChange={(e) => setMinLength(Number(e.target.value))}
        />
      </div>
      <p className="u-text--muted p-text--small">
        Skip cells shorter than this — lower it if short questions are missed.
      </p>

      <div className="answer-wizard__actions">
        <button type="button" className="p-button--base u-no-margin--bottom" onClick={onBack}>
          Back
        </button>
        <button
          type="button"
          className="p-button--positive u-no-margin--bottom"
          onClick={onContinue}
        >
          Continue
        </button>
      </div>
    </div>
  );
}

// readQuestions pulls the extracted candidate questions from the build
// operation's metadata.questions (buildQuestionJSON shape).
function readQuestions(metadata: Record<string, unknown>): BuildQuestion[] {
  const raw = metadata?.questions;
  if (!Array.isArray(raw)) return [];
  return raw
    .filter((q): q is BuildQuestion => typeof q === "object" && q !== null && "question" in q)
    .map((q) => ({
      id: typeof q.id === "string" ? q.id : "",
      question: String(q.question ?? ""),
      source: typeof q.source === "string" ? q.source : undefined,
    }));
}
