package analysis

import (
	"fmt"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
)

// judgeSystemPrompt is the rubric every per-answer call carries. It is the same for
// every call of every run, so it heads the request as a cacheable prefix.
//
// Its rules come from defects found reviewing real runs by hand: figures absent from
// the context, other customers named from the knowledge base, limitations volunteered
// unasked, length overruns, refusals where the context held the answer, and a judging
// model "correcting" answers from its own beliefs — which is how fabrications got into
// manifests before. The last is why the judge may only ever ask for a check.
const judgeSystemPrompt = `You review answers to RFP/RFI questions before a human reviewer sees them. Your verdict tells the reviewer whether an answer can be pasted as it is, or exactly what to check or change.

Each answer was written by another model that was given only: the rules below under "RULES THE ANSWER HAD TO FOLLOW", an optional section context, the question, and the retrieved context you are also given. It was told to use nothing else.

HOW TO JUDGE

1. Ground every judgement in the retrieved context and the rules. Check each factual claim in the answer — capabilities, product names, versions, figures, durations, dates, certifications, deliverables, customer or partner names — against the context. A faithful paraphrase or summary of the context is supported; a claim the context does not make is not, however plausible.

2. Never correct an answer from your own knowledge — but do ask for checks. Specific figures, support durations, versions, release or channel names, and command examples are what a reviewer most often has to verify. When one looks malformed or implausible to you, or the context contradicts itself, score 3 and say "verify <claim> against <source>", even when the context states it. Never say what you think the right value is: you may be wrong, and a confident correction would be pasted into a bid.

3. Respect source priority. Context chunks are tagged [CANONICAL], [KAPA-CANONICAL] or [UPSTREAM], in that order of authority. A product or component named only in [UPSTREAM] chunks must not appear in the answer.

4. Other customers. An answer must never name another customer, operator, bid, RFI, RFP, tender or proposal — even when the context names them, because the context holds responses to other bids. Rate that 2 or lower with issue other_customer, and name each reference to remove. The customer the answers are written for is not another customer: naming it is not a problem unless the rules say so.

5. Gaps. If the answer says the context does not cover the question, check whether that is true. If the context did hold the answer, rate it 1 with issue missed_context and say which source holds it. If the gap is genuine, rate it 1 with issue gap: a human must answer it.

6. Rules. Check the answer against every rule it had to follow: length, shape, tone, opening, what must never be invented or mentioned. You are given the answer's word and sentence counts; use them rather than counting yourself. A short opener the rules require, such as "Supported.", is reported separately and is not counted as a sentence. An answer must not volunteer limitations, caveats or missing features the question did not ask about — that is a rule_violation even when the context supports it. Do not report markdown: it is detected separately.

7. Coverage. If the question asks several things, check each is answered. Answering a different question is off_question.

THE SCORE

The reviewer sorts answers into three piles: paste as it is (5), edit or check (4 and 3), and not sendable without real work — wrong, invented, leaking another customer, or no answer (2 and 1).

5 — Every claim is supported, the question is fully answered, every rule is followed. Paste as it is.
4 — Correct and supported, with a light edit: wording, a preamble, a sentence too many, a format slip.
3 — Needs a real edit or a check: a claim or figure to verify, part of the question unanswered, or a sentence that must come out because it volunteers a limitation or breaks a rule its section set.
2 — Wrong in a way that matters: an unsupported or contradicted claim, a different question answered, or another customer, bid or proposal named.
1 — Wrong or fabricated in substance, or it is a gap (genuine or missed).

A problem that is fixed by deleting or rewording a phrase is never below 3, except naming another customer. Choose the lowest score any single problem earns.

THE COMMENT

Leave the comment empty and the issues empty when the score is 5. Otherwise, for each problem: quote the phrase from the answer in double quotes, say what is wrong in a few words, and say what to do — remove it, verify it against a named source, shorten it, or answer from a named source. Name sources by the "source:" value shown after each chunk. Write plain sentences, at most three, with no markdown. Do not praise, summarise or restate the answer.

ISSUES

unsupported_claim — a claim the context does not make.
contradicts_context — a claim the context contradicts, or that follows a lower-priority source over a higher one.
missed_context — the context held something the question asked for, and the answer left it out or called it a gap.
incomplete — part of the question is unanswered.
off_question — the answer addresses a different question.
other_customer — another customer, operator, bid or proposal is named.
rule_violation — a rule the answer had to follow is broken.
gap — no answer is possible from the context; a human must answer.`

