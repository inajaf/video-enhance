<script lang="ts">
  import Gauge from "./Gauge.svelte";
  import type { UsageMetric } from "./stores/metrics";

  export let label: string;
  export let icon: string;
  export let metric: UsageMetric;
  export let showBytes = false;

  function fmtBytes(n: number | null): string {
    if (n === null) return "--";
    const gb = n / 1024 ** 3;
    return gb >= 1 ? `${gb.toFixed(1)} GB` : `${(n / 1024 ** 2).toFixed(0)} MB`;
  }
</script>

<div class="card">
  <div class="card-header">
    <span class="card-icon">{icon}</span>
    <span class="card-label">{label}</span>
  </div>

  <div class="gauge-wrap">
    <Gauge value={metric.percent} />
    <div class="gauge-center">
      {#if metric.percent !== null}
        <span class="value">{Math.round(metric.percent)}<small>%</small></span>
      {:else}
        <span class="value muted">--</span>
      {/if}
    </div>
  </div>

  <div class="card-meta">
    {#if metric.temp_celsius !== null}
      <span class="pill">{Math.round(metric.temp_celsius)}°C</span>
    {/if}
    {#if showBytes && metric.used_bytes !== null && metric.total_bytes !== null}
      <span class="pill">{fmtBytes(metric.used_bytes)} / {fmtBytes(metric.total_bytes)}</span>
    {/if}
    {#if metric.percent === null}
      <span class="pill unavailable">Unavailable</span>
    {/if}
  </div>
</div>

<style>
  .card {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    padding: 14px 10px;
    background: var(--surface);
    backdrop-filter: var(--glass);
    -webkit-backdrop-filter: var(--glass);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    box-shadow: var(--shadow);
  }

  .card-header {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    font-weight: 600;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .card-icon {
    font-size: 13px;
  }

  .gauge-wrap {
    position: relative;
    display: grid;
    place-items: center;
  }

  .gauge-center {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
  }

  .value {
    font-size: 20px;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
  }

  .value small {
    font-size: 11px;
    font-weight: 600;
    color: var(--muted);
    margin-left: 1px;
  }

  .value.muted {
    color: var(--muted);
  }

  .card-meta {
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    gap: 6px;
    min-height: 20px;
  }

  .pill {
    font-size: 10px;
    font-weight: 600;
    color: var(--muted);
    background: var(--surface-strong);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 2px 8px;
    white-space: nowrap;
  }

  .pill.unavailable {
    color: var(--muted);
    font-style: italic;
  }
</style>
