import test from "node:test";
import assert from "node:assert/strict";
import { applyAnswerFrame, type Message } from "./chatMessages";
import type { CitedSource } from "@/lib/types";

const src = (marker: number, key: string): CitedSource => ({
  marker,
  key,
  label: key,
  base: "docs",
  tag: "canonical",
});

const streaming = (): Message[] => [
  { role: "user", content: "Does Pro include FIPS?", think: "" },
  { role: "assistant", content: "", think: "", streaming: true },
];

test("each sources frame replaces the documents cited so far", () => {
  let msgs = applyAnswerFrame(streaming(), { type: "sources", sources: [src(1, "b")] });
  assert.deepEqual(msgs[1].citedSoFar?.map((s) => s.key), ["b"]);
  msgs = applyAnswerFrame(msgs, { type: "sources", sources: [src(1, "b"), src(2, "a")] });
  assert.deepEqual(msgs[1].citedSoFar?.map((s) => s.marker), [1, 2]);
  assert.equal(msgs[1].streaming, true);
});

test("a done frame keeps the cited sources and the streamed text", () => {
  let msgs = applyAnswerFrame(streaming(), { type: "sources", sources: [src(1, "b")] });
  msgs[1] = { ...msgs[1], content: "Yes. [1]" };
  msgs = applyAnswerFrame(msgs, { type: "done", sources: [src(1, "b")] });
  assert.equal(msgs[1].streaming, false);
  assert.deepEqual(msgs[1].sources?.map((s) => s.marker), [1]);
  assert.equal(msgs[1].content, "Yes. [1]");
});

test("an unsourced done frame keeps the streamed text and flags it", () => {
  let msgs = streaming();
  msgs[1] = { ...msgs[1], content: "Yes, Pro includes FIPS." };
  msgs = applyAnswerFrame(msgs, { type: "done", unsourced: true });
  assert.equal(msgs[1].unsourced, true);
  assert.equal(msgs[1].content, "Yes, Pro includes FIPS.");
  assert.deepEqual(msgs[1].sources, []);
});

test("a sources frame after the answer ended is ignored", () => {
  const done: Message[] = [{ role: "assistant", content: "Yes. [1]", think: "", sources: [src(1, "b")] }];
  assert.equal(applyAnswerFrame(done, { type: "sources", sources: [src(1, "a")] }), done);
});
