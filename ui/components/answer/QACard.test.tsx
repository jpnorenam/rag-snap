import test from "node:test";
import assert from "node:assert/strict";
import { renderToStaticMarkup } from "react-dom/server";
import QACard from "./QACard";
import type { QAItem } from "@/lib/types";

// These render the card to static markup rather than driving a DOM: the card is
// presentational (no state, no effects), so the markup is the whole behaviour
// under test, and it needs no browser harness in the suite.

const older: QAItem = {
  id: "1",
  question: "What redundancy is provided?",
  answer: "Dual power feeds per rack.",
};

test("a results item carrying no domain renders without one", () => {
  // The regression this guards: every results file written before domain
  // routing existed has no `domain` on any item, and so does any question that
  // matched no entry. Those must render exactly as they did — no placeholder, no
  // empty label, no crash on the absent field.
  const html = renderToStaticMarkup(<QACard item={older} index={0} />);
  assert.match(html, /What redundancy is provided\?/);
  assert.match(html, /Dual power feeds per rack\./);
  assert.ok(!html.includes("qa-card__domain"), "an item with no domain must not render the label");
  assert.ok(!/Domain/.test(html), "an item with no domain must not render the word Domain");
});

test("an empty-string domain is treated as absent", () => {
  // chat.BatchResult omits `domain` when a question resolved to no entry, but a
  // hand-written or older-daemon file may carry "" instead.
  const html = renderToStaticMarkup(<QACard item={{ ...older, domain: "" }} index={0} />);
  assert.ok(!html.includes("qa-card__domain"));
});

test("the applied domain renders when the run recorded one", () => {
  const html = renderToStaticMarkup(
    <QACard item={{ ...older, domain: "C*" }} index={3} />
  );
  assert.match(html, /qa-card__domain/);
  assert.match(html, /Domain <code>C\*<\/code>/);
  // Distinct from the answer text: the domain is its own element, not folded
  // into the answer paragraph.
  assert.ok(!/qa-card__answer[^>]*>[^<]*C\*/.test(html));
  // And outside the collapsible provenance section, which this item has none of.
  assert.ok(!html.includes("<details"));
});

test("an unanswered question still shows its domain", () => {
  // The caution treatment and the domain are independent: a question that
  // produced no answer still routed through an entry, and that is exactly when
  // knowing which entry ran is useful.
  const html = renderToStaticMarkup(
    <QACard item={{ ...older, answer: "", domain: "GIS*" }} index={0} />
  );
  assert.match(html, /qa-card--caution/);
  assert.match(html, /No answer was generated for this question\./);
  assert.match(html, /Domain <code>GIS\*<\/code>/);
});
test("an answer's cited sources are listed by label, linked when they have a link", () => {
  const html = renderToStaticMarkup(
    <QACard
      item={{
        ...older,
        sources: [
          { marker: 1, key: "dc-design", label: "Datacentre design", href: "https://example.com/dc.pdf", base: "infra", tag: "canonical" },
          { marker: 2, key: "power.md", label: "power.md", base: "infra", tag: "canonical" },
        ],
      }}
      index={0}
    />
  );
  assert.match(html, /Sources \(2\)/);
  assert.match(html, /<a href="https:\/\/example\.com\/dc\.pdf"[^>]*>Datacentre design<\/a>/);
  assert.match(html, /<span>power\.md<\/span>/);
  assert.ok(!/\[1\]/.test(html), "batch answers list labels without markers");
});

test("a results item from before citations renders without a sources list", () => {
  const html = renderToStaticMarkup(<QACard item={older} index={0} />);
  assert.ok(!html.includes("Sources"));
});
