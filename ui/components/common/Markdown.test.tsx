import test from "node:test";
import assert from "node:assert/strict";
import { renderToStaticMarkup } from "react-dom/server";
import Markdown from "./Markdown";
import { citationMap } from "./SourcesList";
import type { CitedSource } from "@/lib/types";

const linked: CitedSource = {
  marker: 1,
  key: "pro-guide",
  label: "Ubuntu Pro Guide",
  href: "https://ubuntu.com/pro",
  base: "docs",
  tag: "canonical",
};
const unlinked: CitedSource = { marker: 3, key: "faq.md", label: "faq.md", base: "docs", tag: "canonical" };

test("known markers render as source chips named by their label", () => {
  const html = renderToStaticMarkup(
    <Markdown content="Pro includes FIPS. [1][3]" citations={citationMap([linked, unlinked])} />
  );
  assert.equal((html.match(/md__cite/g) ?? []).length, 2, "two chips");
  assert.match(html, /Source 1: Ubuntu Pro Guide/);
  assert.match(html, /Source 3: faq\.md/);
  assert.ok(!html.includes("[1]"), "a known marker is not left as literal text");
});

test("a linked source's chip opens its link in a new tab", () => {
  const html = renderToStaticMarkup(<Markdown content="Yes. [1]" citations={citationMap([linked])} />);
  assert.match(html, /<a class="p-chip md__cite" href="https:\/\/ubuntu\.com\/pro" target="_blank" rel="noopener noreferrer"/);
});

test("a source without a link renders a non-interactive chip", () => {
  const html = renderToStaticMarkup(<Markdown content="Yes. [3]" citations={citationMap([unlinked])} />);
  assert.match(html, /<span class="p-chip md__cite" title="faq\.md">/);
  assert.ok(!/<a[^>]*md__cite/.test(html));
});

test("an unknown marker is not rendered as a chip", () => {
  const html = renderToStaticMarkup(<Markdown content="Yes. [7]" citations={citationMap([linked])} />);
  assert.ok(!html.includes("md__cite"));
  assert.match(html, /\[7\]/);
});

test("a marker next to (…) is a chip, not a link", () => {
  const html = renderToStaticMarkup(
    <Markdown content="See [1](https://evil.example) here." citations={citationMap([linked])} />
  );
  assert.match(html, /md__cite/);
  assert.ok(!html.includes('href="https://evil.example"'), "the marker must not become a link");
});

test("without citations, markers stay literal", () => {
  const html = renderToStaticMarkup(<Markdown content="Yes. [1]" />);
  assert.ok(!html.includes("md__cite"));
});

test("bracketed numbers in code or glued to a word are not chips", () => {
  const html = renderToStaticMarkup(
    <Markdown content="Use `arr[1]` and items[1]. [1]" citations={citationMap([linked])} />
  );
  assert.equal((html.match(/md__cite/g) ?? []).length, 1, "only the standalone marker is a chip");
  assert.match(html, /arr\[1\]/);
  assert.match(html, /items\[1\]/);
});
