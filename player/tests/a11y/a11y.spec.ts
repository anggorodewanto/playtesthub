import { expect, test, type Page, type Route } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

// PRD §6 Accessibility — zero-critical-violation gate over the six player
// pages. Rule scope is the four WCAG tag sets the PRD pins; everything
// outside that scope is informational and not gated here.
const WCAG_TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'];

const SLUG = 'a11y-fixture';

const STUB_CONFIG = {
  grpcGatewayUrl: 'http://127.0.0.1:65535/api',
  iamBaseUrl: 'http://127.0.0.1:65535',
  discordClientId: 'fixture-client-id',
};

const PUBLIC_PLAYTEST = {
  id: 'pt-fixture',
  slug: SLUG,
  title: 'Fixture Playtest',
  description: 'A11y fixture playtest used only for axe-core auditing.',
  status: 'PLAYTEST_STATUS_OPEN',
  ndaRequired: true,
  distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
};

const PLAYER_PLAYTEST = {
  ...PUBLIC_PLAYTEST,
  ndaText: 'You agree not to share keys, screenshots, or playtest details outside the program.',
  currentNdaVersionHash: 'sha256:current',
  surveyId: 'sv-fixture',
};

const APPROVED_APPLICANT = {
  id: 'ap-fixture',
  status: 'APPLICANT_STATUS_APPROVED',
  playtestId: PLAYER_PLAYTEST.id,
  ndaVersionHash: 'sha256:current',
  approvedAt: '2026-05-07T00:00:00Z',
  grantedCodeId: 'cd-fixture',
};

const STALE_NDA_APPLICANT = {
  ...APPROVED_APPLICANT,
  status: 'APPLICANT_STATUS_PENDING',
  ndaVersionHash: 'sha256:stale',
  grantedCodeId: undefined,
  approvedAt: undefined,
};

const GRANTED_CODE = {
  id: 'cd-fixture',
  value: 'STEAMKEY-FIXTURE-12345',
  distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
};

const SURVEY = {
  id: 'sv-fixture',
  version: 1,
  questions: [
    {
      id: 'q-text',
      type: 'SURVEY_QUESTION_TYPE_TEXT',
      prompt: 'How was your experience?',
      required: false,
    },
    {
      id: 'q-rating',
      type: 'SURVEY_QUESTION_TYPE_RATING',
      prompt: 'Rate the build 1–5.',
      required: true,
    },
    {
      id: 'q-multi',
      type: 'SURVEY_QUESTION_TYPE_MULTI_CHOICE',
      prompt: 'Which platforms did you play on?',
      required: false,
      allowMultiple: true,
      options: [
        { id: 'opt-pc', label: 'PC' },
        { id: 'opt-mac', label: 'macOS' },
      ],
    },
  ],
};

type RouteHandler = (route: Route) => Promise<void> | void;

async function stubBackend(page: Page, overrides: Record<string, RouteHandler> = {}): Promise<void> {
  await page.route('**/config.json', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(STUB_CONFIG) });
  });

  const defaults: Record<string, RouteHandler> = {
    [`/v1/public/playtests/${SLUG}`]: (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ playtest: PUBLIC_PLAYTEST }) }),
    [`/v1/player/playtests/${SLUG}`]: (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ playtest: PLAYER_PLAYTEST }) }),
    [`/v1/player/playtests/${SLUG}/applicant`]: (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ applicant: APPROVED_APPLICANT }) }),
    [`/v1/player/playtests/${PLAYER_PLAYTEST.id}/grantedCode`]: (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(GRANTED_CODE) }),
    [`/v1/player/playtests/${PLAYER_PLAYTEST.id}/survey`]: (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ survey: SURVEY }) }),
    [`/v1/player/playtests/${PLAYER_PLAYTEST.id}/survey:submit`]: (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: '{}' }),
    ...overrides,
  };

  await page.route('**/v1/**', async (route) => {
    const url = new URL(route.request().url());
    for (const [path, handler] of Object.entries(defaults)) {
      if (url.pathname.endsWith(path)) {
        await handler(route);
        return;
      }
    }
    await route.fulfill({ status: 404, contentType: 'application/json', body: '{"message":"unmocked"}' });
  });
}

async function seedToken(page: Page): Promise<void> {
  await page.addInitScript(() => {
    sessionStorage.setItem('playtesthub.accessToken', 'fixture-token');
  });
}

async function assertNoCriticalViolations(page: Page, label: string): Promise<void> {
  const result = await new AxeBuilder({ page }).withTags(WCAG_TAGS).analyze();
  const critical = result.violations.filter((v) => v.impact === 'critical');
  expect(critical, `[${label}] critical a11y violations:\n${JSON.stringify(critical, null, 2)}`).toEqual([]);
}

test.describe('player a11y (axe-core / WCAG 2.1 A+AA)', () => {
  test('landing', async ({ page }) => {
    await stubBackend(page);
    await page.goto(`/#/playtest/${SLUG}`);
    await expect(page.getByRole('heading', { name: PUBLIC_PLAYTEST.title })).toBeVisible();
    await assertNoCriticalViolations(page, 'landing');
  });

  test('signup', async ({ page }) => {
    await seedToken(page);
    await stubBackend(page);
    await page.goto(`/#/playtest/${SLUG}/signup`);
    await expect(page.getByRole('heading', { name: /Sign-Up Form/i })).toBeVisible();
    await assertNoCriticalViolations(page, 'signup');
  });

  test('NDA accept', async ({ page }) => {
    await seedToken(page);
    await stubBackend(page, {
      [`/v1/player/playtests/${SLUG}/applicant`]: (route) =>
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ applicant: STALE_NDA_APPLICANT }),
        }),
    });
    await page.goto(`/#/playtest/${SLUG}/nda`);
    await expect(page.getByRole('checkbox', { name: /agree/i })).toBeVisible();
    await assertNoCriticalViolations(page, 'nda');
  });

  test('approved code view', async ({ page }) => {
    await seedToken(page);
    await stubBackend(page);
    await page.goto(`/#/playtest/${SLUG}/pending`);
    await expect(page.getByTestId('granted-code-value')).toBeVisible();
    await assertNoCriticalViolations(page, 'pending-approved');
  });

  test('survey form', async ({ page }) => {
    await seedToken(page);
    await stubBackend(page);
    await page.goto(`/#/playtest/${SLUG}/survey`);
    await expect(page.getByRole('heading', { name: /survey/ })).toBeVisible();
    await assertNoCriticalViolations(page, 'survey-form');
  });

  test('post-submit thanks', async ({ page }) => {
    await seedToken(page);
    await stubBackend(page);
    await page.goto(`/#/playtest/${SLUG}/survey`);
    await page.getByRole('radio', { name: '5' }).check();
    await page.getByRole('button', { name: /Submit response/ }).click();
    await expect(page.getByRole('heading', { name: /Thanks/ })).toBeVisible();
    await assertNoCriticalViolations(page, 'survey-thanks');
  });
});
