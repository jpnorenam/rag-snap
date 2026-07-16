"use client";

import { Component, type ReactNode } from "react";

interface Props {
  children: ReactNode;
  // Rendered instead of the children when a descendant render throws. Receives
  // the error message and a reset callback that clears the boundary so the
  // subtree can re-mount (e.g. after the user retries).
  fallback: (error: string, reset: () => void) => ReactNode;
  // Optional side-effect when an error is caught (e.g. surface a notification).
  onError?: (error: string) => void;
}

interface State {
  error: string | null;
}

// ErrorBoundary catches render/lifecycle errors in its subtree so a malformed
// response or an unexpected shape degrades to a handled fallback instead of
// taking down the whole page (React unmounts the entire tree on an uncaught
// render error). Must be a class — React exposes no hook for this.
export default class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(err: unknown): State {
    return { error: err instanceof Error ? err.message : String(err) };
  }

  componentDidCatch(err: unknown) {
    const message = err instanceof Error ? err.message : String(err);
    this.props.onError?.(message);
  }

  private reset = () => this.setState({ error: null });

  render() {
    if (this.state.error !== null) {
      return this.props.fallback(this.state.error, this.reset);
    }
    return this.props.children;
  }
}
