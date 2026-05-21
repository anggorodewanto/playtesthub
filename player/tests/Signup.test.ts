import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import Signup from '../src/routes/Signup.svelte';
import type { Config } from '../src/lib/config';
import { setAccessToken } from '../src/lib/auth';
import { ndaPath, playtestPath } from '../src/lib/router';

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

describe('Signup', () => {
  it('requires at least one platform before submit', async () => {
    setAccessToken('tok');
    // The crumb-header preload fetches PublicPlaytest; the test only cares
    // that the SignupApplicant POST does not fire when validation fails.
    const fetchMock = vi.fn().mockImplementation(() =>
      Promise.resolve(
        new Response(JSON.stringify({ playtest: { slug: 'demo', title: 'Demo' } }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);

    render(Signup, { config, slug: 'demo' });
    await userEvent.click(screen.getByRole('button', { name: /continue/i }));

    expect(screen.getByTestId('signup-error')).toHaveTextContent(/at least one platform/i);
    const signupCalls = fetchMock.mock.calls.filter(([url]) =>
      String(url).includes('/signup'),
    );
    expect(signupCalls).toHaveLength(0);
  });

  it('POSTs selected platforms and navigates to pending', async () => {
    setAccessToken('tok');
    // Signup performs a side-channel GetPublicPlaytest to render the crumb
    // title, then a separate :signup POST — each call needs a fresh Response
    // because Response bodies are consumed on read.
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes('/signup')) {
        return Promise.resolve(
          new Response(
            JSON.stringify({ applicant: { id: 'a1', status: 'APPLICANT_STATUS_PENDING' } }),
            { status: 200, headers: { 'Content-Type': 'application/json' } },
          ),
        );
      }
      return Promise.resolve(
        new Response(JSON.stringify({ playtest: { slug: 'demo', title: 'Demo' } }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
    });
    vi.stubGlobal('fetch', fetchMock);

    render(Signup, { config, slug: 'demo' });
    await userEvent.click(screen.getByLabelText('Steam'));
    await userEvent.click(screen.getByLabelText('Xbox'));
    await userEvent.click(screen.getByRole('button', { name: /continue/i }));

    await vi.waitFor(() => {
      const signupCall = fetchMock.mock.calls.find(([url]) => String(url).includes('/signup'));
      expect(signupCall).toBeDefined();
    });
    const signupCall = fetchMock.mock.calls.find(([url]) => String(url).includes('/signup'))!;
    const body = JSON.parse(signupCall[1].body);
    expect(body.platforms).toEqual(['PLATFORM_STEAM', 'PLATFORM_XBOX']);

    await vi.waitFor(() => {
      expect(window.location.hash).toBe(`#${ndaPath('demo')}`);
    });
  });

  it('surfaces 401 as expired-session message', async () => {
    setAccessToken('stale');
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response('{"message":"bad token"}', {
          status: 401,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );

    render(Signup, { config, slug: 'demo' });
    await userEvent.click(screen.getByLabelText('Steam'));
    await userEvent.click(screen.getByRole('button', { name: /continue/i }));

    expect(await screen.findByTestId('signup-error')).toHaveTextContent(/session has expired/i);
  });

  it('bounces to landing when no token is present', () => {
    render(Signup, { config, slug: 'demo' });
    expect(window.location.hash).toBe(`#${playtestPath('demo')}`);
  });

  it('routes to pending on 409 (already an applicant)', async () => {
    setAccessToken('tok');
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response('{"message":"applicant already exists"}', {
          status: 409,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );

    render(Signup, { config, slug: 'demo' });
    await userEvent.click(screen.getByLabelText('Steam'));
    await userEvent.click(screen.getByRole('button', { name: /continue/i }));

    await vi.waitFor(() => {
      expect(window.location.hash).toBe(`#${ndaPath('demo')}`);
    });
  });
});