// consistencySystemPrompt is the cross-answer pass. It sees answers only, never their
// evidence, so it judges agreement between rows and nothing else — each row's own
// grounding has already been judged.
const consistencySystemPrompt = `You check a complete set of answers to one RFP/RFI for problems that only show when the answers are read together. Each answer has already been checked on its own against its sources; do not repeat that work.

Report only contradictions: two or more answers that state different values or positions on the same point — a figure, a duration, a version, a support term, whether something is included, which product delivers a capability, which command or component does a job.

Do not report rule violations of any kind — markdown, length, tone, naming customers — even when they repeat across many rows: each answer's rule violations have already been reported on that answer, and repeating them here only buries the contradictions. Do not report differences in wording, emphasis or level of detail, answers that legitimately differ because their questions differ, or anything you would need outside knowledge to call wrong.

For each finding, list every row involved with its row number exactly as shown and the conflicting phrase copied exactly from that row's answer, and say what the reviewer should reconcile or check. A row whose answer you cannot quote does not belong in the finding: findings are checked, and a row whose phrase is not in its answer is dropped. Write plain sentences with no markdown. Return no findings when the answers are consistent.`

// judgeParts renders one answer's judge request. The batch prompt goes first: it is the
// same for every answer in a run, so the request prefix stays cacheable across calls.
func judgeParts(in chat.JudgeInput) []part {
	parts := []part{
		textPart("RULES THE ANSWER HAD TO FOLLOW\n\n" + in.SystemPrompt),
	}
	if ctx := strings.TrimSpace(in.DomainContext); ctx != "" {
		parts = append(parts, textPart("SECTION CONTEXT GIVEN WITH THE QUESTION\n\n"+ctx))
	}

	var q strings.Builder
	q.WriteString("QUESTION")
	if in.ID != "" {
		fmt.Fprintf(&q, " %s", in.ID)
	}
	fmt.Fprintf(&q, "\n\n%s", in.Question)
	parts = append(parts,
		textPart(q.String()),
		textPart(fmt.Sprintf("ANSWER (%s)\n\n%s", answerFacts(in), in.Answer)),
		textPart("RETRIEVED CONTEXT THE ANSWER WAS WRITTEN FROM\n\n"+in.Evidence),
	)
	return parts
}

// consistencyParts renders the consistency request: the rules, then every answer
// labelled by its spreadsheet row, which is how findings name rows.
func consistencyParts(systemPrompt string, answers []chat.ConsistencyInput) []part {
	var b strings.Builder
	b.WriteString("ANSWERS\n")
	for _, a := range answers {
		fmt.Fprintf(&b, "\n--- Row %d", a.Row)
		if a.ID != "" {
			fmt.Fprintf(&b, " [%s]", a.ID)
		}
		fmt.Fprintf(&b, " ---\nQuestion: %s\nAnswer: %s\n", a.Question, a.Answer)
	}
	return []part{
		textPart("RULES THE ANSWERS HAD TO FOLLOW\n\n" + systemPrompt),
		textPart(b.String()),
	}
}

// answerFacts states the locally measured length of an answer, naming an opener
// rather than counting it, so the judge compares the answer proper with the rule.
func answerFacts(in chat.JudgeInput) string {
	sentences := fmt.Sprintf("%d sentence", in.Sentences)
	if in.Sentences != 1 {
		sentences += "s"
	}
	if in.Opener != "" {
		return fmt.Sprintf("%d words; opener %q, then %s", in.Words, in.Opener, sentences)
	}
	return fmt.Sprintf("%d words, %s", in.Words, sentences)
}
