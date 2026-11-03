import test from "node:test";
import assert from "node:assert/strict";
import { renderToStaticMarkup } from "react-dom/server";
import SheetScope, { scopeNeedsAnswer, scopeRequest } from "./SheetScope";
import { TriageStep } from "./AnalysisSteps";
import type { SheetInfo, Triage } from "@/lib/api/answer";

// Static markup, as for the other answer components: the control is driven by props,
// so the markup for each state is the behaviour under test.

const tabs: SheetInfo[] = [
  { position: 1, name: "Read Me", data_rows: 3 },
  { position: 2, name: "1 Hardware", data_rows: 146 },
  { position: 3, name: "App B Sizing & BoQ", data_rows: 22 },
];

const noop = () => {};

test("every tab is listed and set to Answer by default", () => {
  const html = renderToStaticMarkup(
    <SheetScope filename="rfp.xlsx" state={{ status: "loaded", sheets: tabs }} scope={{}} onChange={noop} />
  );
  for (const t of tabs) assert.ok(html.includes(t.name.replace("&", "&amp;")), `missing ${t.name}`);
  assert.equal((html.match(/checked="" value="answer"/g) ?? []).length, 3);
  assert.match(html, /role="radiogroup" aria-label="Use of tab 2, 1 Hardware"/);
  assert.ok(!html.includes('role="alert"'), "a scope with tabs to answer reports no problem");
});

test("a chosen setting is shown, and an excluded row is marked", () => {
  const html = renderToStaticMarkup(
    <SheetScope
      filename="rfp.xlsx"
      state={{ status: "loaded", sheets: tabs }}
      scope={{ 1: "context", 3: "exclude" }}
      onChange={noop}
    />
  );
  assert.match(html, /name="sheet-scope-1" checked="" value="context"/);
  assert.match(html, /sheet-scope__row sheet-scope__row--exclude/);
});

test("nothing left to answer is reported at the control", () => {
  const scope = { 1: "context", 2: "exclude", 3: "exclude" } as const;
  assert.ok(scopeNeedsAnswer(tabs, scope));
  const html = renderToStaticMarkup(
    <SheetScope filename="rfp.xlsx" state={{ status: "loaded", sheets: tabs }} scope={scope} onChange={noop} />
  );
  assert.match(html, /role="alert"[^>]*>Set at least one tab to Answer/);
  assert.match(html, /p-form-validation is-error/);
});

test("a document without tabs renders nothing", () => {
  assert.equal(renderToStaticMarkup(<SheetScope filename="rfp.pdf" state={{ status: "none" }} scope={{}} onChange={noop} />), "");
});

test("loading and failure states", () => {
  assert.match(
    renderToStaticMarkup(<SheetScope filename="rfp.xlsx" state={{ status: "loading" }} scope={{}} onChange={noop} />),
    /Reading the tabs of rfp\.xlsx/
  );
  const html = renderToStaticMarkup(
    <SheetScope filename="rfp.xlsx" state={{ status: "error", message: "tika is down." }} scope={{}} onChange={noop} />
  );
  assert.match(html, /tika is down\./);
  assert.match(html, /Analysis can still run, over every tab/);
});

test("the request names every tab, or nothing when all are answered", () => {
  assert.equal(scopeRequest(tabs, {}), undefined);
  assert.equal(scopeRequest(tabs, { 2: "answer" }), undefined);
  assert.deepEqual(scopeRequest(tabs, { 1: "context", 3: "exclude" }), { "1": "context", "2": "answer", "3": "exclude" });
});

const triage: Triage = {
  sections: [],
  estimated_questions: 40,
  openstack_in_scope: false,
  multi_field_answers: false,
  suggested_knowledge_bases: [],
  suggested_kapa_source_groups: [],
  report: [],
  model: "m",
};

test("the triage step shows the scope the session works to", () => {
  const sheets: SheetInfo[] = [
    { ...tabs[0], scope: "context" },
    { ...tabs[1], scope: "answer" },
    { ...tabs[2], scope: "exclude" },
  ];
  const html = renderToStaticMarkup(
    <TriageStep triage={triage} expiresAt="" instructions="" sheets={sheets} onBack={noop} onContinue={noop} />
  );
  assert.match(html, /aria-label="Tabs and how each is used"/);
  assert.match(html, /Context only/);
  assert.match(html, /Excluded/);

  const unscoped = renderToStaticMarkup(
    <TriageStep triage={triage} expiresAt="" instructions="" sheets={sheets.map((s) => ({ ...s, scope: "answer" }))} onBack={noop} onContinue={noop} />
  );
  assert.ok(!unscoped.includes("Tabs and how each is used"), "an unscoped session shows no tab table");
});
