import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import Survey from '../src/routes/Survey.svelte';
import { setAccessToken } from '../src/lib/auth';
import { pendingPath, playtestPath } from '../src/lib/router';
import type { Config } from '../src/lib/config';

const config: Config = {
  grpcGatewayUrl: 'https://api.example.com/playtesthub',
  iamBaseUrl: 'https://iam.example.com',
  discordClientId: 'client-xyz',
};

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });

const empty = (status: number) =>
  new Response('', { status, headers: { 'Content-Type': 'application/json' } });

type FetchCall = { url: string; init: RequestInit };

type RouteSpec = {
  playerPlaytest?: () => Response;
  applicant?: () => Response;
  survey?: () => Response;
  submit?: (call: FetchCall) => Response;
};

const stubFetch = (routes: RouteSpec): { calls: FetchCall[]; fn: ReturnType<typeof vi.fn> } => {
  const calls: FetchCall[] = [];
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const call: FetchCall = { url, init: init ?? {} };
    calls.push(call);
    if (url.includes('/survey:submit')) return routes.submit?.(call) ?? json(200, { response: {} });
    if (/\/survey$/.test(url)) return routes.survey?.() ?? json(404, {});
    if (/\/applicant(\?|$)/.test(url)) return routes.applicant?.() ?? json(404, {});
    if (/\/playtest|\/v1\/player\/playtests\/[^/]+$/.test(url)) {
      return routes.playerPlaytest?.() ?? json(404, {});
    }
    return routes.playerPlaytest?.() ?? json(404, {});
  });
  vi.stubGlobal('fetch', fn);
  return { calls, fn };
};

const playerPlaytestApproved = (overrides: Partial<{
  surveyId: string | null;
  ndaRequired: boolean;
  currentNdaVersionHash: string;
}> = {}) =>
  json(200, {
    playtest: {
      slug: 'demo',
      title: 'Demo',
      description: 'd',
      status: 'PLAYTEST_STATUS_OPEN',
      ndaRequired: overrides.ndaRequired ?? false,
      ndaText: '',
      currentNdaVersionHash: overrides.currentNdaVersionHash ?? '',
      surveyId:
        overrides.surveyId === undefined
          ? 'srv-1'
          : overrides.surveyId === null
            ? undefined
            : overrides.surveyId,
      distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
    },
  });

const applicantApproved = (overrides: Partial<{ ndaVersionHash: string }> = {}) =>
  json(200, {
    applicant: {
      id: 'a1',
      playtestId: 'pt-1',
      status: 'APPLICANT_STATUS_APPROVED',
      ndaVersionHash: overrides.ndaVersionHash ?? '',
    },
  });

const baseSurvey = (overrides: { id?: string; version?: number } = {}) =>
  json(200, {
    survey: {
      id: overrides.id ?? 'srv-1',
      playtestId: 'pt-1',
      version: overrides.version ?? 1,
      questions: [
        {
          id: 'q-text',
          type: 'SURVEY_QUESTION_TYPE_TEXT',
          prompt: 'What did you think?',
          required: true,
        },
        {
          id: 'q-rating',
          type: 'SURVEY_QUESTION_TYPE_RATING',
          prompt: 'Rate the experience',
          required: true,
        },
        {
          id: 'q-multi',
          type: 'SURVEY_QUESTION_TYPE_MULTI_CHOICE',
          prompt: 'Pick the genres you want next',
          required: false,
          allowMultiple: true,
          options: [
            { id: 'opt-rpg', label: 'RPG' },
            { id: 'opt-fps', label: 'FPS' },
          ],
        },
      ],
    },
  });

beforeEach(() => {
  sessionStorage.clear();
  window.location.hash = '';
});

