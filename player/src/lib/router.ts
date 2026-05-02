import { readable, type Readable } from 'svelte/store';

export type Route =
  | { name: 'landing'; slug: string }
  | { name: 'signup'; slug: string }
  | { name: 'nda'; slug: string }
  | { name: 'pending'; slug: string }
  | { name: 'callback'; params: Record<string, string> }
  | { name: 'not-found' };

export function parseRoute(hash: string): Route {
  if (!hash || hash === '#' || hash === '#/') return { name: 'not-found' };

  const withoutHash = hash.startsWith('#') ? hash.slice(1) : hash;
  const [pathPart, queryPart = ''] = withoutHash.split('?', 2);
  const parts = pathPart.split('/').filter((p) => p.length > 0);

  if (parts[0] === 'callback') {
    const params: Record<string, string> = {};
    for (const [k, v] of new URLSearchParams(queryPart).entries()) params[k] = v;
    return { name: 'callback', params };
  }

  if (parts[0] === 'playtest' && parts[1]) {
    const slug = safeDecode(parts[1]);
    if (!slug) return { name: 'not-found' };
    const sub = parts[2];
    if (!sub) return { name: 'landing', slug };
    if (sub === 'signup') return { name: 'signup', slug };
    if (sub === 'nda') return { name: 'nda', slug };
    if (sub === 'pending') return { name: 'pending', slug };
    return { name: 'not-found' };
  }

  return { name: 'not-found' };
}

function safeDecode(v: string): string | null {
  try {
    return decodeURIComponent(v);
  } catch {
    return null;
  }
}

export function navigate(hash: string): void {
  if (typeof window === 'undefined') return;
  window.location.hash = hash.startsWith('#') ? hash : `#${hash}`;
}

export const playtestPath = (slug: string): string => `/playtest/${slug}`;
export const signupPath = (slug: string): string => `/playtest/${slug}/signup`;
export const ndaPath = (slug: string): string => `/playtest/${slug}/nda`;
export const pendingPath = (slug: string): string => `/playtest/${slug}/pending`;

export const route: Readable<Route> = readable<Route>(
  typeof window === 'undefined' ? { name: 'not-found' } : parseRoute(window.location.hash),
  (set) => {
    if (typeof window === 'undefined') return () => undefined;
    const update = () => set(parseRoute(window.location.hash));
    update();
    window.addEventListener('hashchange', update);
    return () => window.removeEventListener('hashchange', update);
  },
);
