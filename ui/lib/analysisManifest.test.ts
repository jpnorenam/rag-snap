import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { parseManifest, serializeManifest } from "./manifest";
import {
  normaliseCheck,
  parseAnalyzeMetadata,
  parseGenerateMetadata,
  defects,
  warnings,
  parseShortfall,
  acceptanceHeaderLine,
  joinPrompt,
  instructionsHeaderLines,
  type BatchManifest,
} from "./api/answer";

// A generated manifest as the daemon publishes it: a long prompt, a populated domains
// block, and questions carrying ids and sources.
function generatedManifest(): BatchManifest {
  return {
    version: "1.0",
    knowledge_bases: ["vendor-rfp", "platform-docs"],
    prompt:
      "You are a solutions engineer completing a compliance matrix.\n\n" +
      "ANSWER LENGTH\nAnswers are at most 120 words. This is a hard limit and must never be exceeded.\n\n" +
      "GAPS ARE REPORTED, NEVER FILLED\nSay that no context is available rather than inventing an answer.\n\n" +
      "BE DIRECT\nNo filler.\n",
    domains: [
      { match: "C*", context: "Section C covers platform capabilities.", keywords: ["SR-IOV", "NUMA"] },
      { match: "GIS*", context: "Reserved for a later revision." },
    ],
    questions: [
      { id: "C1", question: "SR-IOV", source: "C. Section" },
      { id: "C2", question: "CPU pinning", source: "C. Section" },
    ],
  };
}

test("a generated manifest round-trips through the UI serializer", () => {
  const yaml = serializeManifest(generatedManifest(), { domainsStub: false });

  // The prompt has to survive as a readable literal block, not an escaped one-liner:
  // it is the field most likely to be hand-edited after download.
  assert.match(yaml, /prompt: \|/);
  assert.ok(!yaml.includes("\\n"), "the prompt must not be emitted with escaped newlines");

  // No commented-out stub: the domains block here is real, and a stub beneath it would
  // be confusing rather than helpful.
  assert.ok(!yaml.includes("# domains:"), "a generated manifest must carry no domains stub");

  const round = parseManifest(yaml);
  assert.equal(round.questions.length, 2);
  assert.equal(round.questions[0].source, "C. Section", "source must survive: it is a routing key");
  assert.equal(round.domains?.length, 2);
  assert.equal(round.domains?.[0].match, "C*");
  assert.deepEqual(round.domains?.[0].keywords, ["SR-IOV", "NUMA"]);
  assert.ok(round.prompt?.includes("hard limit"), "the length and its mode must survive");
  assert.ok(round.prompt?.includes("no context is available"), "the no-context rule must survive");
});

test("parseAnalyzeMetadata normalises the arrays the daemon omits", () => {
  const meta = parseAnalyzeMetadata({
    analysis_token: "abc123",
    expires_at: "2026-09-16T10:00:00Z",
    model: "vendor/model-1",
    triage: { estimated_questions: 42, openstack_in_scope: true, multi_field_answers: false },
  });

  assert.equal(meta.analysisToken, "abc123");
  assert.equal(meta.triage.estimated_questions, 42);
  assert.equal(meta.triage.openstack_in_scope, true);
  // Arrays default to empty so callers can map over them without guarding.
  assert.deepEqual(meta.triage.sections, []);
  assert.deepEqual(meta.triage.report, []);
  assert.deepEqual(meta.triage.suggested_knowledge_bases, []);
});

test("parseAnalyzeMetadata rejects a response with no session token", () => {
  // Without a token the generation pass has nothing to reference, so this must be a
  // handled error rather than a later failure with a confusing message.
  assert.throws(() => parseAnalyzeMetadata({ triage: {} }), /session token/);
});

test("parseGenerateMetadata rejects a response with no manifest", () => {
  assert.throws(() => parseGenerateMetadata({ check: {} }), /no manifest/);
});

