import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import Landing from '../src/routes/Landing.svelte';
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

beforeEach(() => {
  sessionStorage.clear();
});

afterEach(() => {
  sessionStorage.clear();
  vi.restoreAllMocks();
});

describe('Landing', () => {
  it('renders unauth fields only and never leaks ndaText', async () => {
    const ndaMarker = 'SECRET_NDA_SHOULD_NEVER_APPEAR';
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        json(200, {
          playtest: {
            slug: 'demo',
            title: 'Demo Game',
            description: 'public description',
            bannerImageUrl: '',
            platforms: ['PLATFORM_STEAM'],
            startsAt: '2026-05-01T00:00:00Z',
            endsAt: '2026-05-08T00:00:00Z',
            // Not part of PublicPlaytest per proto — defensive paranoia:
            // if the backend ever regresses and ships ndaText here,
            // Landing must still not render it.
            ndaText: ndaMarker,
          },
        }),
      ),
    );

    render(Landing, { config, slug: 'demo' });
    expect(await screen.findByText('Demo Game')).toBeInTheDocument();
    expect(screen.getByTestId('playtest-description')).toHaveTextContent('public description');
    expect(document.body.textContent ?? '').not.toContain(ndaMarker);
    expect(screen.getByRole('button', { name: /sign up/i })).toBeInTheDocument();
  });

  it('shows a friendly message on 404', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response('{"message":"not found"}', {
          status: 404,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );
    render(Landing, { config, slug: 'missing' });
    expect(await screen.findByText(/not available/i)).toBeInTheDocument();
  });
});
