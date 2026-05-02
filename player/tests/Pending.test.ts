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

// Pending fetches both the player playtest (NDA hash) and the applicant
// row so the §5.3 re-accept gate can run before any UI renders. Tests
// route by URL so each call gets the right body.
type Routes = { playerPlaytest?: Response; applicant?: Response };
const stubFetchByUrl = (routes: Routes) => {
  const fn = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.includes(':acceptNda')) return json(200, { acceptance: {} });
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

  it('redirects to NDA route when applicant.ndaVersionHash != currentNdaVersionHash', async () => {
    setAccessToken('tok');
    stubFetchByUrl({
      playerPlaytest: json(200, {
        playtest: {
          slug: 'demo',
          title: 't',
          description: 'd',
          status: 'PLAYTEST_STATUS_OPEN',
          ndaRequired: true,
          ndaText: 'v2',
          currentNdaVersionHash: 'sha-v2',
          distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
        },
      }),
      applicant: json(200, {
        applicant: {
          id: 'a1',
          playtestId: 'pt-1',
          status: 'APPLICANT_STATUS_APPROVED',
          ndaVersionHash: 'sha-v1',
        },
      }),
    });
    render(Pending, { config, slug: 'demo' });
    await vi.waitFor(() => {
      expect(window.location.hash).toBe('#/playtest/demo/nda');
    });
  });
});
