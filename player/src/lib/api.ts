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
  | 'DISTRIBUTION_MODEL_AGS_CAMPAIGN'
  | 'DISTRIBUTION_MODEL_ADT';

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
  surveyId?: string;
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
  // PRD §5.6: when the caller has already submitted a survey response
  // for this playtest, the server returns the RFC3339 submission
  // timestamp so the Pending CTA can flip between "Submit feedback" and
  // "Feedback submitted ✓" without an extra round-trip.
  surveyResponseSubmittedAt?: string;
};

export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(`${status}: ${message}`);
    this.name = 'ApiError';
    this.status = status;
  }
}

export function joinPath(base: string, path: string): string {
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

export async function doJson<T>(
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

export type MyProfile = {
  userId: string;
  discordHandle: string;
  discordId: string;
};

export async function fetchMyProfile(config: Config): Promise<MyProfile> {
  const body = await doJson<Partial<MyProfile>>(config, '/v1/player/me', {
    method: 'GET',
    authed: true,
  });
  return {
    userId: body.userId ?? '',
    discordHandle: body.discordHandle ?? '',
    discordId: body.discordId ?? '',
  };
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

export type GrantedCode = {
  value: string;
  distributionModel: DistributionModel;
};

export async function fetchGrantedCode(
  config: Config,
  playtestId: string,
): Promise<GrantedCode> {
  return doJson<GrantedCode>(
    config,
    `/v1/player/playtests/${encodeURIComponent(playtestId)}/grantedCode`,
    { method: 'GET', authed: true },
  );
}

// AdtDownloadInfo mirrors the GetADTDownloadInfoResponse proto shape.
// `urls` lists every download URL ADT minted in original order — a
// single-element list for single-file builds, multiple elements for
// multi-asset builds. `source` is "issued" (ADT-minted URLs) or
// "fallback" (static per-playtest URL, single-element list) — surfaced
// in the UI so the player can tell the two apart per dm-queue.md
// "DM body shape — ADT".
export type AdtDownloadInfo = {
  urls: string[];
  expiresAt?: string;
  source: 'issued' | 'fallback' | string;
};

export async function fetchAdtDownloadInfo(
  config: Config,
  playtestId: string,
): Promise<AdtDownloadInfo> {
  return doJson<AdtDownloadInfo>(
    config,
    `/v1/player/playtests/${encodeURIComponent(playtestId)}/adtDownload`,
    { method: 'GET', authed: true },
  );
}

// Survey wire types — mirror proto/playtesthub/v1 SurveyQuestion /
// SurveyAnswer / Survey via the grpc-gateway camelCase JSON shape
// (see gateway/apidocs/api.swagger.json definitions v1Survey* +
// PlaytesthubServiceSubmitSurveyResponseBody).
export type SurveyQuestionType =
  | 'SURVEY_QUESTION_TYPE_UNSPECIFIED'
  | 'SURVEY_QUESTION_TYPE_TEXT'
  | 'SURVEY_QUESTION_TYPE_RATING'
  | 'SURVEY_QUESTION_TYPE_MULTI_CHOICE';

export type MultiChoiceOption = {
  id: string;
  label: string;
};

export type SurveyQuestion = {
  id: string;
  type: SurveyQuestionType;
  prompt: string;
  required?: boolean;
  options?: MultiChoiceOption[];
  allowMultiple?: boolean;
};

export type Survey = {
  id: string;
  playtestId: string;
  version: number;
  questions: SurveyQuestion[];
  createdAt?: string;
};

// SurveyAnswerInput is the player-form ⇄ wire shape for one answered
// question. Exactly one of `text`/`rating`/`multiChoice` is set per
// entry — server enforces the oneof on submit.
export type SurveyAnswerInput =
  | { questionId: string; text: string }
  | { questionId: string; rating: number }
  | { questionId: string; multiChoice: { optionIds: string[] } };

export type SurveyResponse = {
  id: string;
  playtestId: string;
  userId: string;
  surveyId: string;
  answers: SurveyAnswerInput[];
  submittedAt?: string;
};

export async function fetchSurvey(config: Config, playtestId: string): Promise<Survey> {
  const body = await doJson<{ survey: Survey }>(
    config,
    `/v1/player/playtests/${encodeURIComponent(playtestId)}/survey`,
    { method: 'GET', authed: true },
  );
  return body.survey;
}

export async function submitSurveyResponse(
  config: Config,
  playtestId: string,
  surveyId: string,
  answers: SurveyAnswerInput[],
): Promise<SurveyResponse | null> {
  const body = await doJson<{ response?: SurveyResponse }>(
    config,
    `/v1/player/playtests/${encodeURIComponent(playtestId)}/survey:submit`,
    {
      method: 'POST',
      authed: true,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ surveyId, answers }),
    },
  );
  return body.response ?? null;
}
