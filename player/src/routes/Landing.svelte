<script lang="ts">
  import type { Config } from '../lib/config';
  import type { PublicPlaytest } from '../lib/api';
  import { ApiError, fetchPublicPlaytest } from '../lib/api';
  import { formatDateRange } from '../lib/dates';
  import { platformLabel } from '../lib/platforms';
  import {
    buildDiscordAuthorizeUrl,
    clearPendingLogin,
    storePendingLogin,
  } from '../lib/auth';

  let { config, slug }: { config: Config; slug: string } = $props();

  let playtest = $state<PublicPlaytest | null>(null);
  let loadError = $state<string | null>(null);
  let redirecting = $state(false);

  async function load(target: string) {
    // Stale-response guard: if the slug changed while a fetch was in
    // flight, discard the result so the UI doesn't flash the wrong row.
    try {
      const result = await fetchPublicPlaytest(config, target);
      if (target !== slug) return;
      playtest = result;
      loadError = null;
    } catch (err) {
      if (target !== slug) return;
      if (err instanceof ApiError && err.status === 404) {
        playtest = null;
        loadError = 'This playtest is not available.';
        return;
      }
      playtest = null;
      loadError = 'Could not load playtest — please try again later.';
    }
  }

  function handleSignup() {
    redirecting = true;
    const state = crypto.randomUUID();
    const redirectUri = `${window.location.origin}/callback`;
    clearPendingLogin();
    storePendingLogin({ state, slug, returnTo: `#/playtest/${slug}/signup` });
    window.location.href = buildDiscordAuthorizeUrl({
      clientId: config.discordClientId,
      redirectUri,
      state,
    });
  }

  $effect(() => {
    load(slug);
  });
</script>

<main class="mx-auto max-w-2xl p-6 md:p-10">
  {#if loadError}
    <section class="rounded border border-red-200 bg-red-50 p-4 text-red-800">
      {loadError}
    </section>
  {:else if !playtest}
    <p class="text-slate-500">Loading…</p>
  {:else}
    <article>
      <h1 class="text-3xl font-semibold">{playtest.title}</h1>
      {#if playtest.bannerImageUrl}
        <img
          src={playtest.bannerImageUrl}
          alt=""
          class="mt-6 w-full rounded"
          referrerpolicy="no-referrer"
        />
      {/if}
      <p class="mt-4 text-sm text-slate-600">
        {formatDateRange(playtest.startsAt, playtest.endsAt)}
      </p>
      {#if playtest.platforms && playtest.platforms.length > 0}
        <p class="mt-2 text-sm text-slate-600">
          Platforms: {playtest.platforms.map(platformLabel).join(', ')}
        </p>
      {/if}
      <section class="mt-6 whitespace-pre-wrap text-slate-800" data-testid="playtest-description">
        {playtest.description}
      </section>
      <button
        type="button"
        class="mt-8 rounded bg-slate-900 px-4 py-2 text-white hover:bg-slate-700 disabled:opacity-50"
        disabled={redirecting}
        onclick={handleSignup}
      >
        {redirecting ? 'Redirecting to Discord…' : 'Sign up'}
      </button>
    </article>
  {/if}
</main>
