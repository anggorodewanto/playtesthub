import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import Callback from '../src/routes/Callback.svelte';
import { getAccessToken, storePendingLogin } from '../src/lib/auth';
import { pendingPath, signupPath } from '../src/lib/router';
import type { Config } from '../src/lib/config';

const config: Config = {
  grpcGatewayUrl: 'https://api.example.com/playtesthub',
  iamBaseUrl: 'https://iam.example.com',
  discordClientId: 'discord-client-xyz',
};

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });

const exchangeOk = () =>
  json(200, { accessToken: 'player-tok', tokenType: 'Bearer', expiresIn: 3600 });
const applicantOk = (status: string) => json(200, { applicant: { id: 'a1', status } });

beforeEach(() => {
  sessionStorage.clear();
  window.location.hash = '';
});

afterEach(() => {
  sessionStorage.clear();
  vi.restoreAllMocks();
});

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
    storePendingLogin({ state: 'S', slug: 'demo' });
    vi.stubGlobal(
      'fetch',
      routedFetch({
        '/discord/exchange': exchangeOk,
        '/applicant': () => json(404, { message: 'not found' }),
      }),
    );

    render(Callback, { config, params: { code: 'discord-authcode', state: 'S' } });
    await vi.waitFor(() => {
      expect(getAccessToken()).toBe('player-tok');
    });
    await vi.waitFor(() => {
      expect(window.location.hash).toBe(`#${signupPath('demo')}`);
    });
  });

  it('returning user (PENDING applicant) → routes to pending, skipping signup', async () => {
    storePendingLogin({ state: 'S', slug: 'demo' });
    vi.stubGlobal(
      'fetch',
      routedFetch({
        '/discord/exchange': exchangeOk,
        '/applicant': () => applicantOk('APPLICANT_STATUS_PENDING'),
      }),
    );

    render(Callback, { config, params: { code: 'discord-authcode', state: 'S' } });
    await vi.waitFor(() => {
      expect(window.location.hash).toBe(`#${pendingPath('demo')}`);
    });
  });

  it('returning user (APPROVED applicant) → routes to pending', async () => {
    storePendingLogin({ state: 'S', slug: 'demo' });
    vi.stubGlobal(
      'fetch',
      routedFetch({
        '/discord/exchange': exchangeOk,
        '/applicant': () => applicantOk('APPLICANT_STATUS_APPROVED'),
      }),
    );

    render(Callback, { config, params: { code: 'discord-authcode', state: 'S' } });
    await vi.waitFor(() => {
      expect(window.location.hash).toBe(`#${pendingPath('demo')}`);
    });
  });

  it('status probe error → falls back to pending route (Pending surfaces the load error)', async () => {
    storePendingLogin({ state: 'S', slug: 'demo' });
    vi.stubGlobal(
      'fetch',
      routedFetch({
        '/discord/exchange': exchangeOk,
        '/applicant': () => new Response('boom', { status: 503 }),
      }),
    );

    render(Callback, { config, params: { code: 'discord-authcode', state: 'S' } });
    await vi.waitFor(() => {
      expect(window.location.hash).toBe(`#${pendingPath('demo')}`);
    });
  });

  it('rejects state mismatch', async () => {
    storePendingLogin({ state: 'S', slug: 'demo' });
    vi.stubGlobal('fetch', vi.fn());
    render(Callback, { config, params: { code: 'discord-authcode', state: 'OTHER' } });
    expect(await screen.findByTestId('callback-error')).toHaveTextContent(/login failed/i);
  });

  it('shows user-facing message on backend 5xx', async () => {
    storePendingLogin({ state: 'S', slug: 'demo' });
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
