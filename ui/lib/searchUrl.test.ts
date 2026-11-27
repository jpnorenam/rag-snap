import { test } from "node:test";
import assert from "node:assert/strict";
import { DEFAULT_K, parseSearchQuery, searchQuery } from "./searchUrl";
import { toSearchResponse } from "./api/search";

test("round-trips a search scope with kapa.ai groups through the URL", () => {
  const scope = { q: "commission servers", bases: ["maas", "lxd"], k: 15, kapaGroups: ["g-maas", "g-juju"] };
  const back = parseSearchQuery(new URLSearchParams(searchQuery(scope)));
  assert.deepEqual(back, scope);
});

test("a URL without g selects no kapa.ai groups", () => {
  const back = parseSearchQuery(new URLSearchParams("q=x&b=maas&k=10"));
  assert.deepEqual(back.kapaGroups, []);
  assert.equal(back.k, 10);
});

test("an unknown k falls back to the default", () => {
  assert.equal(parseSearchQuery(new URLSearchParams("q=x&k=7")).k, DEFAULT_K);
});

test("search responses normalize null hits and warnings", () => {
  assert.deepEqual(toSearchResponse(null, null), { results: [], warnings: [] });
  const hit = { score: 1, base: "kapa.ai", source_id: "https://a", created_at: "", label: "kapa-canonical", content: "c" };
  assert.deepEqual(toSearchResponse([hit], ["kapa.ai search failed"]), {
    results: [hit],
    warnings: ["kapa.ai search failed"],
  });
});
