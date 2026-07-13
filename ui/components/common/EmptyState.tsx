"use client";

interface Props {
  // One-line headline stating what's missing ("No operations yet").
  headline: string;
  // One sentence of guidance; per foundation §7 it should include the
  // CLI-equivalent command the user could run instead.
  guidance: React.ReactNode;
  // Optional primary action (a single button/link element).
  action?: React.ReactNode;
  className?: string;
}

// EmptyState is the shared "no data yet" pattern (foundation §7): a muted icon,
// a headline, one sentence of guidance (including the CLI equivalent), and an
// optional primary action. Empty is not error — styling stays neutral.
export default function EmptyState({ headline, guidance, action, className }: Props) {
  const classes = ["app-empty", className].filter(Boolean).join(" ");
  return (
    <div className={classes}>
      <i className="app-empty__icon" aria-hidden="true">
        <svg
          width="32"
          height="32"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth={1.5}
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M3 7l9-4 9 4v10l-9 4-9-4z" />
          <path d="M3 7l9 4 9-4M12 11v10" />
        </svg>
      </i>
      <p className="app-empty__headline u-no-margin--bottom">{headline}</p>
      <p className="app-empty__guidance u-text--muted p-text--small">{guidance}</p>
      {action && <div className="app-empty__action">{action}</div>}
    </div>
  );
}
