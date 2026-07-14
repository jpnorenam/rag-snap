import { getSync } from "./envelope";

// ServiceState is the reachability of one service. "not configured" is distinct
// from "unreachable": nothing is wrong, there is simply no endpoint to reach.
export type ServiceState = "running" | "unreachable" | "not configured";

// ServiceName keys the status payload. The order the page renders them in is
// fixed by SERVICE_ORDER in StatusScreen, not by the object's key order.
export type ServiceName = "opensearch" | "inference" | "tika" | "ragd";

// DeployedModel is an ML model OpenSearch currently has deployed, and can
// therefore serve. Distinct from a *configured* model ID: a configured model
// that is not in this list is the failure the status page exists to surface.
export interface DeployedModel {
  id: string;
  name: string;
  algorithm: string;
  model_version: string;
  model_group_id: string;
}

// ConfiguredModel is a model ID from config, paired with whether OpenSearch has
// it deployed.
export interface ConfiguredModel {
  role: string;
  id: string;
  name: string;
  deployed: boolean;
}

// Listeners are the daemon's own listening surfaces. The localhost token is
// deliberately not part of this payload.
export interface Listeners {
  socket?: string;
  loopback?: string;
}

// ServiceStatus is one service's entry. Detail fields are optional: the daemon
// omits what a degraded service could not report, and each card renders from
// whatever it did get.
export interface ServiceStatus {
  state: ServiceState;
  endpoint?: string;
  error?: string;

  // OpenSearch detail.
  models?: ConfiguredModel[];
  deployed_models?: DeployedModel[];

  // Inference detail.
  llm_model?: string;

  // Tika detail.
  version?: string;

  // ragd detail.
  api_version?: string;
  listeners?: Listeners;
}

// StatusPayload is the metadata of GET /1.0/status, keyed by service.
export type StatusPayload = Record<ServiceName, ServiceStatus>;

// getStatus probes the services and returns their current state. The daemon runs
// the probes at request time, so this is a live answer, not a cached one — which
// is why the page fetches it on mount and on Refresh, and never polls.
export function getStatus(): Promise<StatusPayload> {
  return getSync<StatusPayload>("/1.0/status");
}
