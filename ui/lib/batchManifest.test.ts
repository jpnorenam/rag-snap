import { test } from "node:test";
import assert from "node:assert/strict";
import {
  parseBatchManifest,
  serializeBatchManifest,
  builderJobToBatchItem,
  type BuilderJob,
} from "./batchManifest";

// A documented `k ingest --batch` manifest exercising every job type and field.
const CLI_BATCH_MANIFEST = `version: "1"
jobs:
  - name: ubuntu-docs
    type: github-repo
    source: canonical/ubuntu-docs
    branch: main
    path: docs/
    extensions: [.md, .rst]
    label: docs
  - type: gitea-repo
    source: "https://opendev.org/openstack/nova"
    extensions: [.rst]
  - name: pricing
    type: url
    source: "https://ubuntu.com/pricing"
    label: sales
  - type: file
    source: /home/user/report.pdf
`;

test("parses all job types with their fields, including label", () => {
  const { items, preview, jobs, error } = parseBatchManifest(CLI_BATCH_MANIFEST);
  assert.equal(error, undefined);
  assert.equal(jobs.length, 4);
  assert.equal(preview.length, 4);
  // file jobs are previewed but excluded from the API items.
  assert.equal(items.length, 3);
  assert.equal(preview[3].unsupported !== undefined, true);

  assert.deepEqual(jobs[0], {
    name: "ubuntu-docs",
    type: "github-repo",
    source: "canonical/ubuntu-docs",
    targetKB: "",
    branch: "main",
    path: "docs/",
    extensions: [".md", ".rst"],
    label: "docs",
  });
  assert.equal(items[0].type, "github");
  assert.equal(items[0].label, "docs");
  assert.equal(items[2].type, "url");
  assert.equal(items[2].label, "sales");
});

test("parse(serialize(jobs)) round-trips builder jobs exactly", () => {
  const jobs: BuilderJob[] = [
    {
      name: "docs",
      type: "github-repo",
      source: "owner/repo",
      targetKB: "",
      branch: "release-1.0",
      path: "docs/how-to/",
      extensions: [".md", ".rst"],
      label: "docs",
    },
    {
      name: "",
      type: "gitea-repo",
      source: "https://gitea.example.com/owner/repo",
      targetKB: "",
      branch: "",
      path: "",
      extensions: [".go"],
      label: "",
    },
    {
      name: "faq: common questions", // colon forces quoting
      type: "url",
      source: "https://example.com/faq#anchor", // # must survive quoting
      targetKB: "",
      branch: "",
      path: "",
      extensions: [],
      label: "faq",
    },
  ];
  const { jobs: reparsed, error } = parseBatchManifest(serializeBatchManifest(jobs));
  assert.equal(error, undefined);
  assert.deepEqual(reparsed, jobs);
});

test("round-trips a parsed CLI manifest through the builder model", () => {
  const first = parseBatchManifest(CLI_BATCH_MANIFEST);
  const second = parseBatchManifest(serializeBatchManifest(first.jobs));
  assert.deepEqual(second.jobs, first.jobs);
  assert.deepEqual(second.items, first.items);
});

test("serializes empty optionals by omission", () => {
  const yaml = serializeBatchManifest([
    { name: "", type: "url", source: "https://a.example", targetKB: "", branch: "", path: "", extensions: [], label: "" },
  ]);
  assert.equal(yaml.includes("branch"), false);
  assert.equal(yaml.includes("extensions"), false);
  assert.equal(yaml.includes("label"), false);
  assert.equal(yaml.includes("name"), false);
  assert.equal(yaml.includes("target_kb"), false);
});

test("builderJobToBatchItem maps repo jobs and rejects file jobs", () => {
  const repo = builderJobToBatchItem({
    name: "n",
    type: "github-repo",
    source: "o/r",
    targetKB: "",
    branch: "b",
    path: "p/",
    extensions: [".md"],
    label: "l",
  });
  assert.deepEqual(repo, {
    type: "github",
    source: "o/r",
    source_id: "n",
    branch: "b",
    path: "p/",
    extensions: [".md"],
    label: "l",
  });

  const url = builderJobToBatchItem({
    name: "",
    type: "url",
    source: "https://a.example",
    targetKB: "",
    branch: "",
    path: "",
    extensions: [],
    label: "",
  });
  assert.deepEqual(url, { type: "url", url: "https://a.example", source_id: undefined, label: undefined });

  assert.equal(
    builderJobToBatchItem({ name: "", type: "file", source: "/tmp/x.pdf", targetKB: "", branch: "", path: "", extensions: [], label: "" }),
    null
  );
});

