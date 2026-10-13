# The answer-batch manifest contract

You are generating a **manifest** for `rag-cli`, a retrieval-augmented generation tool
used to answer RFP/RFI/RFx documents. You have not seen this tool before, so this
document tells you exactly what it does with your output and what a good manifest
looks like. Read all of it before producing anything.

## What happens to your output

The manifest you produce drives a batch run. For **each question** in it, rag-cli:

1. Rewrites the question into a search query and merges in any keywords that apply.
2. Retrieves passages from the selected knowledge bases (a local hybrid keyword +
   vector search) and, when selected, from a hosted documentation index.
3. Sends **one** model turn: a system prompt (your `prompt`), plus a user turn
   containing the retrieved passages, the resolved requirement-domain context, and
   the question.
4. Records the answer, and which domain matched.

Three consequences you must design around:

- **Each question is answered in complete isolation.** There is no conversation, no
  memory of earlier questions, and no shared document. A question's own text plus its
  domain context is the *entire* framing the answering model receives. If a question
  reads as a bare label ("SR-IOV", "Cinder Driver Support"), the answering model gets
  only that label — so the `prompt` and the domain `context` have to supply everything
  else.
- **When retrieval finds nothing, rag-cli does not call the model at all.** It emits a
  fixed "not enough information" answer. You cannot cause an ungrounded answer, and you
  must not write a prompt that tries to.
- **Non-negotiable source rules are appended to your prompt automatically.** Do not
  restate them and do not contradict them.

## Output format

Return **JSON** matching the schema you were given. rag-cli converts it to YAML itself,
so do not emit YAML, and do not wrap the JSON in prose or a code fence.

## Field reference

| Field | Required | Meaning |
|---|---|---|
| `version` | yes | Always the string `"1.0"`. |
| `questions` | yes | Every question to be answered. See below. |
| `knowledge_bases` | yes | Which knowledge bases to retrieve from. Choose **only** from the catalogue supplied with your request. |
| `kapa_source_groups` | no | Identifiers of hosted documentation source groups. Choose **only** from the catalogue; these are opaque identifiers and cannot be guessed or invented. |
| `prompt` | yes | One system prompt for the whole RFP. See "Writing the prompt". |
| `domains` | yes when the RFP has sections | Requirement-domain routing. See "Domain routing". |

### `questions[]`

| Field | Required | Meaning |
|---|---|---|
| `id` | yes | A short, routable identifier. Use the document's own where it has one (`C4`, `J1.3`, `3.2.1`); derive one where it does not — see "Identifiers". |
| `question` | yes | **One** self-contained question, readable on its own. See "One question per entry" and "Make each question answerable". |
| `source` | yes when the document has sections | The section, sheet or row the question came from. Always set this when a question was derived from a larger cell or section. |
| `keywords` | no | A few retrieval terms for this specific question. Use sparingly; prefer domain keywords. |

**Extract every question.** A missed requirement is an unanswered requirement in a
submitted bid. Do not sample, summarise, or stop early.

#### One question per entry

An entry is **one** question. A single cell, row, or bullet list frequently contains
several distinct questions, and each becomes its own entry.

This is the most common way an extraction goes wrong. Source documents are often written
as shorthand notes, with several questions packed into one cell and separated by bullet
markers (`_`, `-`, `•`), newlines, semicolons, or simply by a `?` in the middle of the
text. Each of those is a separate thing to answer, and the answering model gets one
question at a time — so a cell left whole produces one crammed answer that addresses the
first clause and neglects the rest.

Split on the question boundaries, not on punctuation you happen to find. Two clauses that
together form one question stay together; two clauses that each demand their own answer
become two entries.

```
Cell:  "_ Hyper-converged Compute & Storage / Dedicated Compute + Storage ?
        _ Compute over-commit levels, general vs tenant-specific ?"

Two entries, not one.
```

If a section header carries no question of its own — it merely labels the cells beneath
it — do not emit an entry for it. A row reading only "References" is not a question.

#### Make each question answerable

The answering model receives a question **in complete isolation**: no document, no
neighbouring rows, no section header. So each `question` must stand on its own.

Where the source is already a well-formed question, keep its wording. Where it is
shorthand, expand it into a complete question that means exactly what the shorthand meant,
supplying the subject the shorthand left implicit:

| Source cell | Entry |
|---|---|
| `_ control over encryption keys.` | "Does the operator retain control over encryption keys, and what key management is available for data at rest and in transit?" |
| `_ Built-In IdP vs Bring-Your-Own IdP ?` | "Does the platform provide a built-in identity provider, and can the operator bring its own instead?" |
| `_ Backup Provider integration support, e.g. Veeam ?` | "Which third-party backup products can be integrated, and through which interfaces?" |

Expanding is not embellishing. Add only what the shorthand already implied and what the
section makes explicit. Never invent a requirement the document does not contain, never
add scope the source did not ask for, and never turn one question into two by inventing a
second clause.

