<script lang="ts">
  let { message }: { message: string } = $props();
</script>

<main class="mx-auto max-w-xl p-8">
  <h1 class="text-2xl font-semibold text-red-700">playtesthub cannot start</h1>
  <p class="mt-4 text-slate-700">
    The runtime configuration in <code class="rounded bg-slate-200 px-1">config.json</code> is
    missing or invalid.
  </p>
  <pre
    data-testid="boot-error-message"
    class="mt-4 overflow-auto rounded bg-slate-100 p-3 text-sm text-red-700">{message}</pre>
  <p class="mt-4 text-sm text-slate-600">
    Ask the operator to fix the deployment before retrying.
  </p>
</main>
