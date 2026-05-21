<script lang="ts">
  import type { Config } from '../lib/config';
  import {
    ApiError,
    fetchMyProfile,
    fetchPlayerPlaytest,
    submitSignup,
    type Platform,
    type PlayerPlaytest,
  } from '../lib/api';
  import { PLATFORM_OPTIONS } from '../lib/platforms';
  import { distributionLabel } from '../lib/distribution';
  import { getAccessToken } from '../lib/auth';
  import { navigate, ndaPath, playtestPath } from '../lib/router';
  import Card from '../lib/ui/Card.svelte';

  let { config, slug }: { config: Config; slug: string } = $props();

  let selected = $state<Set<Platform>>(new Set());
  let submitting = $state(false);
  let errorMessage = $state<string | null>(null);
  let playtest = $state<PlayerPlaytest | null>(null);
  let discordHandle = $state<string>('');

  const PLACEHOLDER_HANDLE = 'Linked via Discord OAuth';

  // If the user arrives without a token (direct link), bounce them back
  // to the landing so they can run the Discord login flow.
  const hasToken = getAccessToken() !== null;
  if (!hasToken && typeof window !== 'undefined') {
    navigate(playtestPath(slug));
  }

  async function loadHeader() {
    try {
      const pt = await fetchPlayerPlaytest(config, slug);
      playtest = pt;
    } catch {
      // Best-effort header crumb + distribution row; signup itself does
      // not depend on it.
    }
  }

  async function loadProfile() {
    try {
      const me = await fetchMyProfile(config);
      if (me.discordHandle) discordHandle = me.discordHandle;
    } catch {
      // Best-effort identity; missing handle is non-fatal (the placeholder
      // text still tells the user the account is authenticated).
    }
  }

  if (hasToken) {
    loadHeader();
    loadProfile();
  }

  function togglePlatform(p: Platform) {
    const next = new Set(selected);
    if (next.has(p)) next.delete(p);
    else next.add(p);
    selected = next;
  }

  async function handleSubmit(event: Event) {
    event.preventDefault();
    if (selected.size === 0) {
      errorMessage = 'Select at least one platform.';
      return;
    }
    errorMessage = null;
    submitting = true;
    try {
      await submitSignup(config, slug, { platforms: Array.from(selected) });
      navigate(ndaPath(slug));
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        navigate(ndaPath(slug));
        return;
      }
      if (err instanceof ApiError) {
        errorMessage =
          err.status === 401
            ? 'Your session has expired — please sign in again.'
            : 'Could not submit signup — please try again.';
      } else {
        errorMessage = 'Could not submit signup — please try again.';
      }
    } finally {
      submitting = false;
    }
  }
</script>

<Card>
  {#snippet crumb()}
    Signing up for: <span class="font-semibold text-slate-900">{playtest?.title ?? slug}</span>
  {/snippet}
  <form class="space-y-6 px-8 py-7" onsubmit={handleSubmit}>
    <header>
      <h1 class="text-2xl font-bold text-slate-900">Sign-Up Form</h1>
      <p class="mt-1 text-sm text-slate-600">Tell us a bit about yourself as a player.</p>
    </header>
    {#if playtest}
      <dl class="flex flex-wrap gap-x-6 gap-y-1 text-sm text-slate-600">
        <div data-testid="signup-distribution">
          <dt class="inline">Distribution:</dt>
          <dd class="ml-1 inline font-semibold text-slate-900">
            {distributionLabel(playtest.distributionModel)}
          </dd>
        </div>
      </dl>
    {/if}
    <hr class="border-slate-200" />
    <div>
      <label for="signup-discord-handle" class="block text-sm font-medium text-slate-800">
        Discord Handle
      </label>
      <input
        id="signup-discord-handle"
        type="text"
        readonly
        value={discordHandle || PLACEHOLDER_HANDLE}
        class="mt-1.5 w-full rounded-lg border border-slate-300 bg-slate-50 px-3 py-2 text-slate-700"
        data-testid="signup-discord-handle"
      />
      <p class="mt-1.5 text-xs text-slate-500">Auto-filled from your Discord account.</p>
    </div>
    <fieldset>
      <legend class="block text-sm font-medium text-slate-800">Platforms You Own</legend>
      <div class="mt-2 flex flex-wrap gap-2">
        {#each PLATFORM_OPTIONS as opt (opt.value)}
          {@const isSelected = selected.has(opt.value)}
          <label
            class="cursor-pointer rounded-lg border px-4 py-2 text-sm font-medium transition select-none {isSelected
              ? 'border-indigo-500 bg-indigo-50 text-indigo-700 ring-1 ring-indigo-500'
              : 'border-slate-300 bg-white text-slate-700 hover:border-slate-400'}"
          >
            <input
              type="checkbox"
              class="sr-only"
              name="platforms"
              value={opt.value}
              checked={isSelected}
              onchange={() => togglePlatform(opt.value)}
              aria-label={opt.label}
            />
            {opt.label}
          </label>
        {/each}
      </div>
      <p class="mt-1.5 text-xs text-slate-500">
        Select all platforms you own. Used for triage only.
      </p>
    </fieldset>
    {#if errorMessage}
      <p data-testid="signup-error" class="text-sm text-red-700" role="alert">{errorMessage}</p>
    {/if}
    <button
      type="submit"
      disabled={submitting}
      class="w-full rounded-lg bg-indigo-600 px-4 py-3 font-medium text-white shadow-sm transition hover:bg-indigo-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-500 disabled:opacity-60"
    >
      {submitting ? 'Submitting…' : 'Continue'}
    </button>
  </form>
</Card>
