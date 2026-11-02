import test from "node:test";
import assert from "node:assert/strict";
import { isHttpUrl } from "./IngestModal";

test("source URLs must be absolute http(s) URLs", () => {
  assert.equal(isHttpUrl("https://example.com/spec.pdf"), true);
  assert.equal(isHttpUrl("http://example.com"), true);
  assert.equal(isHttpUrl("file:///home/me/spec.pdf"), false);
  assert.equal(isHttpUrl("example.com/spec.pdf"), false);
  assert.equal(isHttpUrl(""), false);
});
