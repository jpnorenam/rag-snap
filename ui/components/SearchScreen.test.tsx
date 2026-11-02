import test from "node:test";
import assert from "node:assert/strict";
import { renderToStaticMarkup } from "react-dom/server";
import { SearchCitation } from "./SearchScreen";
import type { SearchResult } from "@/lib/api/search";

const hit: SearchResult = {
  score: 0.9,
  base: "docs",
  source_id: "pro-guide",
  created_at: "2026-10-01",
  label: "canonical",
  content: "Ubuntu Pro includes FIPS.",
};

test("a linked citation renders its label as an external link", () => {
  const html = renderToStaticMarkup(
    <SearchCitation
      result={{ ...hit, citation: { key: "pro-guide", label: "Ubuntu Pro Guide", href: "https://ubuntu.com/pro" } }}
    />
  );
  assert.equal(html, '<a href="https://ubuntu.com/pro" target="_blank" rel="noopener noreferrer">Ubuntu Pro Guide</a>');
});

test("a citation without a link renders its label as plain text", () => {
  const html = renderToStaticMarkup(
    <SearchCitation result={{ ...hit, citation: { key: "pro-guide", label: "Ubuntu Pro Guide" } }} />
  );
  assert.equal(html, "Ubuntu Pro Guide");
});

test("a hit from a daemon without citations falls back to the source ID", () => {
  assert.equal(renderToStaticMarkup(<SearchCitation result={hit} />), "pro-guide");
});
