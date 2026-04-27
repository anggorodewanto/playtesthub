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

export type BuildLoginUrlOpts = {
  state: string;
  codeChallenge: string;
  redirectUri: string;
};

export function buildDiscordLoginUrl(config: Config, opts: BuildLoginUrlOpts): string {
  const url = new URL('/iam/v3/oauth/authorize', config.iamBaseUrl);
  url.searchParams.set('client_id', config.discordClientId);
  url.searchParams.set('response_type', 'code');
  url.searchParams.set('redirect_uri', opts.redirectUri);
  url.searchParams.set('code_challenge', opts.codeChallenge);
  url.searchParams.set('code_challenge_method', 'S256');
  url.searchParams.set('state', opts.state);
  url.searchParams.set('idp_hint', 'discord');
  url.searchParams.set('scope', 'commerce account social publishing analytics');
  return url.toString();
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
