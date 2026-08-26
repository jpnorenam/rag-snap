import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { parse } from "yaml";
import { compileDomains, DomainError, isBareSequence, literalCount, resolveDomain } from "./domains";
import type { BatchDomain } from "./api/answer";

// The fixture is located relative to this file, not the working directory, so the
// suite runs the same from ui/ and from the repo root.
const FIXTURE = fileURLToPath(new URL("../../testdata/domain_resolution.yaml", import.meta.url));

interface FixtureCase {
  name: string;
  id: string;
  source: string;
  want: string;
}

interface FixtureGroup {
  name: string;
  domains: BatchDomain[];
  cases: FixtureCase[];
}

function fixtureGroups(): FixtureGroup[] {
  const doc = parse(readFileSync(FIXTURE, "utf8")) as { groups?: FixtureGroup[] };
  const groups = doc.groups ?? [];
  assert.ok(groups.length > 0, "the shared resolution fixture has no groups");
  return groups;
}

// This is the guard on the two resolvers agreeing: the Go suite
// (cmd/cli/basic/chat/domains_fixture_test.go) asserts the same file, so a change
// to specificity or the source fallback made on one side only fails on the other.
test("resolves the shared Go/TS fixture", () => {
  for (const group of fixtureGroups()) {
    const compiled = compileDomains(group.domains);
    assert.ok(group.cases.length > 0, `fixture group ${group.name} has no cases`);
    for (const c of group.cases) {
      const got = resolveDomain(compiled, c.id, c.source);
      const where = `${group.name} / ${c.name}: resolve(${JSON.stringify(c.id)}, ${JSON.stringify(c.source)})`;
      if (c.want === "") {
        assert.equal(got, null, `${where} should resolve to no domain`);
      } else {
        assert.equal(got?.match, c.want, where);
      }
    }
  }
});

test("compiles an absent or empty table to an empty set", () => {
  assert.deepEqual(compileDomains(undefined), []);
  assert.deepEqual(compileDomains([]), []);
  assert.equal(resolveDomain([], "C4", "C"), null);
});

test("trims the match pattern", () => {
  const compiled = compileDomains([{ match: "  C*  ", context: "EPA" }]);
  assert.equal(compiled[0].pattern, "C*");
  // The trimmed form is what a result would record, so it is what the entry
  // carries afterwards.
  assert.equal(resolveDomain(compiled, "C4", "")?.match, "C*");
});

test("rejects an entry with no match pattern", () => {
  assert.throws(
    () => compileDomains([{ match: "   ", context: "EPA" }]),
    (e: unknown) =>
      e instanceof DomainError && e.entry === 0 && /entry 1 has an empty match pattern/.test(e.message)
  );
});

test("rejects an entry with neither context nor keywords", () => {
  for (const entry of [{ match: "C*" }, { match: "C*", context: "   ", keywords: [] }]) {
    assert.throws(
      () => compileDomains([{ match: "J*", context: "Hardware" }, entry]),
      (e: unknown) =>
        e instanceof DomainError &&
        e.entry === 1 &&
        /entry 2 \(match "C\*"\) has neither context nor keywords/.test(e.message)
    );
  }
});

test("rejects duplicate match patterns, including case-only duplicates", () => {
  assert.throws(
    () =>
      compileDomains([
        { match: "gis*", context: "Security baseline" },
        { match: "GIS*", context: "Something else" },
      ]),
    (e: unknown) =>
      e instanceof DomainError &&
      e.entry === 1 &&
      /entries 1 and 2 declare the same match pattern "GIS\*"/.test(e.message)
  );
});

test("rejects a malformed glob", () => {
  for (const match of ["C[", "C[]", "C[^]", "C[a-", "C\\"]) {
    assert.throws(
      () => compileDomains([{ match, context: "EPA" }]),
      (e: unknown) => e instanceof DomainError && /invalid match pattern/.test(e.message),
      `pattern ${JSON.stringify(match)} should be rejected`
    );
  }
});

// Character classes and escapes are not part of what the manifest schema promises,
// but Go's path.Match accepts them, so the preview has to read them the same way
// rather than treating the punctuation as literal.
test("matches character classes and escapes as path.Match does", () => {
  const compiled = compileDomains([
    { match: "C[1-3]", context: "class" },
    { match: "D[^1-3]", context: "negated class" },
    { match: "E\\*", context: "escaped star" },
  ]);
  assert.equal(resolveDomain(compiled, "C2", "")?.match, "C[1-3]");
  assert.equal(resolveDomain(compiled, "C9", ""), null);
  assert.equal(resolveDomain(compiled, "D9", "")?.match, "D[^1-3]");
  assert.equal(resolveDomain(compiled, "D2", ""), null);
  // An escaped "*" is a literal asterisk, not a wildcard.
  assert.equal(resolveDomain(compiled, "E*", "")?.match, "E\\*");
  assert.equal(resolveDomain(compiled, "E4", ""), null);
});

test("wildcards do not cross a slash", () => {
  const compiled = compileDomains([{ match: "A*", context: "prefix" }]);
  assert.equal(resolveDomain(compiled, "A1", "")?.match, "A*");
  assert.equal(resolveDomain(compiled, "A/1", ""), null);
});

test("resolution returns the entry, so context and keywords travel with it", () => {
  const compiled = compileDomains([{ match: "C*", context: "EPA", keywords: ["sriov", "numa"] }]);
  const got = resolveDomain(compiled, "C4", "");
  assert.equal(got?.context, "EPA");
  assert.deepEqual(got?.keywords, ["sriov", "numa"]);
});

test("literalCount ignores the wildcards", () => {
  const cases: [string, number][] = [
    ["*", 0],
    ["?", 0],
    ["C*", 1],
    ["J1.*", 3],
    ["GIS*", 3],
    ["T?.A", 3],
    ["EXACT", 5],
  ];
  for (const [pattern, want] of cases) {
    assert.equal(literalCount(pattern), want, pattern);
  }
});

test("isBareSequence accepts only a run of digits", () => {
  for (const s of ["17", "0"]) assert.equal(isBareSequence(s), true, s);
  for (const s of ["", "C4", "17a", "1.2", " 17", "-1"]) assert.equal(isBareSequence(s), false, s);
});