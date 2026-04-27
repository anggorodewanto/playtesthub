import { describe, expect, it, vi } from 'vitest';
import { bridgePathCallback } from '../src/lib/bootstrap';

function mockLoc(pathname: string, search: string): Location {
  return { pathname, search } as Location;
}

describe('bridgePathCallback', () => {
  it('rewrites /callback?code=...&state=... to /#/callback?code=...&state=...', () => {
    const replaceState = vi.fn();
    const hist = { replaceState } as unknown as History;
    const loc = mockLoc('/callback', '?code=XYZ&state=ABC');
    bridgePathCallback(loc, hist);
    expect(replaceState).toHaveBeenCalledTimes(1);
    expect(replaceState).toHaveBeenCalledWith(null, '', '/#/callback?code=XYZ&state=ABC');
  });

  it('no-ops when pathname is not /callback', () => {
    const replaceState = vi.fn();
    const hist = { replaceState } as unknown as History;
    bridgePathCallback(mockLoc('/', ''), hist);
    bridgePathCallback(mockLoc('/playtest/foo', ''), hist);
    bridgePathCallback(mockLoc('/something/callback', '?code=X'), hist);
    expect(replaceState).not.toHaveBeenCalled();
  });

  it('preserves empty search string', () => {
    const replaceState = vi.fn();
    const hist = { replaceState } as unknown as History;
    bridgePathCallback(mockLoc('/callback', ''), hist);
    expect(replaceState).toHaveBeenCalledWith(null, '', '/#/callback');
  });
});
