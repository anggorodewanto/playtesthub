<script lang="ts">
  import type { Config } from '../lib/config';
  import {
    ApiError,
    fetchApplicantStatusWithIds,
    fetchPlayerPlaytest,
    type ApplicantWithPlaytestId,
  } from '../lib/api';
  import { navigate, ndaPath, playtestPath } from '../lib/router';

  let { config, slug }: { config: Config; slug: string } = $props();

  let applicant = $state<ApplicantWithPlaytestId | null>(null);
  let loadError = $state<string | null>(null);

  async function load() {
    try {
      const [pt, app] = await Promise.all([
        fetchPlayerPlaytest(config, slug),
        fetchApplicantStatusWithIds(config, slug),
      ]);
      // PRD §5.3: when an NDA is required and the applicant's stored
      // hash diverges from the playtest's current hash, the player must
      // re-accept before progressing — bounce them to the NDA route.
      if (pt.ndaRequired && app.ndaVersionHash !== pt.currentNdaVersionHash) {
        navigate(ndaPath(slug));
        return;
      }
      applicant = app;
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        navigate(playtestPath(slug));
        return;
      }
      if (err instanceof ApiError && err.status === 404) {
        loadError = 'No application on file — please sign up first.';
        return;
      }
      loadError = 'Could not load application status.';
    }
  }

  load();
</script>

<main class="mx-auto max-w-lg p-6 md:p-10">
  {#if loadError}
    <p class="rounded border border-red-200 bg-red-50 p-4 text-red-800">{loadError}</p>
  {:else if !applicant}
    <p class="text-slate-500">Loading…</p>
  {:else if applicant.status === 'APPLICANT_STATUS_PENDING'}
    <h1 class="text-2xl font-semibold">Your application is under review.</h1>
    <p class="mt-3 text-slate-700">
      The studio will review your application. If you're selected, you'll receive a Discord
      message with your key.
    </p>
  {:else if applicant.status === 'APPLICANT_STATUS_REJECTED'}
    <h1 class="text-2xl font-semibold">Not selected for this playtest.</h1>
    <p class="mt-3 text-slate-700">Thanks for applying.</p>
  {:else if applicant.status === 'APPLICANT_STATUS_APPROVED'}
    <h1 class="text-2xl font-semibold">You're approved!</h1>
    <p class="mt-3 text-slate-700">
      Key retrieval is coming in a later release. For now, check your Discord DMs for the code.
    </p>
  {:else}
    <p class="text-slate-500">Status: {applicant.status}</p>
  {/if}
</main>
