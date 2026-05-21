<script lang="ts">
  import type { Config } from '../lib/config';
  import type { PublicPlaytest } from '../lib/api';
  import { ApiError, fetchPublicPlaytest } from '../lib/api';
  import { formatDateRange } from '../lib/dates';
  import { platformLabel } from '../lib/platforms';
  import {
    buildDiscordAuthorizeUrl,
    discordRedirectUri,
    clearPendingLogin,
    storePendingLogin,
  } from '../lib/auth';
  import Card from '../lib/ui/Card.svelte';
  import Banner from '../lib/ui/Banner.svelte';

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
    const redirectUri = discordRedirectUri();
    clearPendingLogin();
    storePendingLogin({ state, slug });
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

{#if loadError}
  <main class="mx-auto w-full max-w-2xl px-4 py-10">
    <section
      class="rounded-lg border border-red-200 bg-red-50 p-4 text-red-800"
      role="alert"
    >
      {loadError}
    </section>
  </main>
{:else if !playtest}
  <main class="mx-auto w-full max-w-2xl px-4 py-10">
    <p class="text-slate-500">Loading…</p>
  </main>
{:else}
  <Card>
    <article>
      <header class="bg-slate-900 px-8 py-10 text-white">
        <p class="text-xs font-semibold tracking-widest text-slate-300 uppercase">
          Closed Playtest
        </p>
        <h1 class="mt-2 text-3xl font-bold leading-tight">{playtest.title}</h1>
      </header>
      <div class="space-y-6 px-8 py-7">
        <dl class="flex flex-wrap gap-x-8 gap-y-2 text-sm text-slate-600">
          <div>
            <dt class="inline">Dates:</dt>
            <dd class="ml-1 inline font-semibold text-slate-900">
              {formatDateRange(playtest.startsAt, playtest.endsAt)}
            </dd>
          </div>
          <div data-testid="playtest-platforms">
            <dt class="inline">Platforms:</dt>
            <dd class="ml-1 inline font-semibold text-slate-900">
              {#if playtest.platforms && playtest.platforms.length > 0}
                {playtest.platforms.map(platformLabel).join(', ')}
              {:else}
                not specified
              {/if}
            </dd>
          </div>
        </dl>
        {#if playtest.bannerImageUrl}
          <img
            src={playtest.bannerImageUrl}
            alt=""
            class="w-full rounded-lg"
            referrerpolicy="no-referrer"
          />
        {/if}
        <section
          class="whitespace-pre-wrap leading-relaxed text-slate-800"
          data-testid="playtest-description"
        >
          {playtest.description}
        </section>
        <Banner tone="muted" role={null}>
          <strong class="font-semibold text-slate-900">Before you sign up:</strong> You'll
          need a Discord account to authenticate. The studio may require NDA acceptance
          before reviewing your application.
        </Banner>
        <button
          type="button"
          class="w-full rounded-lg bg-indigo-600 px-4 py-3 font-medium text-white shadow-sm transition hover:bg-indigo-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-500 disabled:opacity-60"
          disabled={redirecting}
          onclick={handleSignup}
        >
          {redirecting ? 'Redirecting to Discord…' : 'Sign In with Discord to Sign Up'}
        </button>
      </div>
    </article>
  </Card>
{/if}
