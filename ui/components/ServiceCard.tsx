"use client";

import type { ServiceName, ServiceStatus } from "@/lib/api/status";

interface Props {
  name: ServiceName;
  title: string;
  // One-line description of the service's job, so the card is readable by someone
  // who does not already know what "Tika" is.
  role: string;
  // The CLI diagnostic to try when this service is unreachable.
  hint: string;
  status: ServiceStatus;
}

// STATE_WORDS gives each state its visible word. The word is always rendered:
// the dot's color never carries the meaning on its own (docs/ux/06, foundation).
const STATE_WORDS: Record<ServiceStatus["state"], string> = {
  running: "Running",
  unreachable: "Unreachable",
  "not configured": "Not configured",
};

// STATE_DOT maps a service state to the shared status dot's state class. Service
// health has its own modifiers: the dot's existing `is-running` means an operation
// in flight (caution), which is not what a healthy service is.
const STATE_DOT: Record<ServiceStatus["state"], string> = {
  running: "is-reachable",
  unreachable: "is-unreachable",
  "not configured": "is-unconfigured",
};

// ServiceCard renders one service's health and detail. Cards degrade
// independently: everything below the state line is optional, so an unreachable
// service still shows its endpoint and the hint for fixing it.
export default function ServiceCard({ name, title, role, hint, status }: Props) {
  return (
    <li className="status-card">
      <div className="status-card__head">
        <h3 className="status-card__title">{title}</h3>
        <span className="status-card__state">
          <span className={`app-status-dot ${STATE_DOT[status.state]}`} aria-hidden="true" />
          {STATE_WORDS[status.state]}
        </span>
      </div>

      <p className="status-card__role p-text--small u-text--muted">{role}</p>

      {status.endpoint && (
        <div className="status-card__endpoint">
          <div className="p-code-snippet u-no-margin--bottom">
            <pre className="p-code-snippet__block" aria-label={`${title} endpoint`}>
              <code>{status.endpoint}</code>
            </pre>
          </div>
        </div>
      )}

      {status.state === "not configured" && (
        <p className="status-card__hint p-text--small u-text--muted">
          No endpoint is configured for this service. Set it in Configuration below.
        </p>
      )}

      {status.state === "unreachable" && (
        <p className="status-card__hint p-text--small">
          {status.error ? `${status.error}. ` : ""}
          {hint}
        </p>
      )}

      {name === "opensearch" && <OpenSearchDetail status={status} />}

      {name === "inference" && status.llm_model && (
        <DetailLine label="Model" value={status.llm_model} />
      )}

      {name === "tika" && status.version && <DetailLine label="Version" value={status.version} />}

      {name === "ragd" && (
        <>
          {status.api_version && <DetailLine label="API version" value={status.api_version} />}
          {status.listeners?.socket && <DetailLine label="Socket" value={status.listeners.socket} />}
          {status.listeners?.loopback && (
            <DetailLine label="Loopback" value={status.listeners.loopback} />
          )}
        </>
      )}
    </li>
  );
}

// OpenSearchDetail shows the configured model IDs (copyable, as the CLI prints
// them for `k init`) and what OpenSearch actually has deployed. A configured
// model that is not deployed breaks retrieval silently, so it is called out.
function OpenSearchDetail({ status }: { status: ServiceStatus }) {
  const models = status.models ?? [];
  const deployed = status.deployed_models ?? [];

  if (models.length === 0 && deployed.length === 0) return null;

  return (
    <div className="status-card__detail">
      {models.length > 0 && (
        <>
          <h4 className="status-card__detail-label">Configured models</h4>
          {models.map((model) => (
            <div key={model.role} className="status-card__model">
              <span className="p-text--small u-text--muted">{model.role}</span>
              <div className="p-code-snippet u-no-margin--bottom">
                <pre className="p-code-snippet__block" aria-label={`${model.role} model ID`}>
                  <code>{model.id}</code>
                </pre>
              </div>
              {status.state === "running" && !model.deployed && (
                <p className="status-card__warning p-text--small">
                  This model is not deployed in OpenSearch. Retrieval will fail until it is —
                  run <code>rag-cli.rag knowledge init</code>.
                </p>
              )}
            </div>
          ))}
        </>
      )}

      {deployed.length > 0 && (
        <>
          <h4 className="status-card__detail-label">Deployed models</h4>
          <ul className="status-card__models">
            {deployed.map((model) => (
              <li key={model.id} className="p-text--small">
                <span className="status-card__model-name">{model.name}</span>{" "}
                <span className="u-text--muted">
                  {model.algorithm} · v{model.model_version}
                </span>
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
}

// DetailLine is one label/value pair in a card's detail area.
function DetailLine({ label, value }: { label: string; value: string }) {
  return (
    <p className="status-card__line p-text--small">
      <span className="u-text--muted">{label}: </span>
      {value}
    </p>
  );
}
