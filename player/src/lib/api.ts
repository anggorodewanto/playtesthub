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

export type DistributionModel =
  | 'DISTRIBUTION_MODEL_UNSPECIFIED'
  | 'DISTRIBUTION_MODEL_STEAM_KEYS'
  | 'DISTRIBUTION_MODEL_AGS_CAMPAIGN';

export type PlaytestStatus =
  | 'PLAYTEST_STATUS_UNSPECIFIED'
  | 'PLAYTEST_STATUS_DRAFT'
  | 'PLAYTEST_STATUS_OPEN'
  | 'PLAYTEST_STATUS_CLOSED';

export type PlayerPlaytest = {
  id?: string;
  slug: string;
  title: string;
  description: string;
  bannerImageUrl?: string;
  platforms?: Platform[];
  startsAt?: string;
  endsAt?: string;
  status: PlaytestStatus;
  ndaRequired: boolean;
  ndaText: string;
  currentNdaVersionHash: string;
  distributionModel: DistributionModel;
};

export type NdaAcceptance = {
  userId: string;
  playtestId: string;
  ndaVersionHash: string;
  acceptedAt: string;
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

// Applicant for the authenticated player includes playtestId — needed
// to drive AcceptNDA, which is keyed on the UUID rather than the slug.
export type ApplicantWithPlaytestId = Applicant & { playtestId: string };

export async function fetchApplicantStatusWithIds(
  config: Config,
  slug: string,
): Promise<ApplicantWithPlaytestId> {
  const body = await doJson<{ applicant: ApplicantWithPlaytestId }>(
    config,
    `/v1/player/playtests/${encodeURIComponent(slug)}/applicant`,
    { method: 'GET', authed: true },
  );
  return body.applicant;
}

export async function fetchPlayerPlaytest(config: Config, slug: string): Promise<PlayerPlaytest> {
  const body = await doJson<{ playtest: PlayerPlaytest }>(
    config,
    `/v1/player/playtests/${encodeURIComponent(slug)}`,
    { method: 'GET', authed: true },
  );
  return body.playtest;
}

export async function acceptNda(config: Config, playtestId: string): Promise<NdaAcceptance> {
  const body = await doJson<{ acceptance: NdaAcceptance }>(
    config,
    `/v1/player/playtests/${encodeURIComponent(playtestId)}:acceptNda`,
    {
      method: 'POST',
      authed: true,
      headers: { 'Content-Type': 'application/json' },
      body: '{}',
    },
  );
  return body.acceptance;
}