afterEach(() => {
  sessionStorage.clear();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe('Survey route', () => {
  it('redirects to landing when no access token is present', async () => {
    stubFetch({});
    render(Survey, { config, slug: 'demo' });
    await vi.waitFor(() => {
      expect(window.location.hash).toBe(`#${playtestPath('demo')}`);
    });
  });

  it('redirects to pending when applicant is not APPROVED', async () => {
    setAccessToken('tok');
    stubFetch({
      playerPlaytest: () => playerPlaytestApproved(),
      applicant: () =>
        json(200, {
          applicant: { id: 'a1', playtestId: 'pt-1', status: 'APPLICANT_STATUS_PENDING' },
        }),
    });
    render(Survey, { config, slug: 'demo' });
    await vi.waitFor(() => {
      expect(window.location.hash).toBe(`#${pendingPath('demo')}`);
    });
  });

  it('redirects to pending when NDA re-accept is required', async () => {
    setAccessToken('tok');
    stubFetch({
      playerPlaytest: () =>
        playerPlaytestApproved({ ndaRequired: true, currentNdaVersionHash: 'sha-v2' }),
      applicant: () => applicantApproved({ ndaVersionHash: 'sha-v1' }),
    });
    render(Survey, { config, slug: 'demo' });
    await vi.waitFor(() => {
      expect(window.location.hash).toBe(`#${pendingPath('demo')}`);
    });
  });

  it('redirects to pending when surveyId is unset on the playtest', async () => {
    setAccessToken('tok');
    stubFetch({
      playerPlaytest: () => playerPlaytestApproved({ surveyId: null }),
      applicant: () => applicantApproved(),
    });
    render(Survey, { config, slug: 'demo' });
    await vi.waitFor(() => {
      expect(window.location.hash).toBe(`#${pendingPath('demo')}`);
    });
  });

  it('renders typed widgets for text / rating / multi-choice', async () => {
    setAccessToken('tok');
    stubFetch({
      playerPlaytest: () => playerPlaytestApproved(),
      applicant: () => applicantApproved(),
      survey: () => baseSurvey(),
    });
    render(Survey, { config, slug: 'demo' });
    expect(await screen.findByText(/Demo — survey/)).toBeInTheDocument();
    // Text question → textarea (rows=4 + maxlength).
    const textarea = (await screen.findByText(/What did you think/i))
      .closest('fieldset')
      ?.querySelector('textarea');
    expect(textarea).toBeTruthy();
    expect(textarea?.getAttribute('maxlength')).toBe('4000');
    // Rating → 5 radios.
    const ratingFieldset = (await screen.findByText(/Rate the experience/i)).closest('fieldset');
    const ratings = ratingFieldset?.querySelectorAll('input[type=radio]') ?? [];
    expect(ratings.length).toBe(5);
    // Multi-choice with allowMultiple → checkboxes.
    const multiFieldset = (await screen.findByText(/Pick the genres/i)).closest('fieldset');
    const checks = multiFieldset?.querySelectorAll('input[type=checkbox]') ?? [];
    expect(checks.length).toBe(2);
  });

  it('renders single-select multi-choice as radios when allowMultiple is false', async () => {
    setAccessToken('tok');
    stubFetch({
      playerPlaytest: () => playerPlaytestApproved(),
      applicant: () => applicantApproved(),
      survey: () =>
        json(200, {
          survey: {
            id: 'srv-1',
            playtestId: 'pt-1',
            version: 1,
            questions: [
              {
                id: 'q-multi',
                type: 'SURVEY_QUESTION_TYPE_MULTI_CHOICE',
                prompt: 'Favourite difficulty',
                required: true,
                allowMultiple: false,
                options: [
                  { id: 'opt-easy', label: 'Easy' },
                  { id: 'opt-hard', label: 'Hard' },
                ],
              },
            ],
          },
        }),
    });
    render(Survey, { config, slug: 'demo' });
    const fieldset = (await screen.findByText(/Favourite difficulty/i)).closest('fieldset');
    const radios = fieldset?.querySelectorAll('input[type=radio]') ?? [];
    expect(radios.length).toBe(2);
    expect(fieldset?.querySelectorAll('input[type=checkbox]').length).toBe(0);
  });

  it('blocks submit and shows validation when a required text answer is empty', async () => {
    setAccessToken('tok');
    const { calls } = stubFetch({
      playerPlaytest: () => playerPlaytestApproved(),
      applicant: () => applicantApproved(),
      survey: () => baseSurvey(),
    });
    render(Survey, { config, slug: 'demo' });
    const submit = await screen.findByRole('button', { name: /submit response/i });
    await fireEvent.click(submit);
    expect(await screen.findByRole('alert')).toHaveTextContent(/What did you think/);
    expect(calls.some((c) => c.url.includes(':submit'))).toBe(false);
  });

  it('submits answers as the typed oneof shape and shows the thanks view on success', async () => {
    setAccessToken('tok');
    const { calls } = stubFetch({
      playerPlaytest: () => playerPlaytestApproved(),
      applicant: () => applicantApproved(),
      survey: () => baseSurvey(),
      submit: () =>
        json(200, {
          response: {
            id: 'r-1',
            playtestId: 'pt-1',
            userId: 'u-1',
            surveyId: 'srv-1',
            answers: [],
          },
        }),
    });
    render(Survey, { config, slug: 'demo' });
    // Fill the text question.
    const textarea = (await screen.findByText(/What did you think/i))
      .closest('fieldset')
      ?.querySelector('textarea') as HTMLTextAreaElement;
    await fireEvent.input(textarea, { target: { value: 'great game' } });
    // Click rating=4.
    const ratingFieldset = (await screen.findByText(/Rate the experience/i)).closest('fieldset');
    const fourth = ratingFieldset?.querySelectorAll('input[type=radio]')[3] as HTMLInputElement;
    await fireEvent.click(fourth);
    // Tick FPS only.
    const multiFieldset = (await screen.findByText(/Pick the genres/i)).closest('fieldset');
    const fpsCheck = (multiFieldset?.querySelectorAll('input[type=checkbox]') ?? [])[1] as HTMLInputElement;
    await fireEvent.click(fpsCheck);
    // Submit.
    await fireEvent.click(screen.getByRole('button', { name: /submit response/i }));
    expect(await screen.findByText(/your response is recorded/i)).toBeInTheDocument();
    const submitCall = calls.find((c) => c.url.includes(':submit'));
    expect(submitCall).toBeTruthy();
    expect(submitCall!.url).toBe(
      'https://api.example.com/playtesthub/v1/player/playtests/pt-1/survey:submit',
    );
    const body = JSON.parse(String(submitCall!.init.body));
    expect(body.surveyId).toBe('srv-1');
    expect(body.answers).toEqual([
      { questionId: 'q-text', text: 'great game' },
      { questionId: 'q-rating', rating: 4 },
      { questionId: 'q-multi', multiChoice: { optionIds: ['opt-fps'] } },
    ]);
  });

  it('treats AlreadyExists (409) as the duplicate-submit thanks view (PRD §5.6 / errors.md row 31)', async () => {
    setAccessToken('tok');
    stubFetch({
      playerPlaytest: () => playerPlaytestApproved(),
      applicant: () => applicantApproved(),
      survey: () => baseSurvey(),
      submit: () => empty(409),
    });
    render(Survey, { config, slug: 'demo' });
    const textarea = (await screen.findByText(/What did you think/i))
      .closest('fieldset')
      ?.querySelector('textarea') as HTMLTextAreaElement;
    await fireEvent.input(textarea, { target: { value: 'replay' } });
    const ratingFieldset = (await screen.findByText(/Rate the experience/i)).closest('fieldset');
    const fifth = ratingFieldset?.querySelectorAll('input[type=radio]')[4] as HTMLInputElement;
    await fireEvent.click(fifth);
    await fireEvent.click(screen.getByRole('button', { name: /submit response/i }));
    expect(await screen.findByText(/your response is recorded/i)).toBeInTheDocument();
    // Must not echo any submitted answer.
    expect(screen.queryByText(/replay/)).toBeNull();
  });

  it('redirects to landing on 401 from submit', async () => {
    setAccessToken('stale');
    stubFetch({
      playerPlaytest: () => playerPlaytestApproved(),
      applicant: () => applicantApproved(),
      survey: () => baseSurvey(),
      submit: () => json(401, { message: 'bad token' }),
    });
    render(Survey, { config, slug: 'demo' });
    const textarea = (await screen.findByText(/What did you think/i))
      .closest('fieldset')
      ?.querySelector('textarea') as HTMLTextAreaElement;
    await fireEvent.input(textarea, { target: { value: 'foo' } });
    const ratingFieldset = (await screen.findByText(/Rate the experience/i)).closest('fieldset');
    const first = ratingFieldset?.querySelectorAll('input[type=radio]')[0] as HTMLInputElement;
    await fireEvent.click(first);
    await fireEvent.click(screen.getByRole('button', { name: /submit response/i }));
    await vi.waitFor(() => {
      expect(window.location.hash).toBe(`#${playtestPath('demo')}`);
    });
  });

  it('shows the version-bump banner when a poll observes a newer version, and still submits against the loaded id', async () => {
    setAccessToken('tok');
    vi.useFakeTimers();
    let surveyVersion = 1;
    const { calls } = stubFetch({
      playerPlaytest: () => playerPlaytestApproved(),
      applicant: () => applicantApproved(),
      survey: () => baseSurvey({ id: 'srv-1', version: surveyVersion }),
      submit: () => json(200, { response: { id: 'r-1' } }),
    });
    render(Survey, { config, slug: 'demo' });
    // Wait for first paint.
    await screen.findByText(/Demo — survey/);
    // Bump version on the server then trigger the 30s poll.
    surveyVersion = 2;
    await vi.advanceTimersByTimeAsync(30_000);
    expect(await screen.findByTestId('survey-version-bumped')).toBeInTheDocument();
    // Submit still uses the originally loaded surveyId.
    vi.useRealTimers();
    const textarea = (await screen.findByText(/What did you think/i))
      .closest('fieldset')
      ?.querySelector('textarea') as HTMLTextAreaElement;
    await fireEvent.input(textarea, { target: { value: 'still works' } });
    const ratingFieldset = (await screen.findByText(/Rate the experience/i)).closest('fieldset');
    const third = ratingFieldset?.querySelectorAll('input[type=radio]')[2] as HTMLInputElement;
    await fireEvent.click(third);
    await fireEvent.click(screen.getByRole('button', { name: /submit response/i }));
    await screen.findByText(/your response is recorded/i);
    const submitCall = calls.find((c) => c.url.includes(':submit'))!;
    expect(JSON.parse(String(submitCall.init.body)).surveyId).toBe('srv-1');
  });
});
