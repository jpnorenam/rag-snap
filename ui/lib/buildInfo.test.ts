import { strict as assert } from "node:assert";
import test from "node:test";
import { formatBuild } from "./buildInfo";

test("snap version and commit", () => {
  assert.equal(formatBuild("0.0.9", "cd3ac8925556"), "v0.0.9 · cd3ac8925556");
});

test("dirty commit is shown as is", () => {
  assert.equal(formatBuild("0.0.9", "cd3ac8925556-dirty"), "v0.0.9 · cd3ac8925556-dirty");
});

test("dev build gets no v prefix", () => {
  assert.equal(formatBuild("dev", "cd3ac8925556"), "dev · cd3ac8925556");
});

test("unknown commit is left out", () => {
  assert.equal(formatBuild("0.0.9", undefined), "v0.0.9");
});

test("nothing known", () => {
  assert.equal(formatBuild(undefined, undefined), "");
});
