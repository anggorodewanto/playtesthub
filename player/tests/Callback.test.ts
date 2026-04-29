import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import Callback from '../src/routes/Callback.svelte';
import { getAccessToken, storePendingLogin } from '../src/lib/auth';
import type { Config } from '../src/lib/config';

const config: Config = {
  grpcGatewayUrl: 'https://api.example.com/playtesthub',
  iamBaseUrl: 'https://iam.example.com',
  discordClientId: 'discord-client-xyz',
};

beforeEach(() => {
  sessionStorage.clear();
  window.location.hash = '';
});

afterEach(() => {
  sessionStorage.clear();
  vi.restoreAllMocks();
});

function exchangeResponse() {
  return new Response(
    JSON.stringify({ accessToken: 'player-tok', tokenType: 'Bearer', expiresIn: 3600 }),
    { status: 200, headers: { 'Content-Type': 'application/json' } },
  );
}

function applicantNotFoundResponse() {
  return new Response(JSON.stringify({ message: 'not found' }), {
    status: 404,
    headers: { 'Content-Type': 'application/json' },
  });
}

function applicantResponse(status: string) {
  return new Response(
    JSON.stringify({ applicant: { id: 'a1', status } }),
    { status: 200, headers: { 'Content-Type': 'application/json' } },
  );
}

function routedFetch(handlers: Record<string, () => Response>) {
  return vi.fn(async (url: string) => {
    for (const [marker, fn] of Object.entries(handlers)) {
      if (url.includes(marker)) return fn();
    }
    throw new Error(`unexpected fetch: ${url}`);
  });
}

describe('Callback', () => {
  it('first-time user (no applicant) → routes to signup after exchange', async () => {
    storePendingLogin({ state: 'S', slug: 'demo', returnTo: '#/playtest/demo/signup' });
    vi.stubGlobal(
      'fetch',
      routedFetch({
        '/discord/exchange': exchangeResponse,
        '/applicant': applicantNotFoundResponse,
      }),
    );

    render(Callback, { config, params: { code: 'discord-authcode', state: 'S' } });
    await vi.waitFor(() => {
      expect(getAccessToken()).toBe('player-tok');
    });
    await vi.waitFor(() => {
      expect(window.location.hash).toBe('#/playtest/demo/signup');
    });
  });

  it('returning user (PENDING applicant) → routes to pending, skipping signup', async () => {
    storePendingLogin({ state: 'S', slug: 'demo', returnTo: '#/playtest/demo/signup' });
    vi.stubGlobal(
      'fetch',
      routedFetch({
        '/discord/exchange': exchangeResponse,
        '/applicant': () => applicantResponse('APPLICANT_STATUS_PENDING'),
      }),
    );

    render(Callback, { config, params: { code: 'discord-authcode', state: 'S' } });
    await vi.waitFor(() => {
      expect(window.location.hash).toBe('#/playtest/demo/pending');
    });
  });

  it('returning user (APPROVED applicant) → routes to pending', async () => {
    storePendingLogin({ state: 'S', slug: 'demo', returnTo: '#/playtest/demo/signup' });
    vi.stubGlobal(
      'fetch',
      routedFetch({
        '/discord/exchange': exchangeResponse,
        '/applicant': () => applicantResponse('APPLICANT_STATUS_APPROVED'),
      }),
    );

    render(Callback, { config, params: { code: 'discord-authcode', state: 'S' } });
    await vi.waitFor(() => {
      expect(window.location.hash).toBe('#/playtest/demo/pending');
    });
  });

  it('status probe error → falls back to pending route (Pending surfaces the load error)', async () => {
    storePendingLogin({ state: 'S', slug: 'demo', returnTo: '#/playtest/demo/signup' });
    vi.stubGlobal(
      'fetch',
      routedFetch({
        '/discord/exchange': exchangeResponse,
        '/applicant': () => new Response('boom', { status: 503 }),
      }),
    );

    render(Callback, { config, params: { code: 'discord-authcode', state: 'S' } });
    await vi.waitFor(() => {
      expect(window.location.hash).toBe('#/playtest/demo/pending');
    });
  });

  it('rejects state mismatch', async () => {
    storePendingLogin({ state: 'S', slug: 'demo', returnTo: '#/playtest/demo/signup' });
    vi.stubGlobal('fetch', vi.fn());
    render(Callback, { config, params: { code: 'discord-authcode', state: 'OTHER' } });
    expect(await screen.findByTestId('callback-error')).toHaveTextContent(/login failed/i);
  });

  it('shows user-facing message on backend 5xx', async () => {
    storePendingLogin({ state: 'S', slug: 'demo', returnTo: '#/playtest/demo/signup' });
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response('boom', { status: 503 })),
    );
    render(Callback, { config, params: { code: 'discord-authcode', state: 'S' } });
    expect(await screen.findByTestId('callback-error')).toHaveTextContent(
      /try again later/i,
    );
  });

  it('no pending login → error', async () => {
    render(Callback, { config, params: { code: 'discord-authcode', state: 'S' } });
    expect(await screen.findByTestId('callback-error')).toHaveTextContent(/expired/i);
  });
});
