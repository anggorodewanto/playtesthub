<script lang="ts">
  import type { Config } from '../lib/config';
  import {
    ApiError,
    fetchApplicantStatusWithIds,
    fetchPlayerPlaytest,
    fetchSurvey,
    submitSurveyResponse,
    type ApplicantWithPlaytestId,
    type PlayerPlaytest,
    type Survey,
    type SurveyAnswerInput,
    type SurveyQuestion,
  } from '../lib/api';
  import { getAccessToken } from '../lib/auth';
  import { navigate, pendingPath, playtestPath } from '../lib/router';
  import Card from '../lib/ui/Card.svelte';
  import Banner from '../lib/ui/Banner.svelte';

  let { config, slug }: { config: Config; slug: string } = $props();

  let playtest = $state<PlayerPlaytest | null>(null);
  let applicant = $state<ApplicantWithPlaytestId | null>(null);
  let survey = $state<Survey | null>(null);
  let loadedVersion = $state<number | null>(null);
  let versionBumped = $state(false);
  let textAnswers = $state<Record<string, string>>({});
  let ratingAnswers = $state<Record<string, number>>({});
  let multiAnswers = $state<Record<string, string[]>>({});
  let submitting = $state(false);
  let submitted = $state(false);
  let validationError = $state<string | null>(null);
  let submitError = $state<string | null>(null);
  let loadError = $state<string | null>(null);
  let pollHandle: ReturnType<typeof setInterval> | null = null;

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
      // PRD §5.6 client-side gating: APPROVED + NDA-current + survey-pinned.
      // Anything else falls back to Pending — server re-checks regardless.
      if (app.status !== 'APPLICANT_STATUS_APPROVED') {
        navigate(pendingPath(slug));
        return;
      }
      if (pt.ndaRequired && app.ndaVersionHash !== pt.currentNdaVersionHash) {
        navigate(pendingPath(slug));
        return;
      }
      if (!pt.surveyId) {
        navigate(pendingPath(slug));
        return;
      }
      const s = await fetchSurvey(config, app.playtestId);
      playtest = pt;
      applicant = app;
      survey = s;
      loadedVersion = s.version;
      seedAnswerStores(s.questions);
      startVersionPoll(app.playtestId);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        navigate(playtestPath(slug));
        return;
      }
      if (err instanceof ApiError && err.status === 404) {
        // No survey on the playtest, or playtest is invisible — fall back
        // to Pending and let that route's copy explain the state.
        navigate(pendingPath(slug));
        return;
      }
      loadError = 'Could not load the survey — please try again.';
    }
  }

  function seedAnswerStores(questions: SurveyQuestion[]) {
    for (const q of questions) {
      if (q.type === 'SURVEY_QUESTION_TYPE_TEXT') textAnswers[q.id] ??= '';
      if (q.type === 'SURVEY_QUESTION_TYPE_MULTI_CHOICE') multiAnswers[q.id] ??= [];
    }
  }

  function startVersionPoll(playtestId: string) {
    if (typeof window === 'undefined' || pollHandle) return;
    pollHandle = setInterval(() => {
      void refetchForVersionBump(playtestId);
    }, 30_000);
  }

  async function refetchForVersionBump(playtestId: string) {
    if (submitted) return;
    try {
      const fresh = await fetchSurvey(config, playtestId);
      if (loadedVersion !== null && fresh.version > loadedVersion) {
        versionBumped = true;
      }
    } catch {
      // Polling is best-effort; ignore transient errors.
    }
  }

  function toggleMulti(questionId: string, optionId: string, checked: boolean) {
    const current = multiAnswers[questionId] ?? [];
    if (checked) {
      if (!current.includes(optionId)) {
        multiAnswers[questionId] = [...current, optionId];
      }
      return;
    }
    multiAnswers[questionId] = current.filter((id) => id !== optionId);
  }

  function setSingleMulti(questionId: string, optionId: string) {
    multiAnswers[questionId] = [optionId];
  }

  function buildAnswers(): SurveyAnswerInput[] | null {
    if (!survey) return null;
    const out: SurveyAnswerInput[] = [];
    for (const q of survey.questions) {
      if (q.type === 'SURVEY_QUESTION_TYPE_TEXT') {
        const v = (textAnswers[q.id] ?? '').trim();
        if (q.required && v.length === 0) {
          validationError = `Please answer "${q.prompt}".`;
          return null;
        }
        if (v.length > 0) out.push({ questionId: q.id, text: textAnswers[q.id] ?? '' });
        continue;
      }
      if (q.type === 'SURVEY_QUESTION_TYPE_RATING') {
        const r = ratingAnswers[q.id];
        if (q.required && (r === undefined || r < 1 || r > 5)) {
          validationError = `Please rate "${q.prompt}".`;
          return null;
        }
        if (r !== undefined) out.push({ questionId: q.id, rating: r });
        continue;
      }
      if (q.type === 'SURVEY_QUESTION_TYPE_MULTI_CHOICE') {
        const ids = multiAnswers[q.id] ?? [];
        if (q.required && ids.length === 0) {
          validationError = `Please choose an option for "${q.prompt}".`;
          return null;
        }
        if (ids.length > 0) {
          out.push({ questionId: q.id, multiChoice: { optionIds: ids } });
        }
      }
    }
    return out;
  }

  async function handleSubmit(event: Event) {
    event.preventDefault();
    if (!survey || !applicant || submitting || submitted) return;
    validationError = null;
    submitError = null;
    const answers = buildAnswers();
    if (answers === null) return;
    submitting = true;
    try {
      // Submit always pins to the *originally loaded* surveyId — PRD §5.6
      // version bumps mid-fill must not invalidate an in-flight submit.
      await submitSurveyResponse(config, applicant.playtestId, survey.id, answers);
      submitted = true;
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        navigate(playtestPath(slug));
        return;
      }
      // PRD §5.6 / errors.md row 31: AlreadyExists is the duplicate-submit
      // path — the server returns no body. Render the same thanks view as
      // a fresh success (no submitted-answer echo).
      if (err instanceof ApiError && err.status === 409) {
        submitted = true;
        return;
      }
      if (err instanceof ApiError && err.status === 400) {
        submitError = err.message.replace(/^\d+:\s*/, '');
        return;
      }
      submitError = 'Could not submit your response — please try again.';
    } finally {
      submitting = false;
    }
  }

  load();

  $effect(() => {
    return () => {
      if (pollHandle) {
        clearInterval(pollHandle);
        pollHandle = null;
      }
    };
  });
