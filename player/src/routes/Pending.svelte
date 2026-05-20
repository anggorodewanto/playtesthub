<script lang="ts">
  import type { Config } from '../lib/config';
  import {
    type AdtDownloadInfo,
    ApiError,
    fetchAdtDownloadInfo,
    fetchApplicantStatusWithIds,
    fetchGrantedCode,
    fetchPlayerPlaytest,
    type ApplicantWithPlaytestId,
    type GrantedCode,
    type PlayerPlaytest,
  } from '../lib/api';
  import { navigate, ndaPath, playtestPath } from '../lib/router';

  let { config, slug }: { config: Config; slug: string } = $props();

  let playtest = $state<PlayerPlaytest | null>(null);
  let applicant = $state<ApplicantWithPlaytestId | null>(null);
  let grantedCode = $state<GrantedCode | null>(null);
  let adtDownload = $state<AdtDownloadInfo | null>(null);
  let codeError = $state<string | null>(null);
  let copied = $state(false);
  let loadError = $state<string | null>(null);

  let reacceptRequired = $derived(
    !!playtest &&
      !!applicant &&
      playtest.ndaRequired &&
      applicant.ndaVersionHash !== playtest.currentNdaVersionHash,
  );

  async function load() {
    try {
      const [pt, app] = await Promise.all([
        fetchPlayerPlaytest(config, slug),
        fetchApplicantStatusWithIds(config, slug),
      ]);
      // PRD §5.3 + §4.1 step 7: NDA re-accept blocks PENDING applicants
      // (they must re-accept before approval can proceed) but never hides
      // a code that was already GRANTED — APPROVED stays on this view.
      if (
        app.status === 'APPLICANT_STATUS_PENDING' &&
        pt.ndaRequired &&
        app.ndaVersionHash !== pt.currentNdaVersionHash
      ) {
        navigate(ndaPath(slug));
        return;
      }
      playtest = pt;
      applicant = app;
      if (app.status === 'APPLICANT_STATUS_APPROVED') {
        if (pt.distributionModel === 'DISTRIBUTION_MODEL_ADT') {
          await loadAdtDownload(app.playtestId);
        } else {
          await loadGrantedCode(app.playtestId);
        }
      }
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

  async function loadGrantedCode(playtestId: string) {
    try {
      grantedCode = await fetchGrantedCode(config, playtestId);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        navigate(playtestPath(slug));
        return;
      }
      codeError = 'Your code is not available yet — please refresh in a moment.';
    }
  }

  async function loadAdtDownload(playtestId: string) {
    try {
      adtDownload = await fetchAdtDownloadInfo(config, playtestId);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        navigate(playtestPath(slug));
        return;
      }
      codeError = 'Your download link is not available yet — please refresh in a moment.';
    }
  }

  async function handleCopy() {
    if (!grantedCode) return;
    try {
      await navigator.clipboard.writeText(grantedCode.value);
      copied = true;
      setTimeout(() => (copied = false), 2000);
    } catch {
      // Clipboard write can fail (insecure context, denied permission);
      // the input stays selectable so the player can copy manually.
      copied = false;
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
    {#if config.discordInviteUrl}
      <!-- Required for the DM to land — Discord blocks bot DMs when the
           bot and recipient share no guild (error 50278). The invite URL
           is set by the studio via the PLAYER_DISCORD_INVITE_URL repo
           Variable; see docs/runbooks/setup-ags-discord.md. -->
      <p class="mt-4 rounded border border-indigo-200 bg-indigo-50 p-3 text-sm text-indigo-900">
        Join our Discord while you wait so we can DM your key:
        <a
          href={config.discordInviteUrl}
          target="_blank"
          rel="noopener noreferrer"
          data-testid="discord-invite-link"
          class="ml-1 underline"
        >
          Open Discord invite →
        </a>
      </p>
    {/if}
  {:else if applicant.status === 'APPLICANT_STATUS_REJECTED'}
    <h1 class="text-2xl font-semibold">Not selected for this playtest.</h1>
    <p class="mt-3 text-slate-700">Thanks for applying.</p>
  {:else if applicant.status === 'APPLICANT_STATUS_APPROVED'}
    <h1 class="text-2xl font-semibold">You're approved!</h1>
    {#if reacceptRequired}
      <p
        class="mt-4 rounded border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900"
        role="status"
      >
        The NDA was updated. Your key is still valid; please re-accept the new NDA before
        submitting any future surveys.
      </p>
    {/if}
    {#if playtest?.distributionModel === 'DISTRIBUTION_MODEL_ADT'}
      {#if adtDownload}
        <p class="mt-3 text-slate-700">Download your playtest build below.</p>
        <div class="mt-4 rounded border border-slate-200 bg-slate-50 p-4" data-testid="adt-download-card">
          <a
            href={adtDownload.url}
            data-testid="adt-download-link"
            class="break-all font-medium text-indigo-700 underline hover:text-indigo-900"
          >
            Download playtest build
          </a>
          {#if adtDownload.expiresAt}
            <p class="mt-2 text-xs text-slate-500" data-testid="adt-download-expiry">
              Link expires at {new Date(adtDownload.expiresAt).toLocaleString()}.
            </p>
          {/if}
          {#if adtDownload.source === 'fallback'}
            <p class="mt-2 text-xs text-slate-500" data-testid="adt-download-source">
              Shared playtest download (operator-managed).
            </p>
          {/if}
        </div>
        <p class="mt-4 text-sm text-slate-700">
          Trouble downloading? Contact the studio organiser via Discord.
        </p>
      {:else if codeError}
        <p class="mt-4 rounded border border-red-200 bg-red-50 p-3 text-sm text-red-800" role="alert">
          {codeError}
        </p>
      {:else}
        <p class="mt-3 text-slate-500">Loading your download link…</p>
      {/if}
    {:else if grantedCode}
      <p class="mt-3 text-slate-700">Your key is below — copy it before redeeming.</p>
      <div class="mt-4 flex items-center gap-2">
        <input
          type="text"
          readonly
          value={grantedCode.value}
          data-testid="granted-code-value"
          aria-label="Your playtest key"
          class="flex-1 rounded border border-slate-300 bg-slate-50 px-3 py-2 font-mono text-sm"
          onfocus={(e) => (e.currentTarget as HTMLInputElement).select()}
        />
        <button
          type="button"
          onclick={handleCopy}
          class="rounded bg-slate-900 px-3 py-2 text-sm text-white hover:bg-slate-700"
        >
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      {#if grantedCode.distributionModel === 'DISTRIBUTION_MODEL_STEAM_KEYS'}
        <p class="mt-4 text-sm text-slate-700">
          Redeem on Steam: open the Steam client, go to <strong>Games → Activate a Product on
          Steam…</strong>, and paste the key above.
        </p>
      {:else if grantedCode.distributionModel === 'DISTRIBUTION_MODEL_AGS_CAMPAIGN'}
        <p class="mt-4 text-sm text-slate-700">
          Redeem in-game: launch the game and use the in-game code-redeem screen
          (<code class="rounded bg-slate-200 px-1">PublicRedeemCode</code>) to apply this code.
        </p>
      {/if}
    {:else if codeError}
      <p class="mt-4 rounded border border-red-200 bg-red-50 p-3 text-sm text-red-800" role="alert">
        {codeError}
      </p>
    {:else}
      <p class="mt-3 text-slate-500">Loading your key…</p>
    {/if}
  {:else}
    <p class="text-slate-500">Status: {applicant.status}</p>
  {/if}
</main>
