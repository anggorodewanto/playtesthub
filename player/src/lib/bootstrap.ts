// Discord OAuth's allowlisted redirect URIs are byte-exact strings
// without fragments, so the player registers a path-based callback
// (`<origin><base>callback`). The router is hash-based, so on return
// from Discord we rewrite `<base>callback?code=…` to
// `<base>#/callback?code=…` before the app mounts — the router then
// handles it as the existing `callback` route without any other changes.
//
// `basePath` defaults to Vite's compile-time `import.meta.env.BASE_URL`
// (e.g. `/playtesthub/` on GitHub Pages, `/` on a root-served deploy)
// so the same source works under any subpath without a runtime config
// fetch on the redirect-bridge path.
export function bridgePathCallback(
  loc: Location = window.location,
  hist: History = window.history,
  basePath: string = import.meta.env.BASE_URL,
): void {
  const expected = `${basePath}callback`;
  if (loc.pathname !== expected) return;
  const search = loc.search;
  hist.replaceState(null, '', `${basePath}#/callback${search}`);
}