**The exception: a named capability is already a question.** In a compliance matrix whose
rows are single terms — `SR-IOV`, `CPU Pinning`, `Cinder Driver Support` — the term *is*
the requirement, and the implicit question ("is this supported, and by what mechanism?")
is the same for every row. Leave those as they are; the RFP-level prompt handles them.
Tell the two apart by asking whether the cell is *notes* or a *name*: notes carry
multiple clauses, verbs, or a trailing `?`, and need expanding; a bare noun phrase naming
a technology does not.

#### Identifiers

Results are keyed by id, ids must be unique, and domain patterns match against them — so
an id has to be short and routable.

- **The document has ids**: use them verbatim. Never renumber.
- **The document has numbered sections but not questions**: derive from the section and
  suffix each question — section 4 yielding three questions gives `4-a`, `4-b`, `4-c`.
- **The document has only section titles**: assign a short section code yourself and
  suffix it — `R02-a`, `R02-b`, `R02-c` — and put the full section title in `source`.

Do **not** use a section title as an id. A title like `Usability (Customer
Self-Service-Portal)` is long, contains characters that behave oddly in a glob, and gives
every question in that section the same id, which collides. The short code plus `source`
gives you both a routable id and the readable provenance.

### `domains[]`

| Field | Required | Meaning |
|---|---|---|
| `match` | yes | A glob matched against question ids. |
| `context` | usually | Prose injected into the turn for every matching question. |
| `keywords` | no | Retrieval terms every matching question inherits. |

An entry **must** carry `context`, `keywords`, or both. An entry with neither is a
validation error that fails the whole run.

## Domain routing — read this carefully

Domains are how a section of the RFP gets extra context attached to it. This is the
part most likely to go wrong, because a pattern that matches nothing fails **silently**:
the run succeeds and the answers are simply missing their context.

The resolution rules, exactly as rag-cli applies them:

1. The pattern is matched against the question's **`id`**.
2. Matching is **case-insensitive**.
3. Wildcards are `*` (any run of characters) and `?` (one character). A `/` is a
   separator that `*` does not cross.
4. When several patterns match one id, the pattern with the **most literal
   (non-wildcard) characters wins**. `J1.*` beats `J*`. Ties go to the earlier entry, so
   you do not need to order the list.
5. **`source` is consulted only when the id is a bare run of digits** *and* no pattern
   matched the id. This is the rule that catches people out: an id like `1.1` is *not* a
   bare run of digits, so a pattern written against its `source` will never be
   consulted for it. If you want `1.1` routed, write a pattern against the id — `1.*`.
6. Two entries may not declare the same pattern, case-insensitively.

**Write every pattern against the ids you actually emitted.** Before you finish, check
each pattern against your own `questions` list and confirm it matches at least one.

To attach context to a **single** question, use a pattern that matches only that id.

Worked examples:

| Question ids | Correct pattern | Why |
|---|---|---|
| `C1`, `C2`, `C3` | `C*` | Prefix routing on a real id. |
| `J1.1`, `J1.2` and `J2.1` | `J1.*` and `J2.*` | Separate sub-sections; more literals than `J*`, so these win. |
| `1.1`, `1.2`, `2.1` | `1.*`, `2.*` | Not bare digits, so route by id, never by source. |
| `1`, `2`, `3` with `source: Security` | `Security` | Bare digits, so source is consulted. |
| `T1-OVERVIEW` only | `T1-OVERVIEW` | Exact id, one question. |

## Writing the prompt

The `prompt` is one system prompt sent verbatim with every question. It is the single
biggest lever on answer quality. Write it as instructions to the model that will answer,
covering:

- **Who is answering and to whom.** The vendor's role, and that the reader is an
  evaluator scoring a bid.
- **What solution is being proposed.** Name the specific products and versions being
  offered, so every answer describes the same thing. If a technology choice was supplied
  with your request, use exactly that one and do not present the alternative as the
  proposal.
- **What the input looks like.** If questions are terse labels, say so, and say that the
  label is the requirement and must be answered rather than defined. If the source
  document gives each question several fields to fill, say how to produce them.
- **The answer shape.** Prose or bullets, how long, what to lead with, and whether
  markdown is allowed. If the answer is pasted into a spreadsheet cell, markdown and line
  breaks will corrupt it — say so.
- **Specificity.** Name the components, interfaces and configuration surfaces that count
  as evidence. State plainly that versions, dates, figures, customer names, headcounts
  and certifications must never be invented.
- **How to handle third-party products.** Answer at the documented interface rather than
  endorsing a brand that the retrieved context does not cover.

Ground the prompt in what the RFP documents actually say — its evaluation criteria, its
stated environment, its page or length limits, its terminology. A generic prompt wastes
the one lever that matters.

## Rules

- Choose `knowledge_bases` and `kapa_source_groups` **only** from the supplied catalogue.
  A name that is not in it retrieves nothing, silently.
- Extract **every** question. Do not sample, summarise, or merge questions.
- Never invent an id. Use the document's own.
- Do not emit a `domains` entry whose pattern matches none of your questions.
- Return only the JSON object.
