<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    children,
    crumb,
    testid,
  }: {
    children: Snippet;
    crumb?: Snippet;
    testid?: string;
  } = $props();
</script>

<main class="mx-auto w-full max-w-2xl px-4 py-10 sm:py-14">
  {#if crumb}
    <div class="mb-4 px-1 text-sm text-slate-600">{@render crumb()}</div>
  {/if}
  <div
    class="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm"
    data-testid={testid}
  >
    {@render children()}
  </div>
</main>
