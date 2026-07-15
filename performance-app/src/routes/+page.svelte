<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import MetricCard from "$lib/MetricCard.svelte";
  import { metrics, startMetricsStream } from "$lib/stores/metrics";
  import "$lib/styles/tokens.css";
  import "$lib/styles/app.css";

  let stop: (() => void) | null = null;

  onMount(async () => {
    stop = await startMetricsStream();
  });

  onDestroy(() => {
    stop?.();
  });

  $: lastUpdated = $metrics.timestamp_ms
    ? new Date($metrics.timestamp_ms).toLocaleTimeString([], {
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
      })
    : "--";
</script>

<div class="dashboard">
  <div class="dashboard-header">
    <h1>Performance Monitor</h1>
    <span class="timestamp">{lastUpdated}</span>
  </div>

  <div class="grid">
    <MetricCard label="CPU" icon="⚙" metric={$metrics.cpu} />
    <MetricCard label="GPU" icon="🎮" metric={$metrics.gpu} />
    <MetricCard label="RAM" icon="🧠" metric={$metrics.ram} showBytes />
    <MetricCard label="Disk" icon="💾" metric={$metrics.disk} showBytes />
  </div>
</div>