</script>

{#if loadError}
  <main class="mx-auto w-full max-w-2xl px-4 py-10">
    <p class="rounded-lg border border-red-200 bg-red-50 p-4 text-red-800" role="alert">
      {loadError}
    </p>
  </main>
{:else if submitted}
  <Card>
    <div class="space-y-4 px-8 py-8 text-center">
      <div class="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-green-100">
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
      <h1 class="text-2xl font-bold text-slate-900">Thanks — your response is recorded.</h1>
      <p class="text-sm text-slate-600">We've logged your survey. You're all set.</p>
    </div>
  </Card>
{:else if !survey || !applicant || !playtest}
  <main class="mx-auto w-full max-w-2xl px-4 py-10">
    <p class="text-slate-500">Loading…</p>
  </main>
{:else}
  <Card>
    <form class="space-y-6 px-8 py-7" onsubmit={handleSubmit}>
      <header>
        <h1 class="text-2xl font-bold text-slate-900">{playtest.title} — survey</h1>
        <p class="mt-1 text-sm text-slate-600">Help us out with a quick post-playtest survey.</p>
      </header>
      <hr class="border-slate-200" />
      {#if versionBumped}
        <Banner tone="warn" testid="survey-version-bumped">
          The studio updated this survey while you were filling it in. Your answers will still be
          recorded against the version you started with — feel free to keep going.
        </Banner>
      {/if}
      <div class="space-y-5">
        {#each survey.questions as q (q.id)}
          <fieldset class="space-y-2 rounded-lg border border-slate-200 p-4">
            <legend class="text-sm font-medium text-slate-800">
              {q.prompt}
              {#if q.required}<span class="text-red-600" aria-label="required">*</span>{/if}
            </legend>
            {#if q.type === 'SURVEY_QUESTION_TYPE_TEXT'}
              <textarea
                class="w-full rounded-lg border border-slate-300 p-2 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                rows="4"
                maxlength="4000"
                aria-label={q.prompt}
                bind:value={textAnswers[q.id]}
              ></textarea>
            {:else if q.type === 'SURVEY_QUESTION_TYPE_RATING'}
              <div class="flex gap-3">
                {#each [1, 2, 3, 4, 5] as r (r)}
                  <label class="flex items-center gap-1 text-sm">
                    <input
                      type="radio"
                      name={`rating-${q.id}`}
                      value={r}
                      checked={ratingAnswers[q.id] === r}
                      onchange={() => (ratingAnswers[q.id] = r)}
                      class="h-4 w-4 text-indigo-600 focus:ring-indigo-500"
                    />
                    <span>{r}</span>
                  </label>
                {/each}
              </div>
            {:else if q.type === 'SURVEY_QUESTION_TYPE_MULTI_CHOICE'}
              <div class="space-y-1">
                {#each q.options ?? [] as opt (opt.id)}
                  <label class="flex items-center gap-2 text-sm">
                    {#if q.allowMultiple}
                      <input
                        type="checkbox"
                        checked={(multiAnswers[q.id] ?? []).includes(opt.id)}
                        onchange={(e) =>
                          toggleMulti(q.id, opt.id, (e.currentTarget as HTMLInputElement).checked)}
                        class="h-4 w-4 rounded text-indigo-600 focus:ring-indigo-500"
                      />
                    {:else}
                      <input
                        type="radio"
                        name={`multi-${q.id}`}
                        checked={(multiAnswers[q.id] ?? [])[0] === opt.id}
                        onchange={() => setSingleMulti(q.id, opt.id)}
                        class="h-4 w-4 text-indigo-600 focus:ring-indigo-500"
                      />
                    {/if}
                    <span>{opt.label}</span>
                  </label>
                {/each}
              </div>
            {/if}
          </fieldset>
        {/each}
      </div>
      {#if validationError}
        <p class="text-sm text-red-700" role="alert">{validationError}</p>
      {/if}
      {#if submitError}
        <p class="text-sm text-red-700" role="alert">{submitError}</p>
      {/if}
      <button
        type="submit"
        disabled={submitting}
        class="w-full rounded-lg bg-indigo-600 px-4 py-3 font-medium text-white shadow-sm transition hover:bg-indigo-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-500 disabled:opacity-60"
      >
        {submitting ? 'Submitting…' : 'Submit response'}
      </button>
    </form>
  </Card>
{/if}
