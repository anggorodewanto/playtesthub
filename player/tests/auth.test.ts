import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Config } from '../src/lib/config';
import {
  buildDiscordLoginUrl,
  exchangeCodeForToken,
  getAccessToken,
  logout,
  clearPendingLogin,
  storePendingLogin,
  GENERIC_LOGIN_FAILED_MESSAGE,
  IamError,
  TOKEN_STORAGE_KEY,
} from '../src/lib/auth';

const config: Config = {
  grpcGatewayUrl: 'https://api.example.com/playtesthub',
  iamBaseUrl: 'https://iam.example.com',
  discordClientId: 'client-xyz',
};

beforeEach(() => {
  sessionStorage.clear();
  vi.restoreAllMocks();
});

afterEach(() => {
  sessionStorage.clear();
});

describe('buildDiscordLoginUrl', () => {
  it('targets iam authorize endpoint with discord idp_hint and PKCE', () => {
    const u = buildDiscordLoginUrl(config, {
      state: 'state-abc',
      codeChallenge: 'challenge-xyz',
      redirectUri: 'https://player.example.com/#/callback',
    });
    const parsed = new URL(u);
    expect(parsed.origin + parsed.pathname).toBe('https://iam.example.com/iam/v3/authorize');
    expect(parsed.searchParams.get('client_id')).toBe('client-xyz');
    expect(parsed.searchParams.get('response_type')).toBe('code');
    expect(parsed.searchParams.get('code_challenge')).toBe('challenge-xyz');
    expect(parsed.searchParams.get('code_challenge_method')).toBe('S256');
    expect(parsed.searchParams.get('state')).toBe('state-abc');
    expect(parsed.searchParams.get('redirect_uri')).toBe(
      'https://player.example.com/#/callback',
    );
    expect(parsed.searchParams.get('idp_hint')).toBe('discord');
    expect(parsed.searchParams.get('scope')).toBe('commerce account social publishing analytics');
  });
});

describe('exchangeCodeForToken', () => {
  it('POSTs form-encoded body to /iam/v3/oauth/token and stores access_token', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({ access_token: 'tok-1', token_type: 'Bearer', expires_in: 3600 }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    );
    vi.stubGlobal('fetch', fetchMock);

    const result = await exchangeCodeForToken(config, {
      code: 'authcode',
      codeVerifier: 'verifier',
      redirectUri: 'https://player.example.com/#/callback',
    });
    expect(result.access_token).toBe('tok-1');

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://iam.example.com/iam/v3/oauth/token');
    expect(init.method).toBe('POST');
    expect(init.headers['Content-Type']).toBe('application/x-www-form-urlencoded');
    const body = new URLSearchParams(init.body);
    expect(body.get('grant_type')).toBe('authorization_code');
    expect(body.get('code')).toBe('authcode');
    expect(body.get('code_verifier')).toBe('verifier');
    expect(body.get('client_id')).toBe('client-xyz');
    expect(body.get('redirect_uri')).toBe('https://player.example.com/#/callback');

    expect(getAccessToken()).toBe('tok-1');
  });

  it('maps IAM 5xx to generic login-failed message (PRD §5.2)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response('boom', { status: 503, statusText: 'Service Unavailable' })),
    );
    await expect(
      exchangeCodeForToken(config, {
        code: 'c',
        codeVerifier: 'v',
        redirectUri: 'r',
      }),
    ).rejects.toMatchObject({
      name: 'IamError',
      userMessage: GENERIC_LOGIN_FAILED_MESSAGE,
    });
  });

  it('maps network error to generic login-failed message', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockRejectedValue(new TypeError('network down')),
    );
    await expect(
      exchangeCodeForToken(config, { code: 'c', codeVerifier: 'v', redirectUri: 'r' }),
    ).rejects.toBeInstanceOf(IamError);
  });

  it('maps 4xx (bad authorization_code) to generic login-failed', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response('{"error":"invalid_grant"}', { status: 400 })),
    );
    await expect(
      exchangeCodeForToken(config, { code: 'c', codeVerifier: 'v', redirectUri: 'r' }),
    ).rejects.toMatchObject({ userMessage: GENERIC_LOGIN_FAILED_MESSAGE });
  });
});

describe('token storage', () => {
  it('logout clears the access token', async () => {
    sessionStorage.setItem(TOKEN_STORAGE_KEY, 'abc');
    logout();
    expect(getAccessToken()).toBeNull();
  });
});

describe('pending login', () => {
  it('round-trips through sessionStorage and clears', () => {
    storePendingLogin({ state: 's', codeVerifier: 'v', returnTo: '#/playtest/foo/signup' });
    clearPendingLogin();
    expect(sessionStorage.getItem('playtesthub.pendingLogin')).toBeNull();
  });
});
