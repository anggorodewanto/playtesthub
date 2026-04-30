import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import Signup from '../src/routes/Signup.svelte';
import type { Config } from '../src/lib/config';
import { setAccessToken } from '../src/lib/auth';
import { pendingPath, playtestPath } from '../src/lib/router';

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
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    render(Signup, { config, slug: 'demo' });
    await userEvent.click(screen.getByRole('button', { name: /submit application/i }));

    expect(screen.getByTestId('signup-error')).toHaveTextContent(/at least one platform/i);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('POSTs selected platforms and navigates to pending', async () => {
    setAccessToken('tok');
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({ applicant: { id: 'a1', status: 'APPLICANT_STATUS_PENDING' } }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    );
    vi.stubGlobal('fetch', fetchMock);

    render(Signup, { config, slug: 'demo' });
    await userEvent.click(screen.getByLabelText('Steam'));
    await userEvent.click(screen.getByLabelText('Xbox'));
    await userEvent.click(screen.getByRole('button', { name: /submit application/i }));

    await vi.waitFor(() => {
      expect(fetchMock).toHaveBeenCalledOnce();
    });
    const body = JSON.parse(fetchMock.mock.calls[0][1].body);
    expect(body.platforms).toEqual(['PLATFORM_STEAM', 'PLATFORM_XBOX']);

    await vi.waitFor(() => {
      expect(window.location.hash).toBe(`#${pendingPath('demo')}`);
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
    await userEvent.click(screen.getByRole('button', { name: /submit application/i }));

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
    await userEvent.click(screen.getByRole('button', { name: /submit application/i }));

    await vi.waitFor(() => {
      expect(window.location.hash).toBe(`#${pendingPath('demo')}`);
    });
  });
});
