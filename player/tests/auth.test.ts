import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Config } from '../src/lib/config';
import {
  buildDiscordAuthorizeUrl,
  clearPendingLogin,
  DISCORD_LOGIN_SCOPE,
  exchangeDiscordCode,
  GENERIC_LOGIN_FAILED_MESSAGE,
  IamError,
  getAccessToken,
  logout,
  storePendingLogin,
  TOKEN_STORAGE_KEY,
} from '../src/lib/auth';

const config: Config = {
  grpcGatewayUrl: 'https://api.example.com/playtesthub',
  iamBaseUrl: 'https://iam.example.com',
  discordClientId: 'discord-client-xyz',
};

beforeEach(() => {
  sessionStorage.clear();
  vi.restoreAllMocks();
});

afterEach(() => {
  sessionStorage.clear();
});

describe('buildDiscordAuthorizeUrl', () => {
  it('targets discord.com/oauth2/authorize with response_type=code', () => {
    const url = new URL(
      buildDiscordAuthorizeUrl({
        clientId: 'discord-client-xyz',
        redirectUri: 'https://player.example.com/callback',
        state: 'state-abc',
      }),
    );
    expect(url.host).toBe('discord.com');
    expect(url.pathname).toBe('/oauth2/authorize');
    expect(url.searchParams.get('response_type')).toBe('code');
    expect(url.searchParams.get('client_id')).toBe('discord-client-xyz');
    expect(url.searchParams.get('redirect_uri')).toBe(
      'https://player.example.com/callback',
    );
    expect(url.searchParams.get('state')).toBe('state-abc');
    expect(url.searchParams.get('scope')).toBe(DISCORD_LOGIN_SCOPE);
  });

  it('forwards a custom scope when provided', () => {
    const url = new URL(
      buildDiscordAuthorizeUrl({
        clientId: 'c',
        redirectUri: 'https://x/cb',
        state: 's',
        scope: 'identify',
      }),
    );
    expect(url.searchParams.get('scope')).toBe('identify');
  });
});

describe('exchangeDiscordCode', () => {
  it('POSTs JSON to grpcGatewayUrl + /v1/player/discord/exchange and stores access token', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          accessToken: 'ags-tok-1',
          refreshToken: 'ags-refresh-1',
          expiresIn: 3600,
          tokenType: 'Bearer',
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    );
    vi.stubGlobal('fetch', fetchMock);

    const result = await exchangeDiscordCode(config, {
      code: 'discord-code-xyz',
      redirectUri: 'https://player.example.com/callback',
    });
    expect(result.access_token).toBe('ags-tok-1');
    expect(result.refresh_token).toBe('ags-refresh-1');
    expect(result.expires_in).toBe(3600);
    expect(result.token_type).toBe('Bearer');
    expect(getAccessToken()).toBe('ags-tok-1');

    const [calledUrl, init] = fetchMock.mock.calls[0];
    expect(calledUrl).toBe('https://api.example.com/playtesthub/v1/player/discord/exchange');
    expect(init.method).toBe('POST');
    expect(init.headers['Content-Type']).toBe('application/json');
    const body = JSON.parse(init.body);
    expect(body.code).toBe('discord-code-xyz');
    expect(body.redirect_uri).toBe('https://player.example.com/callback');
  });

  it('handles a grpcGatewayUrl with a trailing slash', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ accessToken: 'x' }), { status: 200 }),
    );
    vi.stubGlobal('fetch', fetchMock);

    await exchangeDiscordCode(
      { ...config, grpcGatewayUrl: 'https://api.example.com/playtesthub/' },
      { code: 'c', redirectUri: 'r' },
    );
    const [calledUrl] = fetchMock.mock.calls[0];
    expect(calledUrl).toBe('https://api.example.com/playtesthub/v1/player/discord/exchange');
  });

  it('defaults token_type to Bearer when AGS omits it', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ accessToken: 'x', expiresIn: 60 }), { status: 200 }),
      ),
    );
    const result = await exchangeDiscordCode(config, { code: 'c', redirectUri: 'r' });
    expect(result.token_type).toBe('Bearer');
  });

  it('maps backend 5xx to IamError with generic user message', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response('boom', { status: 503, statusText: 'Service Unavailable' })),
    );
    await expect(
      exchangeDiscordCode(config, { code: 'c', redirectUri: 'r' }),
    ).rejects.toMatchObject({ name: 'IamError', userMessage: GENERIC_LOGIN_FAILED_MESSAGE });
  });

  it('maps a network error to IamError', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('network down')));
    await expect(
      exchangeDiscordCode(config, { code: 'c', redirectUri: 'r' }),
    ).rejects.toBeInstanceOf(IamError);
  });

  it('rejects when the response body is missing accessToken', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response(JSON.stringify({}), { status: 200 })),
    );
    await expect(
      exchangeDiscordCode(config, { code: 'c', redirectUri: 'r' }),
    ).rejects.toBeInstanceOf(IamError);
  });

  it('maps 4xx (invalid_grant) to generic login-failed', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response('{"error":"invalid_grant"}', { status: 400 })),
    );
    await expect(
      exchangeDiscordCode(config, { code: 'c', redirectUri: 'r' }),
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
    storePendingLogin({ state: 's', slug: 'foo', returnTo: '#/playtest/foo/signup' });
    clearPendingLogin();
    expect(sessionStorage.getItem('playtesthub.pendingLogin')).toBeNull();
  });
});
