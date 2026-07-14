"use client";

interface Props {
  // Visible label shown next to the spinner (e.g. "Loading…").
  label: string;
  className?: string;
}

// Spinner is the sanctioned loading affordance: Vanilla's spinner icon plus a
// visible text label (foundation §6/§7). The icon is decorative; the label
// carries the meaning for assistive tech.
export default function Spinner({ label, className }: Props) {
  const classes = ["app-spinner", className].filter(Boolean).join(" ");
  return (
    <span className={classes}>
      <i className="p-icon--spinner u-animation--spin" aria-hidden="true" />
      <span>{label}</span>
    </span>
  );
}
