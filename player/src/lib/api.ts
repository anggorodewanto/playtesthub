import type { Config } from './config';
import { getAccessToken } from './auth';

export type Platform =
  | 'PLATFORM_UNSPECIFIED'
  | 'PLATFORM_STEAM'
  | 'PLATFORM_XBOX'
  | 'PLATFORM_PLAYSTATION'
  | 'PLATFORM_EPIC'
  | 'PLATFORM_OTHER';

export type PublicPlaytest = {
  slug: string;
  title: string;
  description: string;
  bannerImageUrl?: string;
  platforms?: Platform[];
  startsAt?: string;
  endsAt?: string;
};

export type ApplicantStatus =
  | 'APPLICANT_STATUS_UNSPECIFIED'
  | 'APPLICANT_STATUS_PENDING'
  | 'APPLICANT_STATUS_APPROVED'
  | 'APPLICANT_STATUS_REJECTED';

export type Applicant = {
  id: string;
  status: ApplicantStatus;
  grantedCodeId?: string;
  approvedAt?: string;
  ndaVersionHash?: string;
};

export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(`${status}: ${message}`);
    this.name = 'ApiError';
    this.status = status;
  }
}

function joinPath(base: string, path: string): string {
  const trimmedBase = base.replace(/\/$/, '');
  const leading = path.startsWith('/') ? path : `/${path}`;
  return `${trimmedBase}${leading}`;
}

async function parseErrorBody(res: Response): Promise<string> {
  const text = await res.text().catch(() => '');
  if (!text) return res.statusText || 'request failed';
  try {
    const obj = JSON.parse(text) as { message?: string };
    return obj.message ?? text;
  } catch {
    return text;
  }
}

async function doJson<T>(
  config: Config,
  path: string,
  init: RequestInit & { authed: boolean },
): Promise<T> {
  const headers: Record<string, string> = {
    Accept: 'application/json',
    ...((init.headers as Record<string, string>) ?? {}),
  };

  if (init.authed) {
    const token = getAccessToken();
    if (!token) throw new ApiError(401, 'not authenticated');
    headers['Authorization'] = `Bearer ${token}`;
  }

  const res = await fetch(joinPath(config.grpcGatewayUrl, path), {
    ...init,
    headers,
  });
  if (!res.ok) {
    throw new ApiError(res.status, await parseErrorBody(res));
  }
  return (await res.json()) as T;
}

export async function fetchPublicPlaytest(config: Config, slug: string): Promise<PublicPlaytest> {
  const body = await doJson<{ playtest: PublicPlaytest }>(
    config,
    `/v1/public/playtests/${encodeURIComponent(slug)}`,
    { method: 'GET', authed: false },
  );
  return body.playtest;
}

export async function submitSignup(
  config: Config,
  slug: string,
  payload: { platforms: Platform[] },
): Promise<Applicant> {
  const body = await doJson<{ applicant: Applicant }>(
    config,
    `/v1/player/playtests/${encodeURIComponent(slug)}/signup`,
    {
      method: 'POST',
      authed: true,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    },
  );
  return body.applicant;
}

export async function fetchApplicantStatus(config: Config, slug: string): Promise<Applicant> {
  const body = await doJson<{ applicant: Applicant }>(
    config,
    `/v1/player/playtests/${encodeURIComponent(slug)}/applicant`,
    { method: 'GET', authed: true },
  );
  return body.applicant;
}
