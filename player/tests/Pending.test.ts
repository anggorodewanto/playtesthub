import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import Pending from '../src/routes/Pending.svelte';
import { setAccessToken } from '../src/lib/auth';
import { playtestPath } from '../src/lib/router';
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

// Pending fetches the player playtest, the applicant row, and (on
// APPROVED) the granted code. Tests route by URL so each call gets the
// right body.
type Routes = {
  playerPlaytest?: Response;
  applicant?: Response;
  grantedCode?: Response;
  adtDownload?: Response;
};
const stubFetchByUrl = (routes: Routes) => {
  const fn = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.includes(':acceptNda')) return json(200, { acceptance: {} });
    if (url.includes('/adtDownload')) return routes.adtDownload ?? json(404, {});
    if (url.includes('/grantedCode')) return routes.grantedCode ?? json(404, {});
    if (/\/applicant(\?|$)/.test(url)) return routes.applicant ?? json(404, {});
    return routes.playerPlaytest ?? json(404, {});
  });
  vi.stubGlobal('fetch', fn);
  return fn;
};

const playtestNoNda = (slug: string, extras: Record<string, unknown> = {}) =>
  json(200, {
    playtest: {
      slug,
      title: 't',
      description: 'd',
      status: 'PLAYTEST_STATUS_OPEN',
      ndaRequired: false,
      ndaText: '',
      currentNdaVersionHash: '',
      distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
      ...extras,
    },
  });

const playtestADT = (slug: string, extras: Record<string, unknown> = {}) =>
  json(200, {
    playtest: {
      slug,
      title: 't',
      description: 'd',
      status: 'PLAYTEST_STATUS_OPEN',
      ndaRequired: false,
      ndaText: '',
      currentNdaVersionHash: '',
      distributionModel: 'DISTRIBUTION_MODEL_ADT',
      ...extras,
    },
  });

const playtestNdaV2 = (slug: string) =>
  json(200, {
    playtest: {
      slug,
      title: 't',
      description: 'd',
      status: 'PLAYTEST_STATUS_OPEN',
      ndaRequired: true,
      ndaText: 'v2',
      currentNdaVersionHash: 'sha-v2',
      distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
    },
  });

beforeEach(() => {
  sessionStorage.clear();
  window.location.hash = '';
});

afterEach(() => {
  sessionStorage.clear();
  vi.restoreAllMocks();
});

