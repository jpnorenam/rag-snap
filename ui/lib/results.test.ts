import test from "node:test";
import assert from "node:assert/strict";
import { filterByRating, normalizeQAFile, parseResultsJSON, toResultsFile } from "./results";
import type { QAFile } from "./types";

const judged: QAFile = {
  generated_at: "2026-10-22T10:00:00Z",
  model: "global.anthropic.claude-sonnet-5-5",
  prompt: "rfp@3",
  judge: { model: "gemini-test", green: 1, yellow: 1, red: 1, not_judged: 1, consistency: true },
  results: [
    { id: "A", question: "q", answer: "a", judgment: { confidence: 5, rating: "green" } },
    { id: "B", question: "q", answer: "a", judgment: { confidence: 3, rating: "yellow", comment: "verify" } },
    { id: "C", question: "q", answer: "a", judgment: { confidence: 1, rating: "red", comment: "gap" } },
    { id: "D", question: "q", answer: "a", judgment: { error: "judge call failed" } },
  ],
};

test("a judged file keeps its summary and verdicts through parse and export", () => {
  const parsed = parseResultsJSON(JSON.stringify(judged));
  assert.equal(parsed.judge?.model, "gemini-test");
  assert.equal(parsed.items[1].judgment?.comment, "verify");
  // Export re-emits the CLI shape verbatim, so the file round-trips.
  assert.deepEqual(toResultsFile(parsed), judged);
});

test("a file without verdicts parses with no judge summary", () => {
  const parsed = normalizeQAFile({ generated_at: "", model: "m", results: [{ id: "1", question: "q", answer: "a" }] });
  assert.equal(parsed.judge, undefined);
  assert.ok(!("judge" in toResultsFile(parsed)));
});

test("filtering keeps each item's original index", () => {
  const parsed = normalizeQAFile(judged);
  const visible = filterByRating(parsed.items, new Set(["red", "yellow"]));
  // Row numbers derive from the index: B is result 1 (row 3), C is result 2 (row 4).
  assert.deepEqual(
    visible.map(({ item, i }) => [item.id, i]),
    [
      ["B", 1],
      ["C", 2],
    ]
  );
  assert.deepEqual(
    filterByRating(parsed.items, new Set(["not_judged"])).map(({ item }) => item.id),
    ["D"]
  );
});
