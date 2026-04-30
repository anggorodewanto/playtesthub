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
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        json(200, { applicant: { id: 'a1', status: 'APPLICANT_STATUS_PENDING' } }),
      ),
    );
    render(Pending, { config, slug: 'demo' });
    expect(await screen.findByText(/under review/i)).toBeInTheDocument();
  });

  it('shows generic not-selected copy on REJECTED (no rejection reason shown)', async () => {
    setAccessToken('tok');
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        json(200, {
          applicant: {
            id: 'a1',
            status: 'APPLICANT_STATUS_REJECTED',
            // rejectionReason is admin-only per PRD §5.4 — must never render.
          },
        }),
      ),
    );
    render(Pending, { config, slug: 'demo' });
    expect(await screen.findByText(/not selected/i)).toBeInTheDocument();
  });

  it('redirects to landing on 401 (session expired)', async () => {
    setAccessToken('stale');
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(json(401, { message: 'bad token' })),
    );
    render(Pending, { config, slug: 'demo' });
    await vi.waitFor(() => {
      expect(window.location.hash).toBe(`#${playtestPath('demo')}`);
    });
  });
});
