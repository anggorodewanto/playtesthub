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

<main class="mx-auto max-w-2xl p-6 md:p-10">
  {#if loadError}
    <p class="rounded border border-red-200 bg-red-50 p-4 text-red-800" role="alert">{loadError}</p>
  {:else if !playtest || !applicant}
    <p class="text-slate-500">Loading…</p>
  {:else}
    <h1 class="text-2xl font-semibold">Non-disclosure agreement</h1>
    <p class="mt-2 text-sm text-slate-600">
      Please read and accept the NDA before your application proceeds.
    </p>
    <article
      class="mt-6 max-h-96 overflow-y-auto whitespace-pre-wrap rounded border border-slate-200 bg-slate-50 p-4 text-sm text-slate-800"
      data-testid="nda-text"
    >
      {playtest.ndaText}
    </article>
    <form class="mt-6 space-y-4" onsubmit={handleAccept}>
      <label class="flex items-start gap-2">
        <input
          type="checkbox"
          checked={agreed}
          onchange={(e) => (agreed = (e.currentTarget as HTMLInputElement).checked)}
        />
        <span class="text-sm text-slate-800">I have read and agree to the NDA above.</span>
      </label>
      {#if submitError}
        <p class="text-sm text-red-700" role="alert">{submitError}</p>
      {/if}
      <button
        type="submit"
        disabled={!agreed || submitting}
        class="rounded bg-slate-900 px-4 py-2 text-white hover:bg-slate-700 disabled:opacity-50"
      >
        {submitting ? 'Submitting…' : 'Accept'}
      </button>
    </form>
  {/if}
</main>
