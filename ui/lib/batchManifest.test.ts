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
      branch: "release-1.0",
      path: "docs/how-to/",
      extensions: [".md", ".rst"],
      label: "docs",
    },
    {
      name: "",
      type: "gitea-repo",
      source: "https://gitea.example.com/owner/repo",
      branch: "",
      path: "",
      extensions: [".go"],
      label: "",
    },
    {
      name: "faq: common questions", // colon forces quoting
      type: "url",
      source: "https://example.com/faq#anchor", // # must survive quoting
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
    { name: "", type: "url", source: "https://a.example", branch: "", path: "", extensions: [], label: "" },
  ]);
  assert.equal(yaml.includes("branch"), false);
  assert.equal(yaml.includes("extensions"), false);
  assert.equal(yaml.includes("label"), false);
  assert.equal(yaml.includes("name"), false);
});

test("builderJobToBatchItem maps repo jobs and rejects file jobs", () => {
  const repo = builderJobToBatchItem({
    name: "n",
    type: "github-repo",
    source: "o/r",
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
    branch: "",
    path: "",
    extensions: [],
    label: "",
  });
  assert.deepEqual(url, { type: "url", url: "https://a.example", source_id: undefined, label: undefined });

  assert.equal(
    builderJobToBatchItem({ name: "", type: "file", source: "/tmp/x.pdf", branch: "", path: "", extensions: [], label: "" }),
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
