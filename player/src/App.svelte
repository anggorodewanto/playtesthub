<script lang="ts">
  import type { Config } from './lib/config';
  import { route } from './lib/router';
  import Landing from './routes/Landing.svelte';
  import Signup from './routes/Signup.svelte';
  import Nda from './routes/Nda.svelte';
  import Pending from './routes/Pending.svelte';
  import Callback from './routes/Callback.svelte';

  let { config }: { config: Config } = $props();
</script>

{#if $route.name === 'landing'}
  <Landing {config} slug={$route.slug} />
{:else if $route.name === 'signup'}
  <Signup {config} slug={$route.slug} />
{:else if $route.name === 'nda'}
  <Nda {config} slug={$route.slug} />
{:else if $route.name === 'pending'}
  <Pending {config} slug={$route.slug} />
{:else if $route.name === 'callback'}
  <Callback {config} params={$route.params} />
{:else}
  <main class="mx-auto max-w-lg p-8">
    <h1 class="text-2xl font-semibold">playtesthub</h1>
    <p class="mt-3 text-slate-700">No playtest selected.</p>
    <p class="mt-2 text-sm text-slate-500">
      Expected URL shape:
      <code class="rounded bg-slate-200 px-1">#/playtest/&lt;slug&gt;</code>
    </p>
  </main>
{/if}
