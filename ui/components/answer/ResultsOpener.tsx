"use client";

import { useCallback } from "react";
import { parseResultsJSON, ResultsParseError } from "@/lib/results";
import type { ParsedQAFile } from "@/lib/types";

interface Props {
  // onOpen hands the parsed results (and the file name) up so AnswerScreen can
  // switch to the review surface.
  onOpen: (parsed: ParsedQAFile, name?: string) => void;
  onError: (message: string) => void;
}

// ResultsOpener is Flow 3: open a previously exported results JSON from disk
// into the review surface, without a run. Malformed files surface a validation
// error rather than a broken surface.
export default function ResultsOpener({ onOpen, onError }: Props) {
  const onFile = useCallback(
    async (file: File | undefined) => {
      if (!file) return;
      try {
        const text = await file.text();
        onOpen(parseResultsJSON(text), file.name);
      } catch (e) {
        onError(
          e instanceof ResultsParseError ? e.message : `could not read results file: ${String(e)}`
        );
      }
    },
    [onOpen, onError]
  );

  // The whole card is the control: a <label> wrapping a hidden file input, so a
  // click anywhere on the square opens the results-file picker (matching the Run
  // and Build entry cards, which are buttons).
  return (
    <label className="answer-entry">
      <span className="answer-entry__title">Review results</span>
      <span className="answer-entry__desc u-text--muted p-text--small">
        Open a previously exported results file to review the answers.
      </span>
      <input
        type="file"
        accept=".json,application/json"
        className="u-off-screen"
        onChange={(e) => void onFile(e.target.files?.[0])}
      />
    </label>
  );
}
