import type { BatchDomain } from "./api/answer";

// This module is the TypeScript half of the batch domain resolver. The run is
// resolved in Go (cmd/cli/basic/chat/domains.go); this copy exists only so the
// pre-run preview can say which questions each entry would apply to. The two are
// held together by testdata/domain_resolution.yaml, a shared fixture both test
// suites assert against — and by the preview being advisory: the domain recorded
// on a result is what actually ran.

// DomainError is a rejected `domains` block. `entry` is the 0-based index of the
// offending entry, so a caller can point at the right line; it is -1 when the
// fault is not attributable to one entry.
export class DomainError extends Error {
  readonly entry: number;

  constructor(message: string, entry: number) {
    super(message);
    this.name = "DomainError";
    this.entry = entry;
  }
}

// CompiledDomain pairs a manifest entry with what resolution needs on every
// lookup: the uppercased pattern (matching is case-insensitive) as a compiled
// matcher, and its literal, non-wildcard character count, which is the
// specificity metric.
export interface CompiledDomain {
  domain: BatchDomain;
  pattern: string;
  literals: number;
  matcher: RegExp;
}

// compileDomains validates a manifest's domains list and compiles it for lookup,
// mirroring chat.CompileDomains: an entry with no match pattern, an entry
// carrying neither context nor keywords, a malformed glob, and two entries
// declaring the same pattern are all rejected, the last so the pattern recorded
// on a result identifies exactly one entry. Patterns differing only in case are
// duplicates, since matching is case-insensitive.
//
// A missing or empty list compiles to an empty set, which resolves everything to
// no domain. Messages are worded as the daemon words them, so an operator who
// bypasses the UI and posts the same manifest reads the same rejection.
export function compileDomains(domains: BatchDomain[] | undefined): CompiledDomain[] {
  if (!domains || domains.length === 0) return [];

  const compiled: CompiledDomain[] = [];
  const seen = new Map<string, number>();

  domains.forEach((raw, i) => {
    const match = (raw.match ?? "").trim();
    if (match === "") {
      throw new DomainError(`domains entry ${i + 1} has an empty match pattern`, i);
    }
    if (!(raw.context ?? "").trim() && (raw.keywords ?? []).length === 0) {
      throw new DomainError(
        `domains entry ${i + 1} (match "${match}") has neither context nor keywords`,
        i
      );
    }

    const pattern = match.toUpperCase();
    const matcher = globRegExp(pattern);
    if (!matcher) {
      throw new DomainError(`domains entry ${i + 1} has an invalid match pattern "${match}"`, i);
    }
    const first = seen.get(pattern);
    if (first !== undefined) {
      throw new DomainError(
        `domains entries ${first + 1} and ${i + 1} declare the same match pattern "${match}"`,
        i
      );
    }
    seen.set(pattern, i);

    compiled.push({
      domain: { ...raw, match },
      pattern,
      literals: literalCount(pattern),
      matcher,
    });
  });

  return compiled;
}

// resolveDomain returns the entry that applies to a question, or null if none
// does.
//
// The question's id is matched first. Where more than one entry matches, the one
// with the most literal characters in its pattern wins ("J1.*" beats "J*"), ties
// going to the earlier entry in document order. When the id is a bare sequence
// number — which carries no domain prefix — and matched nothing, the question's
// source is tried instead. An id that matched is never re-matched against its
// source.
export function resolveDomain(
  compiled: CompiledDomain[],
  id: string,
  source: string
): BatchDomain | null {
  if (compiled.length === 0) return null;
  const byID = matchKey(compiled, id);
  if (byID) return byID;
  if (source !== "" && isBareSequence(id)) return matchKey(compiled, source);
  return null;
}

