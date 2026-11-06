import { test } from "node:test";
import assert from "node:assert/strict";
import { runWarnings } from "./runWarnings";

test("reads the run's warnings from operation metadata", () => {
  const md = { questions_total: 3, warnings: ["kapa.ai is not configured", "kapa.ai search failed"] };
  assert.deepEqual(runWarnings(md), ["kapa.ai is not configured", "kapa.ai search failed"]);
});

test("a run without warnings has none", () => {
  assert.deepEqual(runWarnings({ questions_total: 3 }), []);
  assert.deepEqual(runWarnings(null), []);
  assert.deepEqual(runWarnings(undefined), []);
});

test("ignores a malformed warnings value", () => {
  assert.deepEqual(runWarnings({ warnings: "not a list" }), []);
  assert.deepEqual(runWarnings({ warnings: ["ok", 3, null, " "] }), ["ok"]);
});
