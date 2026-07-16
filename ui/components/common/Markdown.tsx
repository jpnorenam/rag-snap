"use client";

import { createElement, type ReactNode } from "react";

// Markdown is a small, dependency-free renderer for the subset of Markdown the
// chat model emits: headings, bold/italic/strikethrough, inline code, fenced
// code blocks, links, blockquotes, ordered/unordered lists, GFM pipe tables,
// and horizontal rules. The UI conventions forbid adding UI dependencies (no
// react-markdown), so this is hand-rolled.
//
// Everything is rendered as real React nodes — never dangerouslySetInnerHTML —
// so all text is escaped by React and there is no HTML-injection surface. Link
// hrefs are still sanitized to http(s)/mailto/relative to block javascript:
// URLs. Partial input (a half-streamed table or an unclosed code fence) degrades
// to literal text or a best-effort block rather than throwing.

interface Props {
  content: string;
  className?: string;
}

export default function Markdown({ content, className }: Props) {
  const classes = ["md", className].filter(Boolean).join(" ");
  return <div className={classes}>{parseBlocks(content)}</div>;
}

// ---------------------------------------------------------------------------
// Block parsing
// ---------------------------------------------------------------------------

function parseBlocks(input: string): ReactNode[] {
  const lines = input.replace(/\r\n?/g, "\n").split("\n");
  const blocks: ReactNode[] = [];
  let i = 0;
  let key = 0;

  while (i < lines.length) {
    const line = lines[i];

    // Blank lines separate blocks.
    if (line.trim() === "") {
      i++;
      continue;
    }

    // Fenced code block: ``` … ``` (language after the fence is ignored). An
    // unclosed fence (still streaming) runs to the end of the input.
    if (/^\s*```/.test(line)) {
      const code: string[] = [];
      i++;
      while (i < lines.length && !/^\s*```/.test(lines[i])) {
        code.push(lines[i]);
        i++;
      }
      if (i < lines.length) i++; // consume the closing fence
      blocks.push(
        <pre key={key++} className="md__pre">
          <code>{code.join("\n")}</code>
        </pre>
      );
      continue;
    }

    // ATX heading: #… ######.
    const heading = line.match(/^(#{1,6})\s+(.*)$/);
    if (heading) {
      const level = heading[1].length;
      blocks.push(
        createElement(
          `h${level}`,
          { key: key++, className: "md__heading" },
          ...parseInline(heading[2].replace(/\s+#+\s*$/, ""))
        )
      );
      i++;
      continue;
    }

    // Horizontal rule.
    if (/^(-{3,}|\*{3,}|_{3,})$/.test(line.trim())) {
      blocks.push(<hr key={key++} className="md__hr" />);
      i++;
      continue;
    }

    // GFM pipe table: a header row followed by a |---|---| separator row.
    if (line.includes("|") && i + 1 < lines.length && isTableSeparator(lines[i + 1])) {
      const header = splitTableRow(line);
      i += 2; // header + separator
      const rows: string[][] = [];
      while (i < lines.length && lines[i].includes("|") && lines[i].trim() !== "") {
        rows.push(splitTableRow(lines[i]));
        i++;
      }
      blocks.push(
        <div key={key++} className="md__table-wrap">
          <table className="md__table">
            <thead>
              <tr>
                {header.map((cell, c) => (
                  <th key={c}>{parseInline(cell)}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row, r) => (
                <tr key={r}>
                  {header.map((_, c) => (
                    <td key={c}>{parseInline(row[c] ?? "")}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
      continue;
    }

    // Blockquote: one or more consecutive `>` lines.
    if (/^\s*>\s?/.test(line)) {
      const quoted: string[] = [];
      while (i < lines.length && /^\s*>\s?/.test(lines[i])) {
        quoted.push(lines[i].replace(/^\s*>\s?/, ""));
        i++;
      }
      blocks.push(
        <blockquote key={key++} className="md__quote">
          {parseBlocks(quoted.join("\n"))}
        </blockquote>
      );
      continue;
    }

    // Unordered list: -, *, or + markers.
    if (/^\s*[-*+]\s+/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^\s*[-*+]\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^\s*[-*+]\s+/, ""));
        i++;
      }
      blocks.push(
        <ul key={key++} className="md__list">
          {items.map((it, n) => (
            <li key={n}>{parseInline(it)}</li>
          ))}
        </ul>
      );
      continue;
    }

    // Ordered list: 1. 2. …
    if (/^\s*\d+\.\s+/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^\s*\d+\.\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^\s*\d+\.\s+/, ""));
        i++;
      }
      blocks.push(
        <ol key={key++} className="md__list">
          {items.map((it, n) => (
            <li key={n}>{parseInline(it)}</li>
          ))}
        </ol>
      );
      continue;
    }

    // Paragraph: gather consecutive lines until a blank line or a new block
    // starter. Soft line breaks join with a space (CommonMark).
    const para: string[] = [];
    while (i < lines.length && lines[i].trim() !== "" && !startsNewBlock(lines, i)) {
      para.push(lines[i]);
      i++;
    }
    blocks.push(
      <p key={key++} className="md__p">
        {parseInline(para.join(" "))}
      </p>
    );
  }

  return blocks;
}

// startsNewBlock reports whether the line at i opens a block that must not be
// folded into the current paragraph.
function startsNewBlock(lines: string[], i: number): boolean {
  const line = lines[i];
  return (
    /^\s*```/.test(line) ||
    /^(#{1,6})\s+/.test(line) ||
    /^\s*>\s?/.test(line) ||
    /^\s*[-*+]\s+/.test(line) ||
    /^\s*\d+\.\s+/.test(line) ||
    /^(-{3,}|\*{3,}|_{3,})$/.test(line.trim()) ||
    (line.includes("|") && i + 1 < lines.length && isTableSeparator(lines[i + 1]))
  );
}

function isTableSeparator(line: string): boolean {
  const cells = splitTableRow(line);
  if (cells.length === 0) return false;
  return cells.every((c) => /^:?-+:?$/.test(c.trim()));
}

function splitTableRow(line: string): string[] {
  return line
    .trim()
    .replace(/^\|/, "")
    .replace(/\|$/, "")
    .split("|")
    .map((c) => c.trim());
}

// ---------------------------------------------------------------------------
// Inline parsing
// ---------------------------------------------------------------------------

// Each matcher finds one inline span. They are tried in order; the earliest
// match in the string wins, and ties go to the matcher listed first (so bold's
// ** beats italic's * at the same position). Inner text is parsed recursively,
// except inside code where markers are literal.
interface InlineMatch {
  index: number;
  length: number;
  render: (key: number) => ReactNode;
}

function parseInline(text: string): ReactNode[] {
  const out: ReactNode[] = [];
  let rest = text;
  let key = 0;
  let guard = 0;

  while (rest.length > 0 && guard++ < 5000) {
    const match = firstInlineMatch(rest);
    if (!match) {
      out.push(rest);
      break;
    }
    if (match.index > 0) out.push(rest.slice(0, match.index));
    out.push(match.render(key++));
    rest = rest.slice(match.index + match.length);
  }

  return out;
}

function firstInlineMatch(text: string): InlineMatch | null {
  const matchers: Array<{ re: RegExp; make: (m: RegExpExecArray, key: number) => ReactNode }> = [
    {
      // Inline code: backticks; content is literal (no nested parsing).
      re: /`([^`]+)`/,
      make: (m, key) => (
        <code key={key} className="md__code">
          {m[1]}
        </code>
      ),
    },
    {
      // Link: [text](href).
      re: /\[([^\]]+)\]\(([^)\s]+)\)/,
      make: (m, key) => {
        const href = sanitizeHref(m[2]);
        if (!href) return <span key={key}>{m[0]}</span>;
        return (
          <a key={key} href={href} target="_blank" rel="noreferrer noopener">
            {parseInline(m[1])}
          </a>
        );
      },
    },
    {
      re: /\*\*([^]+?)\*\*/,
      make: (m, key) => <strong key={key}>{parseInline(m[1])}</strong>,
    },
    {
      re: /__([^]+?)__/,
      make: (m, key) => <strong key={key}>{parseInline(m[1])}</strong>,
    },
    {
      re: /~~([^]+?)~~/,
      make: (m, key) => <del key={key}>{parseInline(m[1])}</del>,
    },
    {
      // Italic uses *…* only. Underscore-italic is deliberately not supported:
      // this is a technical tool and _ appears inside snake_case identifiers.
      re: /\*([^*\n]+?)\*/,
      make: (m, key) => <em key={key}>{parseInline(m[1])}</em>,
    },
  ];

  let best: InlineMatch | null = null;
  for (const { re, make } of matchers) {
    const m = re.exec(text);
    if (m && (best === null || m.index < best.index)) {
      best = { index: m.index, length: m[0].length, render: (key) => make(m, key) };
    }
  }
  return best;
}

// sanitizeHref allows only http(s), mailto, and same-origin/relative/anchor
// links; anything else (javascript:, data:, …) returns null so the link falls
// back to literal text.
function sanitizeHref(href: string): string | null {
  return /^(https?:\/\/|mailto:|\/|#)/i.test(href.trim()) ? href.trim() : null;
}