test("parseGenerateMetadata keeps the manifest and its findings", () => {
  const meta = parseGenerateMetadata({
    manifest: generatedManifest(),
    model: "vendor/model-1",
    check: {
      questions: 2,
      domains: 2,
      domain_matches: [
        { match: "C*", count: 2 },
        { match: "GIS*", count: 0 },
      ],
      findings: [{ severity: "warning", code: "domain-matches-nothing", message: "GIS* matches nothing" }],
      references_checked: true,
      kapa_checked: false,
    },
  });

  assert.equal(meta.manifest.questions.length, 2);
  assert.equal(meta.check.domain_matches.length, 2);
  assert.equal(warnings(meta.check).length, 1);
  assert.equal(defects(meta.check).length, 0);
  // kapa was not consulted, which must stay distinguishable from "consulted and empty".
  assert.equal(meta.check.kapa_checked, false);
  assert.equal(meta.check.references_checked, true);
});

test("normaliseCheck defaults a missing check to something renderable", () => {
  const c = normaliseCheck(undefined);
  assert.deepEqual(c.findings, []);
  assert.deepEqual(c.domain_matches, []);
  // Both must default to false: claiming a check ran when it did not would be false
  // reassurance, which is the thing the check exists to remove.
  assert.equal(c.references_checked, false);
  assert.equal(c.kapa_checked, false);
  assert.equal(c.prompt.applicable, false);
});

test("findings split by severity so warnings-only is distinguishable", () => {
  const result = normaliseCheck({
    findings: [
      { severity: "warning", code: "domain-matches-nothing", message: "reserved" },
      { severity: "defect", code: "unknown-knowledge-base", message: "typo" },
    ],
  });
  assert.equal(warnings(result).length, 1);
  assert.equal(defects(result).length, 1);
});

test("a shortfall is read from the failed operation's metadata", () => {
  const sf = parseShortfall({
    shortfall: {
      expected: 220,
      produced: 53,
      finish_reason: "STOP",
      effort: "low",
      next_effort: "medium",
      sections: [{ name: "NFR", id_prefix: "NFR_", estimated: 115, produced: 22 }],
    },
  });
  assert.ok(sf);
  assert.equal(sf.produced, 53);
  assert.equal(sf.next_effort, "medium");
  assert.equal(sf.sections[0].produced, 22);
});

test("a failure without shortfall metadata is not a shortfall", () => {
  // A truncation or an expired session must stay an error with no way to continue.
  assert.equal(parseShortfall(undefined), null);
  assert.equal(parseShortfall({}), null);
  assert.equal(parseShortfall({ shortfall: { produced: 3 } }), null);
});

test("generated metadata carries an accepted shortfall when there was one", () => {
  const meta = parseGenerateMetadata({
    manifest: generatedManifest(),
    shortfall_accepted: { expected: 168, produced: 111 },
  });
  assert.deepEqual(meta.shortfallAccepted, { expected: 168, produced: 111 });
  assert.equal(parseGenerateMetadata({ manifest: generatedManifest() }).shortfallAccepted, null);
});

test("a downloaded manifest records its provenance in header comments and still parses", () => {
  const text = serializeManifest(generatedManifest(), {
    headerComments: ["Generated by rag-cli analysis in the web UI", acceptanceHeaderLine({ expected: 168, produced: 111 })],
  });
  assert.ok(text.startsWith("# Generated by rag-cli analysis in the web UI\n"));
  assert.ok(text.includes("# Question count accepted by the operator below the triage estimate: 111 produced, about 168 estimated\n"));
  assert.equal(parseManifest(text).questions.length, generatedManifest().questions.length);
});

// The acceptance line is written by both the CLI and the UI. They must say the same
// thing, so the UI's text is checked against the Go format string it mirrors.
const WRITE_GO = fileURLToPath(new URL("../../cmd/cli/basic/analysis/write.go", import.meta.url));

