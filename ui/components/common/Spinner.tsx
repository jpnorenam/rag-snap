"use client";

interface Props {
  // The visible label shown next to the spinner ("Loading knowledge bases…").
  label: string;
  className?: string;
}

// Spinner is the shared busy indicator: Vanilla's spinner icon plus a visible
// text label (the icon alone conveys nothing to a screen reader).
export default function Spinner({ label, className }: Props) {
  const classes = ["app-spinner", className].filter(Boolean).join(" ");
  return (
    <p className={classes}>
      <i className="p-icon--spinner u-animation--spin" aria-hidden="true" />
      <span>{label}</span>
    </p>
  );
}
