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

const playtestNoNda = (slug: string) =>
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
    },
  });

const playtestADT = (slug: string) =>
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

  it('does not show the Discord invite link on APPROVED even when configured', async () => {
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
    render(Pending, {
      config: { ...config, discordInviteUrl: 'https://discord.gg/playtesthub-demo' },
      slug: 'demo',
    });
    await screen.findByText(/you're approved/i);
    expect(screen.queryByTestId('discord-invite-link')).toBeNull();
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
        url: 'https://cdn.example.com/builds/abc.zip',
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
        url: 'https://example.com/fallback.zip',
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
});
