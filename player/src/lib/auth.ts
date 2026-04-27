import type { Config } from './config';

export const TOKEN_STORAGE_KEY = 'playtesthub.accessToken';
const PENDING_LOGIN_KEY = 'playtesthub.pendingLogin';

export const GENERIC_LOGIN_FAILED_MESSAGE = 'Login failed — please try again later';

export class IamError extends Error {
  userMessage: string;

  constructor(message: string, userMessage: string = GENERIC_LOGIN_FAILED_MESSAGE) {
    super(message);
    this.name = 'IamError';
    this.userMessage = userMessage;
  }
}

export type PendingLogin = {
  state: string;
  codeVerifier: string;
  returnTo: string;
};

export type TokenResponse = {
  access_token: string;
  token_type: string;
  expires_in: number;
  refresh_token?: string;
};

export function storePendingLogin(p: PendingLogin): void {
  sessionStorage.setItem(PENDING_LOGIN_KEY, JSON.stringify(p));
}

export function readPendingLogin(): PendingLogin | null {
  const raw = sessionStorage.getItem(PENDING_LOGIN_KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as PendingLogin;
  } catch {
    return null;
  }
}

export function clearPendingLogin(): void {
  sessionStorage.removeItem(PENDING_LOGIN_KEY);
}

export function getAccessToken(): string | null {
  return sessionStorage.getItem(TOKEN_STORAGE_KEY);
}

export function setAccessToken(token: string): void {
  sessionStorage.setItem(TOKEN_STORAGE_KEY, token);
}

export function logout(): void {
  sessionStorage.removeItem(TOKEN_STORAGE_KEY);
  clearPendingLogin();
}

export type FetchDiscordLoginUrlOpts = {
  state: string;
  codeChallenge: string;
  redirectUri: string;
  scope?: string;
};

export const DEFAULT_DISCORD_LOGIN_SCOPE = 'commerce account social publishing analytics';

// fetchDiscordLoginUrl asks the backend to build the AGS IAM Discord
// login URL on the player's behalf. The RPC performs a server-side
// /iam/v3/oauth/authorize hop and returns the second-hop URL the
// player should navigate to (/iam/v3/oauth/platforms/discord/authorize).
// See STATUS.md M1 phase 9.2 — AGS IAM's hosted /auth/ SPA does not
// render the Discord button on shared cloud, so the player cannot
// drive /oauth/authorize directly.
export async function fetchDiscordLoginUrl(
  config: Config,
  opts: FetchDiscordLoginUrlOpts,
): Promise<string> {
  const url = joinGatewayPath(config.grpcGatewayUrl, '/v1/player/discord/login-url');
  const body = {
    redirect_uri: opts.redirectUri,
    state: opts.state,
    code_challenge: opts.codeChallenge,
    code_challenge_method: 'S256',
    scope: opts.scope ?? DEFAULT_DISCORD_LOGIN_SCOPE,
  };

  let res: Response;
  try {
    res = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
  } catch (err) {
    throw new IamError(`Discord login URL fetch network error: ${(err as Error).message}`);
  }

  if (!res.ok) {
    throw new IamError(`Discord login URL fetch failed: ${res.status} ${res.statusText}`);
  }

  // grpc-gateway emits proto fields as camelCase — `login_url` on the
  // wire becomes `loginUrl` in JSON. Reading snake_case here would
  // silently miss the field on every call.
  const parsed = (await res.json()) as { loginUrl?: string };
  if (!parsed.loginUrl) {
    throw new IamError('Discord login URL response missing loginUrl');
  }
  return parsed.loginUrl;
}

function joinGatewayPath(base: string, path: string): string {
  // Trim a single trailing slash from base and a single leading slash
  // from path; concatenating cleanly handles both `https://x/playtesthub`
  // and `https://x/playtesthub/` config shapes.
  const trimmedBase = base.endsWith('/') ? base.slice(0, -1) : base;
  const trimmedPath = path.startsWith('/') ? path : `/${path}`;
  return trimmedBase + trimmedPath;
}

export type ExchangeOpts = {
  code: string;
  codeVerifier: string;
  redirectUri: string;
};

export async function exchangeCodeForToken(
  config: Config,
  opts: ExchangeOpts,
): Promise<TokenResponse> {
  const body = new URLSearchParams({
    grant_type: 'authorization_code',
    code: opts.code,
    code_verifier: opts.codeVerifier,
    client_id: config.discordClientId,
    redirect_uri: opts.redirectUri,
  });

  let res: Response;
  try {
    res = await fetch(new URL('/iam/v3/oauth/token', config.iamBaseUrl).toString(), {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body.toString(),
    });
  } catch (err) {
    throw new IamError(`IAM token exchange network error: ${(err as Error).message}`);
  }

  if (!res.ok) {
    throw new IamError(`IAM token exchange failed: ${res.status} ${res.statusText}`);
  }

  const parsed = (await res.json()) as TokenResponse;
  if (!parsed.access_token) {
    throw new IamError('IAM token exchange response missing access_token');
  }
  setAccessToken(parsed.access_token);
  return parsed;
}
