<script lang="ts">
  import type { Config } from '../lib/config';
  import {
    clearPendingLogin,
    exchangeDiscordCode,
    IamError,
    readPendingLogin,
  } from '../lib/auth';
  import { navigate } from '../lib/router';

  let {
    config,
    params,
  }: { config: Config; params: Record<string, string> } = $props();

  let errorMessage = $state<string | null>(null);

  async function run() {
    if (params.error) {
      errorMessage = 'Login failed — please try again later';
      return;
    }
    const pending = readPendingLogin();
    if (!pending) {
      errorMessage = 'Login session expired. Please try again.';
      return;
    }
    if (!params.code || !params.state) {
      errorMessage = 'Login failed — please try again later';
      return;
    }
    if (params.state !== pending.state) {
      errorMessage = 'Login failed — please try again later';
      return;
    }
    // MUST byte-exactly match what Landing.svelte sent to
    // discord.com/oauth2/authorize. Discord rejects mismatches with
    // invalid_grant; AGS forwards that error verbatim.
    const redirectUri = `${window.location.origin}/callback`;
    try {
      await exchangeDiscordCode(config, {
        code: params.code,
        redirectUri,
      });
      clearPendingLogin();
      navigate(pending.returnTo);
    } catch (err) {
      errorMessage =
        err instanceof IamError ? err.userMessage : 'Login failed — please try again later';
    }
  }

  run();
</script>

<main class="mx-auto max-w-md p-8">
  {#if errorMessage}
    <h1 class="text-2xl font-semibold text-red-700">Login failed</h1>
    <p class="mt-3 text-slate-700" data-testid="callback-error">{errorMessage}</p>
  {:else}
    <p class="text-slate-500">Finishing login…</p>
  {/if}
</main>