test("the acceptance line matches analysis.ShortfallAcceptance.HeaderLine", () => {
  const source = readFileSync(WRITE_GO, "utf8");
  const goFormat = "Question count accepted by the operator below the triage estimate: %d produced, about %d estimated";
  assert.ok(source.includes(goFormat), "the Go acceptance format string changed; update acceptanceHeaderLine to match");
  assert.equal(
    acceptanceHeaderLine({ expected: 168, produced: 111 }),
    goFormat.replace("%d", "111").replace("%d", "168")
  );
});

const PROMPT_GO = fileURLToPath(new URL("../../cmd/cli/basic/analysis/prompt.go", import.meta.url));

test("joinPrompt matches analysis.JoinPrompt", () => {
  // An edited prompt is only safe if it re-joins exactly as the daemon assembled it.
  const source = readFileSync(PROMPT_GO, "utf8");
  assert.ok(
    source.includes('return strings.TrimSpace(model) + "\\n\\n" + appended'),
    "analysis.JoinPrompt changed; update joinPrompt to match"
  );
  assert.equal(joinPrompt("  Model prose.\n ", "ANSWER LENGTH\nTarget about 100 words.\n"), "Model prose.\n\nANSWER LENGTH\nTarget about 100 words.\n");
});

test("instructionsHeaderLines matches analysis.InstructionsHeaderLines", () => {
  const source = readFileSync(WRITE_GO, "utf8");
  assert.ok(source.includes('out := []string{"Operator instructions:"}'), "the Go header heading changed");
  assert.ok(source.includes('out = append(out, "  "+strings.TrimRight(line, " \\t\\r"))'), "the Go header line format changed");
  assert.deepEqual(instructionsHeaderLines("  Split every row.\nOnly Annexure A.   \n"), [
    "Operator instructions:",
    "  Split every row.",
    "  Only Annexure A.",
  ]);
  assert.deepEqual(instructionsHeaderLines("   "), []);
});

test("analysis metadata carries the session's instructions", () => {
  const meta = parseAnalyzeMetadata({ analysis_token: "t", triage: {}, instructions: "Only Annexure A." });
  assert.equal(meta.instructions, "Only Annexure A.");
  assert.equal(parseAnalyzeMetadata({ analysis_token: "t", triage: {} }).instructions, "");
});

test("prompt parts are read together, or not at all", () => {
  const both = parseGenerateMetadata({
    manifest: generatedManifest(),
    prompt_generated: "Model prose.",
    prompt_appended: "ANSWER LENGTH\n",
  });
  assert.equal(both.promptGenerated, "Model prose.");
  assert.equal(both.promptAppended, "ANSWER LENGTH\n");
  // An older daemon sends neither; half a pair is never used.
  for (const meta of [{}, { prompt_generated: "x" }, { prompt_appended: "y" }]) {
    const m = parseGenerateMetadata({ manifest: generatedManifest(), ...meta });
    assert.equal(m.promptGenerated, null);
    assert.equal(m.promptAppended, null);
  }
});

test("an edited prompt downloads as the edit followed by the unchanged rules", () => {
  const appended = "ANSWER LENGTH\nTarget about 100 words.\n\nGAPS ARE REPORTED, NEVER FILLED\nSay so.\n";
  const manifest = { ...generatedManifest(), prompt: joinPrompt("Edited prose for the bid.", appended) };
  const text = serializeManifest(manifest, {
    headerComments: ["Generated by rag-cli analysis in the web UI", ...instructionsHeaderLines("Only Annexure A.")],
  });
  const parsed = parseManifest(text);
  assert.ok(parsed.prompt?.startsWith("Edited prose for the bid.\n\nANSWER LENGTH"));
  assert.ok(parsed.prompt?.includes("GAPS ARE REPORTED, NEVER FILLED"));
  assert.ok(text.includes("# Operator instructions:\n#   Only Annexure A.\n"));
});