describe('Pending', () => {
  it('renders the pending review copy when status=PENDING', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: playtestNoNda('demo'),
      applicant: json(200, {
        applicant: { id: 'a1', playtestId: 'pt-1', status: 'APPLICANT_STATUS_PENDING' },
      }),
    });
    render(Pending, { config, slug: 'demo' });
    expect(await screen.findByText(/under review/i)).toBeInTheDocument();
    expect(screen.queryByTestId('discord-invite-link')).toBeNull();
  });

  it('shows the Discord invite link on PENDING when configured', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: playtestNoNda('demo'),
      applicant: json(200, {
        applicant: { id: 'a1', playtestId: 'pt-1', status: 'APPLICANT_STATUS_PENDING' },
      }),
    });
    const inviteUrl = 'https://discord.gg/playtesthub-demo';
    render(Pending, {
      config: { ...config, discordInviteUrl: inviteUrl },
      slug: 'demo',
    });
    const link = (await screen.findByTestId('discord-invite-link')) as HTMLAnchorElement;
    expect(link.href).toBe(inviteUrl);
    expect(link.target).toBe('_blank');
    expect(link.rel).toContain('noopener');
  });

  it('shows the Discord-server join link on APPROVED code-distribution when discordInviteUrl is configured', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: playtestNoNda('demo'),
      applicant: json(200, {
        applicant: {
          id: 'a1',
          playtestId: 'pt-1',
          status: 'APPLICANT_STATUS_APPROVED',
          ndaVersionHash: '',
        },
      }),
      grantedCode: json(200, {
        value: 'STEAM-AAAA-BBBB-CCCC',
        distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
      }),
    });
    const inviteUrl = 'https://discord.gg/playtesthub-demo';
    render(Pending, {
      config: { ...config, discordInviteUrl: inviteUrl },
      slug: 'demo',
    });
    await screen.findByText(/you're approved/i);
    const link = (await screen.findByTestId('discord-invite-link-approved')) as HTMLAnchorElement;
    expect(link.href).toBe(inviteUrl);
    expect(link.target).toBe('_blank');
    expect(link.rel).toContain('noopener');
    // PENDING-specific invite link must not appear on APPROVED — it uses a
    // different testid.
    expect(screen.queryByTestId('discord-invite-link')).toBeNull();
  });

  it('omits the Discord-server join link on APPROVED code-distribution when discordInviteUrl is not configured', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: playtestNoNda('demo'),
      applicant: json(200, {
        applicant: {
          id: 'a1',
          playtestId: 'pt-1',
          status: 'APPLICANT_STATUS_APPROVED',
          ndaVersionHash: '',
        },
      }),
      grantedCode: json(200, {
        value: 'STEAM-AAAA-BBBB-CCCC',
        distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
      }),
    });
    render(Pending, { config, slug: 'demo' });
    await screen.findByText(/you're approved/i);
    expect(screen.queryByTestId('discord-invite-link-approved')).toBeNull();
  });

  it('shows generic not-selected copy on REJECTED (no rejection reason shown)', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: playtestNoNda('demo'),
      applicant: json(200, {
        applicant: {
          id: 'a1',
          playtestId: 'pt-1',
          status: 'APPLICANT_STATUS_REJECTED',
          // rejectionReason is admin-only per PRD §5.4 — must never render.
        },
      }),
    });
    render(Pending, { config, slug: 'demo' });
    expect(await screen.findByText(/not selected/i)).toBeInTheDocument();
  });

  it('redirects to landing on 401 (session expired)', async () => {
    setAccessToken('stale');
    stubFetchByUrl({
      playerPlaytest: json(401, { message: 'bad token' }),
      applicant: json(401, { message: 'bad token' }),
    });
    render(Pending, { config, slug: 'demo' });
    await vi.waitFor(() => {
      expect(window.location.hash).toBe(`#${playtestPath('demo')}`);
    });
  });

  it('redirects PENDING applicants to NDA route when ndaVersionHash diverges', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: playtestNdaV2('demo'),
      applicant: json(200, {
        applicant: {
          id: 'a1',
          playtestId: 'pt-1',
          status: 'APPLICANT_STATUS_PENDING',
          ndaVersionHash: 'sha-v1',
        },
      }),
    });
    render(Pending, { config, slug: 'demo' });
    await vi.waitFor(() => {
      expect(window.location.hash).toBe('#/playtest/demo/nda');
    });
  });

  it('renders STEAM_KEYS code with redeem-on-Steam instructions when APPROVED', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: playtestNoNda('demo'),
      applicant: json(200, {
        applicant: {
          id: 'a1',
          playtestId: 'pt-1',
          status: 'APPLICANT_STATUS_APPROVED',
          ndaVersionHash: '',
        },
      }),
      grantedCode: json(200, {
        value: 'STEAM-AAAA-BBBB-CCCC',
        distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
      }),
    });
    render(Pending, { config, slug: 'demo' });
    expect(await screen.findByText(/you're approved/i)).toBeInTheDocument();
    const input = (await screen.findByTestId('granted-code-value')) as HTMLInputElement;
    expect(input.value).toBe('STEAM-AAAA-BBBB-CCCC');
    expect(screen.getByText(/Activate a Product on Steam/)).toBeInTheDocument();
  });

  it('renders AGS_CAMPAIGN code with PublicRedeemCode instructions when APPROVED', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: playtestNoNda('demo'),
      applicant: json(200, {
        applicant: {
          id: 'a1',
          playtestId: 'pt-1',
          status: 'APPLICANT_STATUS_APPROVED',
          ndaVersionHash: '',
        },
      }),
      grantedCode: json(200, {
        value: 'AGS-XXXX-YYYY',
        distributionModel: 'DISTRIBUTION_MODEL_AGS_CAMPAIGN',
      }),
    });
    render(Pending, { config, slug: 'demo' });
    const input = (await screen.findByTestId('granted-code-value')) as HTMLInputElement;
    expect(input.value).toBe('AGS-XXXX-YYYY');
    expect(screen.getByText(/PublicRedeemCode/)).toBeInTheDocument();
  });

  it('keeps the granted code visible when re-accept is required (PRD §5.3)', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: playtestNdaV2('demo'),
      applicant: json(200, {
        applicant: {
          id: 'a1',
          playtestId: 'pt-1',
          status: 'APPLICANT_STATUS_APPROVED',
          ndaVersionHash: 'sha-v1', // diverges from current sha-v2
        },
      }),
      grantedCode: json(200, {
        value: 'STILL-VISIBLE-CODE',
        distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
      }),
    });
    render(Pending, { config, slug: 'demo' });
    const input = (await screen.findByTestId('granted-code-value')) as HTMLInputElement;
    expect(input.value).toBe('STILL-VISIBLE-CODE');
    expect(screen.getByText(/NDA was updated/i)).toBeInTheDocument();
    // Must not redirect — APPROVED stays put even with re-accept required.
    expect(window.location.hash).not.toBe('#/playtest/demo/nda');
  });

  it('renders the ADT download card with an "issued" URL when distribution=ADT', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: playtestADT('demo'),
      applicant: json(200, {
        applicant: {
          id: 'a1',
          playtestId: 'pt-1',
          status: 'APPLICANT_STATUS_APPROVED',
          ndaVersionHash: '',
        },
      }),
      adtDownload: json(200, {
        urls: ['https://cdn.example.com/builds/abc.zip'],
        expiresAt: '2026-05-21T00:00:00Z',
        source: 'issued',
      }),
    });
    render(Pending, { config, slug: 'demo' });
    const link = (await screen.findByTestId('adt-download-link')) as HTMLAnchorElement;
    expect(link.href).toBe('https://cdn.example.com/builds/abc.zip');
    expect(screen.getByTestId('adt-download-expiry')).toHaveTextContent(/Link expires/);
    expect(screen.queryByTestId('adt-download-source')).toBeNull();
  });

  it('shows the Discord-server join link on APPROVED ADT when discordInviteUrl is configured', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: playtestADT('demo'),
      applicant: json(200, {
        applicant: {
          id: 'a1',
          playtestId: 'pt-1',
          status: 'APPLICANT_STATUS_APPROVED',
          ndaVersionHash: '',
        },
      }),
      adtDownload: json(200, {
        urls: ['https://cdn.example.com/builds/abc.zip'],
        source: 'issued',
      }),
    });
    const inviteUrl = 'https://discord.gg/playtesthub-demo';
    render(Pending, {
      config: { ...config, discordInviteUrl: inviteUrl },
      slug: 'demo',
    });
    const link = (await screen.findByTestId('discord-invite-link-approved')) as HTMLAnchorElement;
    expect(link.href).toBe(inviteUrl);
    expect(link.target).toBe('_blank');
    expect(link.rel).toContain('noopener');
  });

  it('omits the Discord-server join link on APPROVED ADT when discordInviteUrl is not configured', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: playtestADT('demo'),
      applicant: json(200, {
        applicant: {
          id: 'a1',
          playtestId: 'pt-1',
          status: 'APPLICANT_STATUS_APPROVED',
          ndaVersionHash: '',
        },
      }),
      adtDownload: json(200, {
        urls: ['https://cdn.example.com/builds/abc.zip'],
        source: 'issued',
      }),
    });
    render(Pending, { config, slug: 'demo' });
    await screen.findByTestId('adt-download-link');
    expect(screen.queryByTestId('discord-invite-link-approved')).toBeNull();
  });

  it('renders one link per URL when ADT returns a multi-asset build', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: playtestADT('demo'),
      applicant: json(200, {
        applicant: {
          id: 'a1',
          playtestId: 'pt-1',
          status: 'APPLICANT_STATUS_APPROVED',
          ndaVersionHash: '',
        },
      }),
      adtDownload: json(200, {
        urls: [
          'https://cdn.example.com/builds/main.zip',
          'https://cdn.example.com/builds/patch.bin',
          'https://cdn.example.com/builds/manifest.json',
        ],
        source: 'issued',
      }),
    });
    render(Pending, { config, slug: 'demo' });
    const first = (await screen.findByTestId('adt-download-link-0')) as HTMLAnchorElement;
    expect(first.href).toBe('https://cdn.example.com/builds/main.zip');
    const second = screen.getByTestId('adt-download-link-1') as HTMLAnchorElement;
    expect(second.href).toBe('https://cdn.example.com/builds/patch.bin');
    const third = screen.getByTestId('adt-download-link-2') as HTMLAnchorElement;
    expect(third.href).toBe('https://cdn.example.com/builds/manifest.json');
    expect(screen.getByTestId('adt-download-multi-heading')).toHaveTextContent('3 files');
    // Single-URL test-id must NOT be present in multi-URL mode (the
    // single-link branch is skipped when urls.length > 1).
    expect(screen.queryByTestId('adt-download-link')).toBeNull();
  });

  it('labels the fallback source on ADT downloads', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: playtestADT('demo'),
      applicant: json(200, {
        applicant: {
          id: 'a1',
          playtestId: 'pt-1',
          status: 'APPLICANT_STATUS_APPROVED',
          ndaVersionHash: '',
        },
      }),
      adtDownload: json(200, {
        urls: ['https://example.com/fallback.zip'],
        source: 'fallback',
      }),
    });
    render(Pending, { config, slug: 'demo' });
    expect(await screen.findByTestId('adt-download-source')).toHaveTextContent(/Shared playtest download/);
  });

  it('shows an ADT-download error message when fetchAdtDownloadInfo fails (non-401)', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: playtestADT('demo'),
      applicant: json(200, {
        applicant: {
          id: 'a1',
          playtestId: 'pt-1',
          status: 'APPLICANT_STATUS_APPROVED',
          ndaVersionHash: '',
        },
      }),
      adtDownload: json(503, { message: 'service unavailable' }),
    });
    render(Pending, { config, slug: 'demo' });
    expect(await screen.findByRole('alert')).toHaveTextContent(/download link is not available/i);
  });

  it('shows a code-fetch error message when GetGrantedCode fails (non-401)', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: playtestNoNda('demo'),
      applicant: json(200, {
        applicant: {
          id: 'a1',
          playtestId: 'pt-1',
          status: 'APPLICANT_STATUS_APPROVED',
          ndaVersionHash: '',
        },
      }),
      grantedCode: json(503, { message: 'service unavailable' }),
    });
    render(Pending, { config, slug: 'demo' });
    expect(await screen.findByRole('alert')).toHaveTextContent(/not available yet/i);
  });

  // Survey-discovery phase 1 (PRD §5.6): the Pending page surfaces a CTA
  // for the survey when the playtest is configured with one. The CTA
  // flips between "Submit feedback" and "Feedback submitted ✓" based on
  // the server-supplied `surveyResponseSubmittedAt` field so the player
  // sees the right affordance without an extra round-trip.
  it('omits the survey CTA when playtest has no surveyId (STEAM_KEYS)', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: playtestNoNda('demo'),
      applicant: json(200, {
        applicant: {
          id: 'a1',
          playtestId: 'pt-1',
          status: 'APPLICANT_STATUS_APPROVED',
          ndaVersionHash: '',
        },
      }),
      grantedCode: json(200, {
        value: 'STEAM-AAAA',
        distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
      }),
    });
    render(Pending, { config, slug: 'demo' });
    await screen.findByTestId('granted-code-value');
    expect(screen.queryByTestId('survey-cta-link')).toBeNull();
    expect(screen.queryByTestId('survey-cta-submitted')).toBeNull();
  });

  it('renders the survey CTA link when surveyId is set and no response is recorded', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: playtestNoNda('demo', { surveyId: 'srv-1' }),
      applicant: json(200, {
        applicant: {
          id: 'a1',
          playtestId: 'pt-1',
          status: 'APPLICANT_STATUS_APPROVED',
          ndaVersionHash: '',
        },
      }),
      grantedCode: json(200, {
        value: 'STEAM-AAAA',
        distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
      }),
    });
    render(Pending, { config, slug: 'demo' });
    const cta = (await screen.findByTestId('survey-cta-link')) as HTMLAnchorElement;
    // surveyPath is hash-routed — link must include the hash prefix so a
    // plain anchor click triggers a route change.
    expect(cta.getAttribute('href')).toBe('#/playtest/demo/survey');
    expect(screen.queryByTestId('survey-cta-submitted')).toBeNull();
  });

  it('renders the survey CTA submitted label with timestamp when surveyResponseSubmittedAt is set', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: playtestNoNda('demo', { surveyId: 'srv-1' }),
      applicant: json(200, {
        applicant: {
          id: 'a1',
          playtestId: 'pt-1',
          status: 'APPLICANT_STATUS_APPROVED',
          ndaVersionHash: '',
          surveyResponseSubmittedAt: '2026-05-22T09:00:00Z',
        },
      }),
      grantedCode: json(200, {
        value: 'STEAM-AAAA',
        distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
      }),
    });
    render(Pending, { config, slug: 'demo' });
    const submitted = await screen.findByTestId('survey-cta-submitted');
    expect(submitted).toHaveTextContent(/Feedback submitted/);
    expect(screen.queryByTestId('survey-cta-link')).toBeNull();
    const time = submitted.parentElement?.querySelector('time');
    expect(time?.getAttribute('datetime')).toBe('2026-05-22T09:00:00Z');
  });

  it('also shows the survey CTA on ADT-distribution playtests', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: playtestADT('demo', { surveyId: 'srv-1' }),
      applicant: json(200, {
        applicant: {
          id: 'a1',
          playtestId: 'pt-1',
          status: 'APPLICANT_STATUS_APPROVED',
          ndaVersionHash: '',
        },
      }),
      adtDownload: json(200, {
        urls: ['https://cdn.example.com/builds/abc.zip'],
        source: 'issued',
      }),
    });
    render(Pending, { config, slug: 'demo' });
    await screen.findByTestId('adt-download-link');
    const cta = (await screen.findByTestId('survey-cta-link')) as HTMLAnchorElement;
    expect(cta.getAttribute('href')).toBe('#/playtest/demo/survey');
  });

  it('hides the survey CTA when NDA re-accept is required (server bounces submits)', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: playtestNdaV2('demo'),
      applicant: json(200, {
        applicant: {
          id: 'a1',
          playtestId: 'pt-1',
          status: 'APPLICANT_STATUS_APPROVED',
          ndaVersionHash: 'sha-v1', // diverges from current sha-v2
          // Server would attach surveyId on the playtest, but the
          // re-accept-required client guard should hide the CTA
          // anyway.
        },
      }),
      grantedCode: json(200, {
        value: 'STILL-VISIBLE-CODE',
        distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
      }),
    });
    // playtestNdaV2 does not carry surveyId; emulate a surveyed playtest
    // by overriding the response.
    const surveyedNdaV2 = json(200, {
      playtest: {
        slug: 'demo',
        title: 't',
        description: 'd',
        status: 'PLAYTEST_STATUS_OPEN',
        ndaRequired: true,
        ndaText: 'v2',
        currentNdaVersionHash: 'sha-v2',
        distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
        surveyId: 'srv-1',
      },
    });
    stubFetchByUrl({
      playerPlaytest: surveyedNdaV2,
      applicant: json(200, {
        applicant: {
          id: 'a1',
          playtestId: 'pt-1',
          status: 'APPLICANT_STATUS_APPROVED',
          ndaVersionHash: 'sha-v1',
        },
      }),
      grantedCode: json(200, {
        value: 'STILL-VISIBLE-CODE',
        distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
      }),
    });
    render(Pending, { config, slug: 'demo' });
    await screen.findByTestId('granted-code-value');
    expect(screen.queryByTestId('survey-cta-link')).toBeNull();
    expect(screen.queryByTestId('survey-cta-submitted')).toBeNull();
  });
});
