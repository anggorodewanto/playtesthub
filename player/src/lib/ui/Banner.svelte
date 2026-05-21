<script lang="ts">
  import type { Snippet } from 'svelte';

  type Tone = 'info' | 'warn' | 'muted';

  let {
    tone = 'info',
    children,
    testid,
    role = 'status',
  }: {
    tone?: Tone;
    children: Snippet;
    testid?: string;
    role?: 'status' | 'alert' | null;
  } = $props();

  const palette: Record<Tone, string> = {
    info: 'border-indigo-200 bg-indigo-50 text-indigo-900',
    warn: 'border-amber-200 bg-amber-50 text-amber-900',
    muted: 'border-slate-200 bg-slate-50 text-slate-700',
  };
</script>

<div
  class="flex gap-3 rounded-lg border p-4 text-sm {palette[tone]}"
  role={role ?? undefined}
  data-testid={testid}
>
  <svg
    class="mt-0.5 h-5 w-5 flex-shrink-0"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.5"
    aria-hidden="true"
  >
    <path
      stroke-linecap="round"
      stroke-linejoin="round"
      d="M3.75 12c0-4.55 3.7-8.25 8.25-8.25S20.25 7.45 20.25 12c0 1.74-.54 3.36-1.46 4.7l.81 3.05-3.13-.83A8.21 8.21 0 0 1 12 20.25c-4.55 0-8.25-3.7-8.25-8.25Z"
    />
  </svg>
  <div class="min-w-0 flex-1 leading-relaxed">{@render children()}</div>
</div>
