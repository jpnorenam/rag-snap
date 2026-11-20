import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { noMatchesMessage, resultCount, similarNotice, splitResults } from "./searchSplit";

// Shared with the CLI tests (cmd/cli/basic/knowledge/search_split_test.go): the
// UI and the CLI must split and summarize every case identically.
const FIXTURE = fileURLToPath(new URL("../../testdata/search_split_cases.json", import.meta.url));

interface FixtureHit {
  id: string;
  score: number;
  matched?: boolean;
}

interface FixtureCase {
  name: string;
  query: string;
  hits: FixtureHit[];
  matched: string[];
  similar: string[];
  no_matches_message: string | null;
  similar_notice: string | null;
}

const { cases } = JSON.parse(readFileSync(FIXTURE, "utf8")) as { cases: FixtureCase[] };

test("fixture has cases", () => {
  assert.ok(cases.length > 0);
});

for (const c of cases) {
  test(`split: ${c.name}`, () => {
    const { matched, similar } = splitResults(c.hits);
    assert.deepEqual(matched.map((h) => h.id), c.matched);
    assert.deepEqual(similar.map((h) => h.id), c.similar);

    // The page shows the no-matches message only when nothing matches, and the
    // notice only when something is hidden: the same rules as FormatSplit.
    const shownNoMatches = matched.length === 0 ? noMatchesMessage(c.query) : null;
    const shownNotice = similar.length > 0 ? similarNotice(similar.length) : null;
    assert.equal(shownNoMatches, c.no_matches_message);
    assert.equal(shownNotice, c.similar_notice);
  });
}

test("the result count covers what is on screen", () => {
  assert.equal(resultCount(1, 0, false), "1 result");
  assert.equal(resultCount(2, 8, false), "2 results");
  assert.equal(resultCount(0, 10, false), "0 results");
  assert.equal(resultCount(0, 10, true), "10 results (0 matching, 10 similar)");
  assert.equal(resultCount(2, 8, true), "10 results (2 matching, 8 similar)");
  assert.equal(resultCount(3, 0, true), "3 results");
});
