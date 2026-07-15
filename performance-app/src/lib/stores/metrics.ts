import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import { writable } from "svelte/store";

export interface UsageMetric {
  percent: number | null;
  used_bytes: number | null;
  total_bytes: number | null;
  temp_celsius: number | null;
}

export interface MetricsSnapshot {
  cpu: UsageMetric;
  ram: UsageMetric;
  disk: UsageMetric;
  gpu: UsageMetric;
  timestamp_ms: number;
}

const EMPTY_METRIC: UsageMetric = {
  percent: null,
  used_bytes: null,
  total_bytes: null,
  temp_celsius: null,
};

export const EMPTY_SNAPSHOT: MetricsSnapshot = {
  cpu: EMPTY_METRIC,
  ram: EMPTY_METRIC,
  disk: EMPTY_METRIC,
  gpu: EMPTY_METRIC,
  timestamp_ms: 0,
};

export const metrics = writable<MetricsSnapshot>(EMPTY_SNAPSHOT);

/**
 * Fetches the current snapshot for an instant first paint, then subscribes to
 * the backend's live event stream. Returns a cleanup function to unsubscribe.
 */
export async function startMetricsStream(): Promise<() => void> {
  try {
    const initial = await invoke<MetricsSnapshot>("get_current_metrics");
    metrics.set(initial);
  } catch (err) {
    console.error("failed to fetch initial metrics", err);
  }

  let unlisten: UnlistenFn | null = await listen<MetricsSnapshot>(
    "metrics://update",
    (event) => metrics.set(event.payload),
  );

  return () => {
    unlisten?.();
    unlisten = null;
  };
}
