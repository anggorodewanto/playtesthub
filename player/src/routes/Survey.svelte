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

<main class="mx-auto max-w-2xl p-6 md:p-10">
  {#if loadError}
    <p class="rounded border border-red-200 bg-red-50 p-4 text-red-800" role="alert">{loadError}</p>
  {:else if submitted}
    <h1 class="text-2xl font-semibold">Thanks — your response is recorded.</h1>
    <p class="mt-3 text-slate-700">We've logged your survey. You're all set.</p>
  {:else if !survey || !applicant || !playtest}
    <p class="text-slate-500">Loading…</p>
  {:else}
    <h1 class="text-2xl font-semibold">{playtest.title} — survey</h1>
    <p class="mt-2 text-sm text-slate-600">
      Help us out with a quick post-playtest survey.
    </p>
    {#if versionBumped}
      <p
        class="mt-4 rounded border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900"
        role="status"
        data-testid="survey-version-bumped"
      >
        The studio updated this survey while you were filling it in. Your answers will still be
        recorded against the version you started with — feel free to keep going.
      </p>
    {/if}
    <form class="mt-6 space-y-6" onsubmit={handleSubmit}>
      {#each survey.questions as q (q.id)}
        <fieldset class="space-y-2 border border-slate-200 rounded p-4">
          <legend class="text-sm font-medium text-slate-800">
            {q.prompt}
            {#if q.required}<span class="text-red-600" aria-label="required">*</span>{/if}
          </legend>
          {#if q.type === 'SURVEY_QUESTION_TYPE_TEXT'}
            <textarea
              class="w-full rounded border border-slate-300 p-2 text-sm"
              rows="4"
              maxlength="4000"
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
                    />
                  {:else}
                    <input
                      type="radio"
                      name={`multi-${q.id}`}
                      checked={(multiAnswers[q.id] ?? [])[0] === opt.id}
                      onchange={() => setSingleMulti(q.id, opt.id)}
                    />
                  {/if}
                  <span>{opt.label}</span>
                </label>
              {/each}
            </div>
          {/if}
        </fieldset>
      {/each}
      {#if validationError}
        <p class="text-sm text-red-700" role="alert">{validationError}</p>
      {/if}
      {#if submitError}
        <p class="text-sm text-red-700" role="alert">{submitError}</p>
      {/if}
      <button
        type="submit"
        disabled={submitting}
        class="rounded bg-slate-900 px-4 py-2 text-white hover:bg-slate-700 disabled:opacity-50"
      >
        {submitting ? 'Submitting…' : 'Submit response'}
      </button>
    </form>
  {/if}
</main>
