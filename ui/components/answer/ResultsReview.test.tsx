import test from "node:test";
import assert from "node:assert/strict";
import { renderToStaticMarkup } from "react-dom/server";
import ResultsReview from "./ResultsReview";
import type { ParsedQAFile } from "@/lib/types";

const noop = () => {};

const base: ParsedQAFile = {
  generated_at: "",
  model: "m",
  items: [{ id: "1", question: "q", answer: "a" }],
};

test("a results file with no verdicts renders without the judge strip", () => {
  const html = renderToStaticMarkup(
    <ResultsReview parsed={base} resumable={false} consumed={false} onConsumed={noop} onExit={noop} />
  );
  assert.ok(!html.includes("answer-judge"));
  assert.ok(!html.includes("qa-verdict"));
});

test("a judged file shows the per-rating counts as pressed toggles", () => {
  const parsed: ParsedQAFile = {
    ...base,
    judge: { model: "gemini-test", green: 0, yellow: 0, red: 1, not_judged: 0, consistency: false, consistency_error: "quota" },
    items: [{ id: "1", question: "q", answer: "a", judgment: { confidence: 1, rating: "red", comment: "gap" } }],
  };
  const html = renderToStaticMarkup(
    <ResultsReview parsed={parsed} resumable={false} consumed={false} onConsumed={noop} onExit={noop} />
  );
  assert.match(html, /Judged by gemini-test/);
  assert.match(html, /aria-pressed="true"/);
  assert.ok(!html.includes("p-chip--positive"), "rating chips stay neutral");
  assert.match(html, /<span class="p-chip__lead">Red<\/span><span class="p-chip__value">1<\/span>/);
  assert.match(html, /consistency check did not complete: quota/);
});

const judged = { model: "gemini-test", green: 0, yellow: 2, red: 0, not_judged: 0, consistency: true };

test("consistency findings are listed once, a broad one marked as applied to no row", () => {
  const many = Array.from({ length: 284 }, (_, k) => ({ row: k + 2, phrase: "Customer A - RFI" }));
  const parsed: ParsedQAFile = {
    ...base,
    judge: {
      ...judged,
      consistency_findings: [
        { rows: [{ row: 47, phrase: "projected for 2026" }, { row: 263, phrase: "is available" }], comment: "S3 replication timing differs." },
        { rows: many, comment: "Repeated mentions of other customers.", broad: true },
      ],
      consistency_discarded: 1,
    },
  };
  const html = renderToStaticMarkup(
    <ResultsReview parsed={parsed} resumable={false} consumed={false} onConsumed={noop} onExit={noop} />
  );
  assert.equal(html.split("S3 replication timing differs.").length - 1, 1, "each finding is shown once");
  assert.match(html, /aria-label="Go to row 47"/);
  assert.match(html, /aria-label="Go to row 263"/);
  assert.match(html, /Not applied to rows: 284 rows is too broad/);
  assert.match(html, /… and 276 more/);
  assert.ok(!html.includes('aria-label="Go to row 285"'), "a broad finding's row list is abbreviated");
  assert.match(html, /1 finding was discarded/);
});

test("a judged file without consistency findings shows no consistency section", () => {
  const parsed: ParsedQAFile = { ...base, judge: judged };
  const html = renderToStaticMarkup(
    <ResultsReview parsed={parsed} resumable={false} consumed={false} onConsumed={noop} onExit={noop} />
  );
  assert.ok(!html.includes("answer-consistency"));
});
