import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Config } from '../src/lib/config';
import { ApiError, fetchApplicantStatus, fetchPublicPlaytest, submitSignup } from '../src/lib/api';
import { setAccessToken, TOKEN_STORAGE_KEY } from '../src/lib/auth';

const config: Config = {
  grpcGatewayUrl: 'https://api.example.com/playtesthub',
  iamBaseUrl: 'https://iam.example.com',
  discordClientId: 'client-xyz',
};

const respond = (status: number, body: unknown) =>
  new Response(typeof body === 'string' ? body : JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });

beforeEach(() => {
  sessionStorage.clear();
});

afterEach(() => {
  sessionStorage.clear();
  vi.restoreAllMocks();
});

describe('fetchPublicPlaytest', () => {
  it('GETs /v1/public/playtests/:slug without auth header', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      respond(200, {
        playtest: {
          slug: 'demo',
          title: 'Demo',
          description: 'hello',
          bannerImageUrl: '',
          platforms: ['PLATFORM_STEAM'],
          startsAt: '2026-05-01T00:00:00Z',
          endsAt: '2026-05-08T00:00:00Z',
        },
      }),
    );
    vi.stubGlobal('fetch', fetchMock);

    const playtest = await fetchPublicPlaytest(config, 'demo');

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.example.com/playtesthub/v1/public/playtests/demo');
    expect(init.method).toBe('GET');
    expect((init.headers ?? {})['Authorization']).toBeUndefined();
    expect(playtest.slug).toBe('demo');
    expect(playtest.platforms).toEqual(['PLATFORM_STEAM']);
  });

  it('URL-encodes slugs', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(respond(200, { playtest: { slug: 'a/b', title: 't' } }));
    vi.stubGlobal('fetch', fetchMock);
    await fetchPublicPlaytest(config, 'a/b');
    expect(fetchMock.mock.calls[0][0]).toBe(
      'https://api.example.com/playtesthub/v1/public/playtests/a%2Fb',
    );
  });

  it('maps 404 to ApiError with NOT_FOUND code', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(respond(404, '{"message":"not found"}')));
    await expect(fetchPublicPlaytest(config, 'missing')).rejects.toMatchObject({
      name: 'ApiError',
      status: 404,
    });
  });
});

describe('submitSignup', () => {
  it('POSTs with bearer token and platform array', async () => {
    setAccessToken('tok-player');
    const fetchMock = vi.fn().mockResolvedValue(
      respond(200, {
        applicant: { id: 'a1', status: 'APPLICANT_STATUS_PENDING' },
      }),
    );
    vi.stubGlobal('fetch', fetchMock);

    const result = await submitSignup(config, 'demo', {
      platforms: ['PLATFORM_STEAM', 'PLATFORM_XBOX'],
    });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.example.com/playtesthub/v1/player/playtests/demo/signup');
    expect(init.method).toBe('POST');
    expect(init.headers['Authorization']).toBe('Bearer tok-player');
    expect(init.headers['Content-Type']).toBe('application/json');
    const body = JSON.parse(init.body);
    expect(body.platforms).toEqual(['PLATFORM_STEAM', 'PLATFORM_XBOX']);
    expect(result.status).toBe('APPLICANT_STATUS_PENDING');
  });

  it('throws ApiError with status=401 when unauthenticated', async () => {
    vi.stubGlobal('fetch', vi.fn());
    await expect(submitSignup(config, 'demo', { platforms: [] })).rejects.toMatchObject({
      name: 'ApiError',
      status: 401,
    });
  });

  it('bubbles 401 from gateway as ApiError', async () => {
    setAccessToken('stale');
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(respond(401, '{"message":"bad token"}')));
    await expect(submitSignup(config, 'demo', { platforms: [] })).rejects.toMatchObject({
      status: 401,
    });
  });
});

describe('fetchApplicantStatus', () => {
  it('GETs /v1/player/playtests/:slug/applicant with bearer', async () => {
    sessionStorage.setItem(TOKEN_STORAGE_KEY, 'tok');
    const fetchMock = vi.fn().mockResolvedValue(
      respond(200, {
        applicant: { id: 'a1', status: 'APPLICANT_STATUS_PENDING' },
      }),
    );
    vi.stubGlobal('fetch', fetchMock);
    const applicant = await fetchApplicantStatus(config, 'demo');
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.example.com/playtesthub/v1/player/playtests/demo/applicant');
    expect(init.headers['Authorization']).toBe('Bearer tok');
    expect(applicant.status).toBe('APPLICANT_STATUS_PENDING');
  });
});

describe('ApiError', () => {
  it('exposes code/message for user-facing rendering', () => {
    const e = new ApiError(500, 'boom');
    expect(e.status).toBe(500);
    expect(e.message).toContain('boom');
  });
});
