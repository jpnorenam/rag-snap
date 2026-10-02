"use client";

import { useCallback, useEffect, useId, useState } from "react";
import { errorMessage } from "@/lib/api/envelope";
import { listKapaSourceGroups, type KapaSourceGroup } from "@/lib/api/kapa";
import EmptyState from "@/components/common/EmptyState";
import Spinner from "@/components/common/Spinner";

interface Props {
  // selected holds the chosen group ids; the picker never changes it on its
  // own, so an existing selection survives a listing that fails.
  selected: string[];
  onChange: (ids: string[]) => void;
  disabled?: boolean;
  // label precedes the chips (the row's own label keeps it distinct from the
  // knowledge-base chips beside it).
  label?: string;
}

type Listing =
  | { state: "loading" }
  | { state: "error"; message: string }
  | { state: "loaded"; configured: boolean; groups: KapaSourceGroup[] };

// KapaSourcePicker selects kapa.ai source groups, listed by name from the
// daemon. It is shared by the answer-batch, chat, and Search screens, and
// distinguishes loading, error, unconfigured (credentials hint, not an error),
// empty, and loaded.
export default function KapaSourcePicker({ selected, onChange, disabled, label = "Kapa.ai source groups:" }: Props) {
  const labelId = useId();
  const [listing, setListing] = useState<Listing>({ state: "loading" });

  const load = useCallback(async () => {
    setListing({ state: "loading" });
    try {
      const { configured, groups } = await listKapaSourceGroups();
      setListing({ state: "loaded", configured, groups });
    } catch (e) {
      setListing({ state: "error", message: errorMessage(e) });
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const toggle = useCallback(
    (id: string) => {
      onChange(selected.includes(id) ? selected.filter((g) => g !== id) : [...selected, id]);
    },
    [selected, onChange]
  );

  const kept =
    selected.length > 0 ? (
      <p className="u-text--muted p-text--small u-no-margin--bottom">
        {`The current selection (${selected.length} group${selected.length === 1 ? "" : "s"}) is kept.`}
      </p>
    ) : null;

  return (
    <div className="kapa-picker">
      <span className="kb-selector__label" id={labelId}>
        {label}
      </span>

      {listing.state === "loading" && <Spinner label="Loading kapa.ai source groups…" />}

      {listing.state === "error" && (
        <div className="p-notification--negative kapa-picker__notice" role="alert">
          <div className="p-notification__content">
            <p className="p-notification__message">
              {`Could not list kapa.ai source groups: ${listing.message}`}
            </p>
            <p className="u-text--muted p-text--small u-no-margin--bottom">
              Check the daemon&apos;s <code>KAPA_API_KEY</code> and <code>kapa.project.id</code>, then retry.
              The same listing is available in the CLI with <code>/use-kapa</code> in <code>rag-cli.rag chat</code>.
            </p>
            {kept}
          </div>
          <div className="p-notification__meta">
            <div className="p-notification__actions">
              <button type="button" className="p-button--base u-no-margin--bottom" onClick={() => void load()}>
                Retry
              </button>
            </div>
          </div>
        </div>
      )}

      {listing.state === "loaded" && !listing.configured && (
        <div className="p-notification--information kapa-picker__notice">
          <div className="p-notification__content">
            <p className="p-notification__message">
              kapa.ai is not configured on the daemon, so only local knowledge bases are used.
              Credentials are required: set the project with{" "}
              <code>sudo rag-cli.rag set --package kapa.project.id=&lt;id&gt;</code>, add{" "}
              <code>KAPA_API_KEY</code> to the ragd systemd drop-in, then run{" "}
              <code>sudo snap restart rag-cli.ragd</code>.
            </p>
            {kept}
          </div>
        </div>
      )}

      {listing.state === "loaded" && listing.configured && listing.groups.length === 0 && (
        <EmptyState
          className="kapa-picker__empty"
          headline="No kapa.ai source groups"
          guidance="This kapa.ai project has no source groups yet. Group its sources in kapa.ai, then reload this page. In the CLI, /use-kapa inside the chat lists the same groups."
          command="rag-cli.rag chat"
        />
      )}

      {listing.state === "loaded" && listing.configured && listing.groups.length > 0 && (
        <div className="kapa-picker__chips" role="group" aria-labelledby={labelId}>
          {listing.groups.map((g) => (
            <ChipToggle
              key={g.id}
              label={g.name}
              pressed={selected.includes(g.id)}
              disabled={disabled}
              onToggle={() => toggle(g.id)}
            />
          ))}
          {/* Ids the manifest selects that the project no longer lists: shown so
              they can be removed rather than silently sent or dropped. */}
          {selected
            .filter((id) => !listing.groups.some((g) => g.id === id))
            .map((id) => (
              <ChipToggle
                key={id}
                label={id}
                title="Not found in this kapa.ai project"
                pressed
                disabled={disabled}
                onToggle={() => toggle(id)}
              />
            ))}
          {selected.length === 0 && (
            <span className="u-text--muted p-text--small">None selected: kapa.ai is not queried.</span>
          )}
        </div>
      )}
    </div>
  );
}

interface ChipToggleProps {
  label: string;
  pressed: boolean;
  onToggle: () => void;
  disabled?: boolean;
  title?: string;
}

function ChipToggle({ label, pressed, onToggle, disabled, title }: ChipToggleProps) {
  const classes = ["p-chip", "u-no-margin--bottom", pressed ? "p-chip--positive" : ""].filter(Boolean).join(" ");
  return (
    <button
      type="button"
      className={classes}
      aria-pressed={pressed}
      title={title}
      disabled={disabled}
      onClick={onToggle}
    >
      <span className="p-chip__value">{label}</span>
    </button>
  );
}
