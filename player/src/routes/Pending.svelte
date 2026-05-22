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
  import { navigate, ndaPath, playtestPath, surveyPath } from '../lib/router';
  import Card from '../lib/ui/Card.svelte';
  import Banner from '../lib/ui/Banner.svelte';

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

{#if loadError}
  <main class="mx-auto w-full max-w-2xl px-4 py-10">
    <p class="rounded-lg border border-red-200 bg-red-50 p-4 text-red-800" role="alert">{loadError}</p>
  </main>
{:else if !applicant}
  <main class="mx-auto w-full max-w-2xl px-4 py-10">
    <p class="text-slate-500">Loading…</p>
  </main>
{:else}
  <Card>
    <div class="space-y-6 px-8 py-8">
      {#if applicant.status === 'APPLICANT_STATUS_PENDING'}
        <div class="flex flex-col items-center text-center">
          <div class="flex h-16 w-16 items-center justify-center rounded-full bg-amber-100">
            <svg
              class="h-8 w-8 text-amber-600"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="1.8"
              aria-hidden="true"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M6.75 3h10.5M6.75 21h10.5M7.5 3v3.75c0 1.75 1 3.34 2.56 4.12L12 12l1.94.87A4.6 4.6 0 0 1 16.5 17v4M16.5 3v3.75c0 1.75-1 3.34-2.56 4.12L12 12l-1.94.87A4.6 4.6 0 0 0 7.5 17v4"
              />
            </svg>
          </div>
          <h1 class="mt-5 text-2xl font-bold text-slate-900">Application Under Review</h1>
          <p class="mt-2 max-w-md text-sm leading-relaxed text-slate-600">
            Your sign-up for <span class="font-semibold text-slate-900">{playtest?.title ?? slug}</span>
            has been received. The studio team will look at your application and respond soon.
          </p>
        </div>
        <Banner tone="warn">
          If your application is approved, you'll be notified via
          <strong class="font-semibold">Discord DM</strong> from the
          <strong class="font-semibold">PlaytestHub bot</strong>. Make sure your Discord DMs are
          open.
        </Banner>
        <dl class="rounded-lg border border-slate-200 bg-slate-50 p-4 text-sm">
          <div class="flex items-center justify-between">
            <dt class="text-slate-600">Status</dt>
            <dd class="font-semibold text-amber-700">Awaiting decision</dd>
          </div>
        </dl>
        {#if config.discordInviteUrl}
          <!-- Required for the DM to land — Discord blocks bot DMs when the
               bot and recipient share no guild (error 50278). The invite URL
               is set by the studio via the PLAYER_DISCORD_INVITE_URL repo
               Variable; see docs/runbooks/setup-ags-discord.md. -->
          <p class="text-sm text-slate-600">
            Join our Discord while you wait so we can DM your key:
            <a
              href={config.discordInviteUrl}
              target="_blank"
              rel="noopener noreferrer"
              data-testid="discord-invite-link"
              class="ml-1 font-medium text-indigo-700 underline hover:text-indigo-900"
            >
              Open Discord invite →
            </a>
          </p>
        {/if}
      {:else if applicant.status === 'APPLICANT_STATUS_REJECTED'}
        <div class="flex flex-col items-center text-center">
          <div class="flex h-16 w-16 items-center justify-center rounded-full bg-slate-200">
            <svg
              class="h-8 w-8 text-slate-500"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="1.8"
              aria-hidden="true"
            >
              <path stroke-linecap="round" stroke-linejoin="round" d="M6 18 18 6M6 6l12 12" />
            </svg>
          </div>
          <h1 class="mt-5 text-2xl font-bold text-slate-900">Not selected for this playtest.</h1>
          <p class="mt-2 text-sm text-slate-600">Thanks for applying.</p>
        </div>
      {:else if applicant.status === 'APPLICANT_STATUS_APPROVED'}
        <div class="flex flex-col items-center text-center">
          <div class="flex h-16 w-16 items-center justify-center rounded-full bg-green-100">
            <svg
              class="h-8 w-8 text-green-600"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2.2"
              aria-hidden="true"
            >
              <path stroke-linecap="round" stroke-linejoin="round" d="m4.5 12.75 6 6 9-13.5" />
            </svg>
          </div>
          {#if playtest?.distributionModel === 'DISTRIBUTION_MODEL_ADT'}
            <h1 class="mt-5 text-2xl font-bold text-slate-900">Sign-Up Successful!</h1>
            <p class="mt-2 max-w-md text-sm leading-relaxed text-slate-600">
              You've successfully signed up for
              <span class="font-semibold text-slate-900">{playtest?.title ?? slug}</span>. Your
              download link is below — we've also sent it via
              <span class="font-semibold text-slate-900">Discord DM</span> from the PlaytestHub
              bot.
            </p>
          {:else}
            <h1 class="mt-5 text-2xl font-bold text-slate-900">You're approved!</h1>
            <p class="mt-2 max-w-md text-sm leading-relaxed text-slate-600">
              Your key for
              <span class="font-semibold text-slate-900">{playtest?.title ?? slug}</span> is below
              — copy it before redeeming.
            </p>
          {/if}
        </div>
        {#if reacceptRequired}
          <Banner tone="warn">
            The NDA was updated. Your key is still valid; please re-accept the new NDA before
            submitting any future surveys.
          </Banner>
        {/if}
        {#if playtest?.distributionModel === 'DISTRIBUTION_MODEL_ADT'}
          {#if adtDownload}
            <Banner tone="info">
              Check your Discord DMs from
              <strong class="font-semibold">PlaytestHub Bot</strong>. Make sure your DMs are open
              so the bot can reach you.
              {#if config.discordInviteUrl}
                <span class="mt-2 block">
                  You must also join our Discord server so the bot can reach you —
                  <a
                    href={config.discordInviteUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    data-testid="discord-invite-link-approved"
                    class="font-medium text-indigo-700 underline hover:text-indigo-900"
                  >
                    Open Discord invite →
                  </a>
                </span>
              {/if}
            </Banner>
            <div
              class="rounded-lg border border-slate-200 bg-slate-50 p-4"
              data-testid="adt-download-card"
            >
              {#if adtDownload.urls.length <= 1}
                <a
                  href={adtDownload.urls[0] ?? '#'}
                  data-testid="adt-download-link"
                  class="break-all font-medium text-indigo-700 underline hover:text-indigo-900"
                >
                  Download playtest build
                </a>
              {:else}
                <p class="font-medium text-slate-900" data-testid="adt-download-multi-heading">
                  Download playtest build ({adtDownload.urls.length} files)
                </p>
                <ol class="mt-2 list-decimal space-y-1 pl-5 text-sm">
                  {#each adtDownload.urls as url, i}
                    <li>
                      <a
                        href={url}
                        data-testid={`adt-download-link-${i}`}
                        class="break-all font-medium text-indigo-700 underline hover:text-indigo-900"
                      >
                        File {i + 1}
                      </a>
                    </li>
                  {/each}
                </ol>
              {/if}
              {#if adtDownload.expiresAt}
                <p class="mt-2 text-xs text-slate-500" data-testid="adt-download-expiry">
                  Link{adtDownload.urls.length > 1 ? 's expire' : ' expires'} at
                  {new Date(adtDownload.expiresAt).toLocaleString()}.
                </p>
              {/if}
              {#if adtDownload.source === 'fallback'}
                <p class="mt-2 text-xs text-slate-500" data-testid="adt-download-source">
                  Shared playtest download (operator-managed).
                </p>
              {/if}
            </div>
            <p class="text-sm text-slate-600">
              Trouble downloading? Contact the studio organiser via Discord.
            </p>
          {:else if codeError}
            <p
              class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-800"
              role="alert"
            >
              {codeError}
            </p>
          {:else}
            <p class="text-sm text-slate-500">Loading your download link…</p>
          {/if}
        {:else if grantedCode}
          <Banner tone="info">
            Check your Discord DMs from
            <strong class="font-semibold">PlaytestHub Bot</strong>. Make sure your DMs are open
            so the bot can reach you.
            {#if config.discordInviteUrl}
              <span class="mt-2 block">
                You must also join our Discord server so the bot can reach you —
                <a
                  href={config.discordInviteUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  data-testid="discord-invite-link-approved"
                  class="font-medium text-indigo-700 underline hover:text-indigo-900"
                >
                  Open Discord invite →
                </a>
              </span>
            {/if}
          </Banner>
          <div class="flex items-center gap-2">
            <input
              type="text"
              readonly
              value={grantedCode.value}
              data-testid="granted-code-value"
              aria-label="Your playtest key"
              class="flex-1 rounded-lg border border-slate-300 bg-slate-50 px-3 py-2 font-mono text-sm"
              onfocus={(e) => (e.currentTarget as HTMLInputElement).select()}
            />
            <button
              type="button"
              onclick={handleCopy}
              class="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white shadow-sm transition hover:bg-slate-700"
            >
              {copied ? 'Copied' : 'Copy'}
            </button>
          </div>
          {#if grantedCode.distributionModel === 'DISTRIBUTION_MODEL_STEAM_KEYS'}
            <p class="text-sm leading-relaxed text-slate-700">
              Redeem on Steam: open the Steam client, go to
              <strong>Games → Activate a Product on Steam…</strong>, and paste the key above.
            </p>
          {:else if grantedCode.distributionModel === 'DISTRIBUTION_MODEL_AGS_CAMPAIGN'}
            <p class="text-sm leading-relaxed text-slate-700">
              Redeem in-game: launch the game and use the in-game code-redeem screen
              (<code class="rounded bg-slate-200 px-1">PublicRedeemCode</code>) to apply this code.
            </p>
          {/if}
        {:else if codeError}
          <p
            class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-800"
            role="alert"
          >
            {codeError}
          </p>
        {:else}
          <p class="text-sm text-slate-500">Loading your key…</p>
        {/if}
        {#if playtest?.surveyId && !reacceptRequired}
          <!-- Survey discovery CTA (PRD §5.6). Server enforces
               one-shot via SurveyResponseSubmittedAt on the applicant;
               we flip the affordance on whether that timestamp is set. -->
          <div class="rounded-lg border border-slate-200 bg-slate-50 p-4">
            {#if applicant.surveyResponseSubmittedAt}
              <span
                class="inline-flex items-center rounded-lg bg-slate-200 px-4 py-2 text-sm font-medium text-slate-600"
                data-testid="survey-cta-submitted"
              >
                Feedback submitted
                <span aria-hidden="true" class="ml-1">✓</span>
              </span>
              <p class="mt-2 text-xs text-slate-500">
                Submitted
                <time datetime={applicant.surveyResponseSubmittedAt}>
                  {new Date(applicant.surveyResponseSubmittedAt).toLocaleString()}
                </time>.
              </p>
            {:else}
              <p class="mb-2 text-sm text-slate-700">
                Help the studio — share your feedback on this playtest.
              </p>
              <a
                href={`#${surveyPath(slug)}`}
                data-testid="survey-cta-link"
                class="inline-flex items-center rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white shadow-sm transition hover:bg-slate-700"
              >
                Submit feedback
              </a>
            {/if}
          </div>
        {/if}
      {:else}
        <p class="text-sm text-slate-500">Status: {applicant.status}</p>
      {/if}
    </div>
  </Card>
{/if}