// matchKey returns the most specific entry matching key, or null.
function matchKey(compiled: CompiledDomain[], key: string): BatchDomain | null {
  // Both sides are uppercased, as the Go resolver does, so a pattern and an id
  // that differ only in case match.
  const upper = key.toUpperCase();
  let best = -1;
  for (let i = 0; i < compiled.length; i++) {
    if (!compiled[i].matcher.test(upper)) continue;
    // Strictly greater, so a tie leaves the earlier entry in place.
    if (best < 0 || compiled[i].literals > compiled[best].literals) best = i;
  }
  return best < 0 ? null : compiled[best].domain;
}

// literalCount counts the characters in a glob that are neither of the wildcards
// the schema commits to, "*" and "?". It is the specificity metric for picking
// among matching patterns, so operators do not have to keep the list in any
// particular order.
export function literalCount(pattern: string): number {
  let n = 0;
  for (const c of pattern) {
    if (c !== "*" && c !== "?") n++;
  }
  return n;
}

// isBareSequence reports whether s is a non-empty run of ASCII digits — the shape
// the extractor assigns as an id when a row carries no identifier of its own, and
// the only case where source is consulted for routing.
export function isBareSequence(s: string): boolean {
  return /^[0-9]+$/.test(s);
}

// globRegExp translates a glob into an anchored regular expression, or returns
// null when the glob is malformed. It reproduces Go's path.Match, which is what
// resolves the run: "*" and "?" do not cross "/", a backslash escapes the next
// character, and "[...]" is a character class (negated with a leading "^"). The
// schema commits only to "*" and "?", but the rest is translated rather than
// taken literally, because a preview that quietly reads a pattern differently
// from the run is worse than no preview.
function globRegExp(pattern: string): RegExp | null {
  let out = "";
  let i = 0;
  while (i < pattern.length) {
    const c = pattern[i];
    if (c === "*") {
      out += "[^/]*";
      i++;
    } else if (c === "?") {
      out += "[^/]";
      i++;
    } else if (c === "\\") {
      // A trailing backslash has nothing to escape: path.Match calls that a bad
      // pattern.
      if (i + 1 >= pattern.length) return null;
      out += escapeRegExp(pattern[i + 1]);
      i += 2;
    } else if (c === "[") {
      const cls = charClass(pattern, i);
      if (!cls) return null;
      out += cls.source;
      i = cls.next;
    } else {
      out += escapeRegExp(c);
      i++;
    }
  }
  return new RegExp(`^${out}$`);
}

// charClass translates the "[...]" class starting at pattern[start], returning
// its regular-expression form and the index just past it, or null when the class
// is malformed. path.Match rejects an unterminated class and an empty one, and
// treats "]" or "-" where a class character belongs as an error too.
function charClass(pattern: string, start: number): { source: string; next: number } | null {
  let i = start + 1;
  let negated = false;
  if (pattern[i] === "^") {
    negated = true;
    i++;
  }
  let body = "";
  let members = 0;
  while (i < pattern.length && pattern[i] !== "]") {
    const lo = classChar(pattern, i);
    if (!lo) return null;
    i = lo.next;
    if (pattern[i] === "-") {
      const hi = classChar(pattern, i + 1);
      if (!hi) return null;
      body += `${escapeRegExp(lo.char)}-${escapeRegExp(hi.char)}`;
      i = hi.next;
    } else {
      body += escapeRegExp(lo.char);
    }
    members++;
  }
  // Unterminated, or "[]" / "[^]" with no members.
  if (i >= pattern.length || members === 0) return null;
  return { source: `[${negated ? "^" : ""}${body}]`, next: i + 1 };
}

// classChar reads one character of a class, honouring a backslash escape. "-" and
// "]" cannot start a class member, matching path.Match's getEsc.
function classChar(pattern: string, i: number): { char: string; next: number } | null {
  const c = pattern[i];
  if (c === undefined || c === "-" || c === "]") return null;
  if (c === "\\") {
    if (i + 1 >= pattern.length) return null;
    return { char: pattern[i + 1], next: i + 2 };
  }
  return { char: c, next: i + 1 };
}

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\/-]/g, "\\$&");
}