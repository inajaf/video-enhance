<script lang="ts">
  import { tweened } from "svelte/motion";
  import { cubicOut } from "svelte/easing";

  export let value: number | null = null;
  export let size = 96;
  export let strokeWidth = 9;

  const display = tweened(0, { duration: 600, easing: cubicOut });
  $: display.set(value ?? 0);

  $: level =
    value === null
      ? "unavailable"
      : value >= 99
        ? "very-high"
        : value >= 85
          ? "high"
          : value >= 60
            ? "medium"
            : "low";

  $: radius = (size - strokeWidth) / 2;
  $: circumference = 2 * Math.PI * radius;
  $: offset = circumference * (1 - $display / 100);
</script>

<svg width={size} height={size} viewBox="0 0 {size} {size}" class="gauge" data-level={level}>
  <circle
    class="gauge-track"
    cx={size / 2}
    cy={size / 2}
    r={radius}
    stroke-width={strokeWidth}
    fill="none"
  />
  {#if value !== null}
    <circle
      class="gauge-fill"
      cx={size / 2}
      cy={size / 2}
      r={radius}
      stroke-width={strokeWidth}
      fill="none"
      stroke-linecap="round"
      stroke-dasharray={circumference}
      stroke-dashoffset={offset}
      transform="rotate(-90 {size / 2} {size / 2})"
    />
  {/if}
</svg>

<style>
  .gauge-track {
    stroke: var(--border-strong);
  }

  .gauge-fill {
    transition: stroke 400ms ease;
  }

  .gauge[data-level="low"] .gauge-fill {
    stroke: var(--accent);
  }

  .gauge[data-level="medium"] .gauge-fill {
    stroke: var(--primary);
  }

  .gauge[data-level="high"] .gauge-fill {
    stroke: var(--warn);
  }

  .gauge[data-level="very-high"] .gauge-fill {
    stroke: var(--danger);
    filter: drop-shadow(0 0 6px var(--danger-soft));
  }

  .gauge[data-level="unavailable"] .gauge-track {
    stroke-dasharray: 3 6;
  }

  @media (prefers-reduced-motion: reduce) {
    .gauge-fill {
      transition: none;
    }
  }
</style>
