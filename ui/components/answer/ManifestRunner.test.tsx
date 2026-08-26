import test from "node:test";
import assert from "node:assert/strict";
import { routingPreview } from "./ManifestRunner";
import type { BatchManifest } from "@/lib/api/answer";

// routingPreview is what the pre-run preview asserts about a manifest: which
// entry reaches how many questions, and which questions it reaches nothing for.
// The resolution itself is covered by lib/domains.test.ts against the shared
// Go/TS fixture; these are the counts built on top of it.

function manifest(m: Partial<BatchManifest>): BatchManifest {
  return { questions: [], ...m };
}

test("no routing table means no summary", () => {
  const questions = [{ id: "C4", question: "Q" }];
  assert.equal(routingPreview(manifest({ questions })), null);
  assert.equal(routingPreview(manifest({ questions, domains: [] })), null);
});

test("counts the questions each entry applies to, in document order", () => {
  const preview = routingPreview(
    manifest({
      domains: [
        { match: "C*", context: "Enhanced Platform Awareness" },
        { match: "J*", context: "Hardware" },
        { match: "J1.*", context: "Storage vendor" },
      ],
      questions: [
        { id: "C1", question: "a" },
        { id: "C2", question: "b" },
        { id: "J2.1", question: "c" },
        { id: "J1.4", question: "d" },
        { id: "A9", question: "e" },
      ],
    })
  );
  assert.ok(preview);
  assert.deepEqual(
    preview.entries.map((e) => [e.domain.match, e.count]),
    [
      ["C*", 2],
      ["J*", 1],
      ["J1.*", 1],
    ]
  );
  // The narrower pattern takes J1.4 off "J*", as the run would.
  assert.deepEqual(preview.matches, ["C*", "C*", "J*", "J1.*", null]);
  assert.equal(preview.unrouted, 1);
});

test("an entry that reaches nothing is still listed, with a count of zero", () => {
  // A pattern that matches no question is the mistake the summary exists to
  // surface, so it must appear rather than be filtered out.
  const preview = routingPreview(
    manifest({
      domains: [{ match: "GIS*", context: "Mapping" }],
      questions: [{ id: "C4", question: "a" }],
    })
  );
  assert.ok(preview);
  assert.deepEqual(preview.entries.map((e) => e.count), [0]);
  assert.deepEqual(preview.matches, [null]);
  assert.equal(preview.unrouted, 1);
});

test("a bare numeric id is counted against its source", () => {
  const preview = routingPreview(
    manifest({
      domains: [{ match: "GIS*", context: "Mapping" }],
      questions: [
        { id: "17", question: "a", source: "GIS Deliverables" },
        { id: "18", question: "b", source: "Admin" },
      ],
    })
  );
  assert.ok(preview);
  assert.deepEqual(preview.entries.map((e) => e.count), [1]);
  assert.deepEqual(preview.matches, ["GIS*", null]);
});

test("a question with no id routes only through a catch-all", () => {
  // chat.RunBatch resolves on q.ID verbatim, with no positional fallback, so the
  // preview must not substitute the question's position for a missing id.
  const questions = [{ question: "a" }];
  const specific = routingPreview(manifest({ domains: [{ match: "1*", context: "x" }], questions }));
  assert.deepEqual(specific?.matches, [null]);
  const catchAll = routingPreview(manifest({ domains: [{ match: "*", context: "x" }], questions }));
  assert.deepEqual(catchAll?.matches, ["*"]);
});

test("keywords-only entries are summarized like any other", () => {
  const preview = routingPreview(
    manifest({
      domains: [{ match: "C*", keywords: ["sriov", "numa"] }],
      questions: [{ id: "C4", question: "a" }],
    })
  );
  assert.ok(preview);
  assert.equal(preview.entries[0].count, 1);
  assert.deepEqual(preview.entries[0].domain.keywords, ["sriov", "numa"]);
  assert.equal(preview.entries[0].domain.context, undefined);
});

test("an uncompilable table yields no summary rather than throwing", () => {
  // parseManifest rejects these before the screen ever holds one, so this is the
  // defensive path: the run controls must not be taken down by the summary.
  assert.equal(
    routingPreview(
      manifest({ domains: [{ match: "C*" }], questions: [{ id: "C4", question: "a" }] })
    ),
    null
  );
});