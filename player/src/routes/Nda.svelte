<script lang="ts">
  import type { Config } from '../lib/config';
  import {
    ApiError,
    acceptNda,
    fetchApplicantStatusWithIds,
    fetchPlayerPlaytest,
    type ApplicantWithPlaytestId,
    type PlayerPlaytest,
  } from '../lib/api';
  import { getAccessToken } from '../lib/auth';
  import { navigate, pendingPath, playtestPath } from '../lib/router';
  import Card from '../lib/ui/Card.svelte';

  let { config, slug }: { config: Config; slug: string } = $props();

  let playtest = $state<PlayerPlaytest | null>(null);
  let applicant = $state<ApplicantWithPlaytestId | null>(null);
  let agreed = $state(false);
  let submitting = $state(false);
  let loadError = $state<string | null>(null);
  let submitError = $state<string | null>(null);

  // Direct hits without a token bounce to landing for Discord login.
  if (typeof window !== 'undefined' && !getAccessToken()) {
    navigate(playtestPath(slug));
  }

  async function load() {
    try {
      const [pt, app] = await Promise.all([
        fetchPlayerPlaytest(config, slug),
        fetchApplicantStatusWithIds(config, slug),
      ]);
      // PRD §5.3 client-detection rule: NdaReacceptRequired ⇔
      // applicant.ndaVersionHash !== playtest.currentNdaVersionHash. If
      // an NDA is not required, or the applicant already accepted the
      // current version, fall through to Pending.
      if (!pt.ndaRequired || app.ndaVersionHash === pt.currentNdaVersionHash) {
        navigate(pendingPath(slug));
        return;
      }
      playtest = pt;
      applicant = app;
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        navigate(playtestPath(slug));
        return;
      }
      if (err instanceof ApiError && err.status === 404) {
        loadError = 'This playtest is not available.';
        return;
      }
      loadError = 'Could not load the NDA — please try again.';
    }
  }

  async function handleAccept(event: Event) {
    event.preventDefault();
    if (!agreed || !applicant) return;
    submitError = null;
    submitting = true;
    try {
      await acceptNda(config, applicant.playtestId);
      navigate(pendingPath(slug));
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        submitError = 'Your session has expired — please sign in again.';
      } else {
        submitError = 'Could not record your acceptance — please try again.';
      }
    } finally {
      submitting = false;
    }
  }

  load();
</script>

{#if loadError}
  <main class="mx-auto w-full max-w-2xl px-4 py-10">
    <p class="rounded-lg border border-red-200 bg-red-50 p-4 text-red-800" role="alert">
      {loadError}
    </p>
  </main>
{:else if !playtest || !applicant}
  <main class="mx-auto w-full max-w-2xl px-4 py-10">
    <p class="text-slate-500">Loading…</p>
  </main>
{:else}
  <Card>
    <form class="space-y-6 px-8 py-7" onsubmit={handleAccept}>
      <header>
        <h1 class="text-2xl font-bold text-slate-900">Non-Disclosure Agreement</h1>
        <p class="mt-1 text-sm text-slate-600">
          You must read and accept the NDA before your sign-up is submitted.
        </p>
      </header>
      <hr class="border-slate-200" />
      <article
        class="max-h-72 overflow-y-auto rounded-lg border border-slate-200 bg-slate-50 p-4 text-sm leading-relaxed whitespace-pre-wrap text-slate-800"
        data-testid="nda-text"
      >
        {playtest.ndaText}
      </article>
      <label class="flex items-start gap-2.5 text-sm text-slate-800">
        <input
          type="checkbox"
          checked={agreed}
          onchange={(e) => (agreed = (e.currentTarget as HTMLInputElement).checked)}
          class="mt-0.5 h-4 w-4 rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
        />
        <span>I have read and agree to the Non-Disclosure Agreement</span>
      </label>
      {#if submitError}
        <p class="text-sm text-red-700" role="alert">{submitError}</p>
      {/if}
      <button
        type="submit"
        disabled={!agreed || submitting}
        class="w-full rounded-lg bg-indigo-600 px-4 py-3 font-medium text-white shadow-sm transition hover:bg-indigo-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-500 disabled:opacity-60"
      >
        {submitting ? 'Submitting…' : 'Submit Sign-Up'}
      </button>
    </form>
  </Card>
{/if}
