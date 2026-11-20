"use client";

interface Props {
  warnings: string[];
}

// RunWarnings shows the problems a batch run hit without failing, such as
// kapa.ai being unavailable so answers came from local knowledge only. A
// caution notice rather than an error: the run still produced answers, but
// they may be missing grounding the manifest asked for.
export default function RunWarnings({ warnings }: Props) {
  if (warnings.length === 0) return null;
  return (
    <div className="p-notification--caution" role="status">
      <div className="p-notification__content">
        <h3 className="p-notification__title">
          {warnings.length === 1 ? "This run has a warning" : `This run has ${warnings.length} warnings`}
        </h3>
        {warnings.length === 1 ? (
          <p className="p-notification__message">{warnings[0]}</p>
        ) : (
          <ul className="p-notification__message run-warnings__list">
            {warnings.map((w, i) => (
              <li key={i}>{w}</li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
