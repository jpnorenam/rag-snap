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

  return (
    <label className="p-button u-no-margin--bottom answer-entry__file">
      Open results file
      <input
        type="file"
        accept=".json,application/json"
        className="u-off-screen"
        onChange={(e) => void onFile(e.target.files?.[0])}
      />
    </label>
  );
}
