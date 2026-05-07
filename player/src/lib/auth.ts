import type { Config } from './config';
import { joinPath } from './api';

export const TOKEN_STORAGE_KEY = 'playtesthub.accessToken';
const PENDING_LOGIN_KEY = 'playtesthub.pendingLogin';

export const GENERIC_LOGIN_FAILED_MESSAGE = 'Login failed — please try again later';

// DISCORD_LOGIN_SCOPE is what the player asks Discord for. AGS IAM uses
// the linked Discord account's identity + email to create / look up the
// federated user; broader Discord scopes aren't needed.
export const DISCORD_LOGIN_SCOPE = 'identify email';

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
  slug: string;
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

// discordRedirectUri returns the byte-exact URL Discord must allowlist
// AND the player must send to /oauth2/authorize AND AGS Admin Portal's
// Discord platform RedirectUri must equal — see runbooks/setup-ags-discord.md
// § Three URLs that must agree byte-for-byte. Built from `loc.origin`
// (scheme + host) plus Vite's compile-time BASE_URL (with trailing
// slash — `/` for a root deploy, `/<repo>/` for GitHub Pages project
// sites). Fed by the same source through the round-trip — Landing
// uses it to build the authorize URL, Callback uses it to populate
// the `redirect_uri` form-body to the exchange RPC — so the two
// sides cannot drift.
export function discordRedirectUri(
  loc: { origin: string } = window.location,
  basePath: string = import.meta.env.BASE_URL,
): string {
  return `${loc.origin}${basePath}callback`;
}

// buildDiscordAuthorizeUrl composes the URL the player navigates to to
// start Discord OAuth. The Discord developer portal owns the redirect
// URI allowlist — AGS IAM is not involved until ExchangeDiscordCode.
export type BuildDiscordAuthorizeUrlOpts = {
  clientId: string;
  redirectUri: string;
  state: string;
  scope?: string;
};

export function buildDiscordAuthorizeUrl(opts: BuildDiscordAuthorizeUrlOpts): string {
  const params = new URLSearchParams({
    response_type: 'code',
    client_id: opts.clientId,
    redirect_uri: opts.redirectUri,
    state: opts.state,
    scope: opts.scope ?? DISCORD_LOGIN_SCOPE,
  });
  return `https://discord.com/oauth2/authorize?${params.toString()}`;
}

export type ExchangeDiscordCodeOpts = {
  code: string;
  redirectUri: string;
};

// exchangeDiscordCode forwards the Discord OAuth code to the backend,
// which calls AGS IAM's platform-token grant with confidential client
// credentials. AGS auto-creates the Justice platform account on first
// call. See STATUS.md M1 phase 9.3.
export async function exchangeDiscordCode(
  config: Config,
  opts: ExchangeDiscordCodeOpts,
): Promise<TokenResponse> {
  const url = joinPath(config.grpcGatewayUrl, '/v1/player/discord/exchange');
  const body = { code: opts.code, redirect_uri: opts.redirectUri };

  let res: Response;
  try {
    res = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
  } catch (err) {
    throw new IamError(`Discord exchange network error: ${(err as Error).message}`);
  }

  if (!res.ok) {
    throw new IamError(`Discord exchange failed: ${res.status} ${res.statusText}`);
  }

  // grpc-gateway emits proto fields as camelCase. The wire field is
  // accessToken, but downstream consumers use the snake_case form
  // documented in TokenResponse, so we normalise here.
  const parsed = (await res.json()) as {
    accessToken?: string;
    refreshToken?: string;
    expiresIn?: number;
    tokenType?: string;
  };
  if (!parsed.accessToken) {
    throw new IamError('Discord exchange response missing accessToken');
  }
  setAccessToken(parsed.accessToken);
  return {
    access_token: parsed.accessToken,
    refresh_token: parsed.refreshToken,
    expires_in: parsed.expiresIn ?? 0,
    token_type: parsed.tokenType ?? 'Bearer',
  };
}