test("rejects a manifest without jobs", () => {
  const { error } = parseBatchManifest('version: "1"\n');
  assert.match(error ?? "", /No jobs found/);
});

test("round-trips values whose YAML form needs escaping", () => {
  // yamlScalar (shared with lib/manifest.ts) escapes quotes and backslashes in
  // the double-quoted form; stripQuotes has to undo them or these values come
  // back with their escapes still in place.
  const jobs = [
    {
      name: 'a "quoted" name',
      type: "url" as const,
      source: "https://example.com/a?q=1",
      targetKB: "",
      branch: "",
      path: "docs\\windows",
      extensions: [],
      label: "yes",
    },
  ];
  const { jobs: reparsed, error } = parseBatchManifest(serializeBatchManifest(jobs));
  assert.equal(error, undefined);
  assert.deepEqual(reparsed, jobs);
});

// --- target_kb: a documented per-job field this API cannot route on ----------
// The manifest schema routes each job to its own knowledge base; a batch ingest
// here posts to the one base the screen selected. The field therefore has to be
// read, kept, and reported — never silently dropped, and never silently obeyed by
// sending the job to the wrong base.

const TARGETED_MANIFEST = `version: "1"
jobs:
  - name: docs
    type: github-repo
    source: owner/repo
    target_kb: other-base
  - name: pricing
    type: url
    source: "https://example.com/pricing"
`;

test("target_kb is parsed and survives a serialize round trip", () => {
  const { jobs, error } = parseBatchManifest(TARGETED_MANIFEST);
  assert.equal(error, undefined);
  assert.equal(jobs[0].targetKB, "other-base");
  assert.equal(jobs[1].targetKB, "");

  // Exporting an imported manifest must not strip the operator's routing.
  const yaml = serializeBatchManifest(jobs);
  assert.match(yaml, /^ {4}target_kb: other-base$/m);
  assert.deepEqual(parseBatchManifest(yaml).jobs, jobs);
});

test("a target_kb naming another base holds the job back from the run", () => {
  const { items, preview } = parseBatchManifest(TARGETED_MANIFEST, "telco");
  // Running it would load "telco" with a job written for "other-base".
  assert.equal(preview[0].unsupported !== undefined, true);
  assert.match(preview[0].unsupported ?? "", /other-base/);
  assert.match(preview[0].unsupported ?? "", /telco/);
  assert.match(preview[0].unsupported ?? "", /k ingest --batch/);
  // Only the untargeted job is offered to the API.
  assert.equal(items.length, 1);
  assert.equal(preview[1].unsupported, undefined);
  assert.equal(preview[1].warning, undefined);
});

test("a target_kb differing only in case is the same base", () => {
  // Index names are lower-cased (knowledge.FullIndexName), so "Other-Base" and
  // "other-base" are one base; treating the case as a mismatch would hold back a
  // job that in fact targets the selected base.
  const { items, preview } = parseBatchManifest(
    TARGETED_MANIFEST.replace("target_kb: other-base", "target_kb: Other-Base"),
    "other-base"
  );
  assert.equal(preview[0].unsupported, undefined);
  assert.equal(preview[0].warning, undefined);
  assert.equal(items.length, 2);
});

test("target_kb is reported as unhonourable when no destination is known", () => {
  // With no base to compare against the field still cannot take effect, so the
  // reader says so rather than dropping it in silence.
  const { items, preview } = parseBatchManifest(TARGETED_MANIFEST);
  assert.equal(preview[0].unsupported, undefined);
  assert.match(preview[0].warning ?? "", /target_kb/);
  assert.equal(items.length, 2);
});

test("an unrecognised job field is reported, not dropped", () => {
  // An unmodelled key is something the operator wrote and expects to take
  // effect; the default branch used to swallow it, so the run ignored the intent
  // with nothing said.
  const { items, preview } = parseBatchManifest(`version: "1"
jobs:
  - name: docs
    type: github-repo
    source: owner/repo
    future_field: something this reader does not model
`);
  assert.equal(items.length, 1);
  assert.match(preview[0].warning ?? "", /future_field/);
});
