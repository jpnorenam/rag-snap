"use client";

interface Props {
  // One line stating what is missing ("No knowledge bases yet").
  headline: string;
  // One sentence of guidance. Include the CLI-equivalent command via `command`.
  guidance: string;
  // The CLI command that does the same thing, rendered as a code snippet.
  command?: string;
  // The primary action ("Create knowledge base"), when one applies.
  action?: React.ReactNode;
  className?: string;
}

// EmptyState is the shared "nothing here yet" pattern: a muted icon, what is
// missing, how to fix it (including the CLI equivalent — the CLI is the
// power-user escape hatch), and the primary action. Empty is not an error.
export default function EmptyState({ headline, guidance, command, action, className }: Props) {
  const classes = ["app-empty", className].filter(Boolean).join(" ");
  return (
    <div className={classes}>
      <svg
        className="app-empty__icon"
        width={40}
        height={40}
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth={1.5}
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
      </svg>
      <h2 className="app-empty__headline">{headline}</h2>
      <p className="app-empty__guidance u-text--muted">{guidance}</p>
      {command && (
        <div className="p-code-snippet app-empty__command">
          <pre className="p-code-snippet__block">
            <code>{command}</code>
          </pre>
        </div>
      )}
      {action}
    </div>
  );
}
