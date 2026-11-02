import test from "node:test";
import assert from "node:assert/strict";
import { answerText, applyAnswerFrame, type Message } from "./chatMessages";
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

test("a sources frame records the retrieved documents on the streaming answer", () => {
  const next = applyAnswerFrame(streaming(), { type: "sources", sources: [src(1, "a"), src(2, "b")] });
  assert.deepEqual(next[1].retrieved?.map((s) => s.marker), [1, 2]);
  assert.equal(next[1].streaming, true);
});

test("a done frame keeps the cited sources", () => {
  let msgs = applyAnswerFrame(streaming(), { type: "sources", sources: [src(1, "a"), src(2, "b")] });
  msgs[1] = { ...msgs[1], content: "Yes. [2][7]" };
  msgs = applyAnswerFrame(msgs, { type: "done", sources: [src(2, "b")] });
  assert.equal(msgs[1].streaming, false);
  assert.deepEqual(msgs[1].sources?.map((s) => s.marker), [2]);
  assert.equal(answerText(msgs[1]), "Yes. [2]", "the rejected [7] is dropped once done");
});

test("an unsourced done frame keeps the streamed text and flags it", () => {
  let msgs = streaming();
  msgs[1] = { ...msgs[1], content: "Yes, Pro includes FIPS." };
  msgs = applyAnswerFrame(msgs, { type: "done", unsourced: true });
  assert.equal(msgs[1].unsourced, true);
  assert.equal(msgs[1].content, "Yes, Pro includes FIPS.");
  assert.deepEqual(msgs[1].sources, []);
});

test("legacy S-prefixed markers and code are handled", () => {
  const m: Message = {
    role: "assistant",
    content: "One [S1, S2]. Code `x[1]` stays.",
    think: "",
    streaming: true,
    retrieved: [src(1, "a"), src(2, "b")],
  };
  assert.equal(answerText(m), "One [1][2]. Code `x[1]` stays.");
});

test("list-form markers normalize to chips while streaming", () => {
  const m: Message = {
    role: "assistant",
    content: "One [1, 2].",
    think: "",
    streaming: true,
    retrieved: [src(1, "a"), src(2, "b")],
  };
  assert.equal(answerText(m), "One [1][2].");
});

test("markers in a turn with nothing retrieved are dropped", () => {
  // The model copied a marker although no sources frame arrived: it refers to
  // nothing, so neither a chip nor literal "[1]" text is shown.
  const streamingTurn: Message = { role: "assistant", content: "Kafka is a broker. [1]", think: "", streaming: true };
  assert.equal(answerText(streamingTurn), "Kafka is a broker.");
  const doneTurn: Message = { ...streamingTurn, streaming: false, sources: [] };
  assert.equal(answerText(doneTurn), "Kafka is a broker.");
});
