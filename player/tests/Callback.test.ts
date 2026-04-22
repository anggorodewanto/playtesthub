import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import Callback from '../src/routes/Callback.svelte';
import { getAccessToken, storePendingLogin } from '../src/lib/auth';
import type { Config } from '../src/lib/config';

const config: Config = {
  grpcGatewayUrl: 'https://api.example.com/playtesthub',
  iamBaseUrl: 'https://iam.example.com',
  discordClientId: 'client-xyz',
};

beforeEach(() => {
  sessionStorage.clear();
  window.location.hash = '';
});

afterEach(() => {
  sessionStorage.clear();
  vi.restoreAllMocks();
});

describe('Callback', () => {
  it('exchanges code → token, clears pending, and navigates back to signup', async () => {
    storePendingLogin({ state: 'S', codeVerifier: 'V', returnTo: '#/playtest/demo/signup' });
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({ access_token: 'player-tok', token_type: 'Bearer', expires_in: 3600 }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    );

    render(Callback, { config, params: { code: 'authcode', state: 'S' } });
    await vi.waitFor(() => {
      expect(getAccessToken()).toBe('player-tok');
    });
    expect(window.location.hash).toBe('#/playtest/demo/signup');
  });

  it('rejects state mismatch', async () => {
    storePendingLogin({ state: 'S', codeVerifier: 'V', returnTo: '#/playtest/demo/signup' });
    vi.stubGlobal('fetch', vi.fn());
    render(Callback, { config, params: { code: 'authcode', state: 'OTHER' } });
    expect(await screen.findByTestId('callback-error')).toHaveTextContent(/login failed/i);
  });

  it('shows user-facing message on IAM 5xx', async () => {
    storePendingLogin({ state: 'S', codeVerifier: 'V', returnTo: '#/playtest/demo/signup' });
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response('boom', { status: 503 })),
    );
    render(Callback, { config, params: { code: 'authcode', state: 'S' } });
    expect(await screen.findByTestId('callback-error')).toHaveTextContent(
      /try again later/i,
    );
  });

  it('no pending login → error', async () => {
    render(Callback, { config, params: { code: 'authcode', state: 'S' } });
    expect(await screen.findByTestId('callback-error')).toHaveTextContent(/expired/i);
  });
});
