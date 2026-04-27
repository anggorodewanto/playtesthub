// AGS IAM forbids fragments in registered redirect URIs (RFC 6749 §3.1.2),
// so the player registers a path-based callback (`<origin>/callback`). The
// router is hash-based, so on return from IAM we rewrite `/callback?...` to
// `/#/callback?...` before the app mounts — the router then handles it as
// the existing `callback` route without any other changes.
export function bridgePathCallback(loc: Location = window.location, hist: History = window.history): void {
  if (loc.pathname !== '/callback') return;
  const search = loc.search;
  hist.replaceState(null, '', `/#/callback${search}`);
}
