/*
 * Copyright (c) 2022-2026 AccelByte Inc. All Rights Reserved
 * This is licensed software from AccelByte Inc, for limitations
 * and restrictions contact your company contract manager.
 */
/**
 * AUTO GENERATED
 */
import type { AccelByteSDK, ApiError, SdkSetConfigParam } from '@accelbyte/sdk'
import type { UseMutationOptions, UseMutationResult, UseQueryOptions, UseQueryResult } from '@tanstack/react-query'
import { useMutation, useQuery } from '@tanstack/react-query'
import type { AxiosError, AxiosResponse } from 'axios'
import { PlaytesthubServiceApi } from '../PlaytesthubServiceApi.js'

import { PlaytesthubServiceAcceptNdaBody } from '../../generated-definitions/PlaytesthubServiceAcceptNdaBody.js'
import { PlaytesthubServiceSignupBody } from '../../generated-definitions/PlaytesthubServiceSignupBody.js'
import { PlaytesthubServiceSubmitSurveyResponseBody } from '../../generated-definitions/PlaytesthubServiceSubmitSurveyResponseBody.js'
import { V1AcceptNdaResponse } from '../../generated-definitions/V1AcceptNdaResponse.js'
import { V1ExchangeDiscordCodeRequest } from '../../generated-definitions/V1ExchangeDiscordCodeRequest.js'
import { V1ExchangeDiscordCodeResponse } from '../../generated-definitions/V1ExchangeDiscordCodeResponse.js'
import { V1GetAdtDownloadInfoResponse } from '../../generated-definitions/V1GetAdtDownloadInfoResponse.js'
import { V1GetApplicantStatusResponse } from '../../generated-definitions/V1GetApplicantStatusResponse.js'
import { V1GetGrantedCodeResponse } from '../../generated-definitions/V1GetGrantedCodeResponse.js'
import { V1GetPlaytestForPlayerResponse } from '../../generated-definitions/V1GetPlaytestForPlayerResponse.js'
import { V1GetPublicConfigResponse } from '../../generated-definitions/V1GetPublicConfigResponse.js'
import { V1GetPublicPlaytestResponse } from '../../generated-definitions/V1GetPublicPlaytestResponse.js'
import { V1GetSurveyResponse } from '../../generated-definitions/V1GetSurveyResponse.js'
import { V1SignupResponse } from '../../generated-definitions/V1SignupResponse.js'
import { V1SubmitSurveyResponseResponse } from '../../generated-definitions/V1SubmitSurveyResponseResponse.js'
import { V1WhoAmIResponse } from '../../generated-definitions/V1WhoAmIResponse.js'

export const Key_PlaytesthubService = {
  PlayerMe: 'Playtesthubapi.PlaytesthubService.PlayerMe',
  Config: 'Playtesthubapi.PlaytesthubService.Config',
  PlayerDiscordExchange: 'Playtesthubapi.PlaytesthubService.PlayerDiscordExchange',
  PlayerPlaytest_BySlug: 'Playtesthubapi.PlaytesthubService.PlayerPlaytest_BySlug',
  Playtest_BySlug: 'Playtesthubapi.PlaytesthubService.Playtest_BySlug',
  SignupPlayer_BySlug: 'Playtesthubapi.PlaytesthubService.SignupPlayer_BySlug',
  ApplicantPlayer_BySlug: 'Playtesthubapi.PlaytesthubService.ApplicantPlayer_BySlug',
  SurveyPlayer_ByPlaytestId: 'Playtesthubapi.PlaytesthubService.SurveyPlayer_ByPlaytestId',
  PlayerPlaytest_ByPlaytestIdAcceptNda: 'Playtesthubapi.PlaytesthubService.PlayerPlaytest_ByPlaytestIdAcceptNda',
  AdtDownloadPlayer_ByPlaytestId: 'Playtesthubapi.PlaytesthubService.AdtDownloadPlayer_ByPlaytestId',
  GrantedCodePlayer_ByPlaytestId: 'Playtesthubapi.PlaytesthubService.GrantedCodePlayer_ByPlaytestId',
  SurveySubmitPlayer_ByPlaytestId: 'Playtesthubapi.PlaytesthubService.SurveySubmitPlayer_ByPlaytestId'
} as const

/**
 * Returns the caller's AGS user id plus the best-effort Discord handle resolved via the same bot-token lookup the signup flow uses. discord_handle is empty when the caller is not Discord-federated or the lookup fails — the field is informational; callers must not treat it as authoritative identity.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubService.PlayerMe, input]
 * }
 * ```
 */
export const usePlaytesthubServiceApi_GetPlayerMe = (
  sdk: AccelByteSDK,
  input: SdkSetConfigParam,
  options?: Omit<UseQueryOptions<V1WhoAmIResponse, AxiosError<ApiError>>, 'queryKey'>,
  callback?: (data: AxiosResponse<V1WhoAmIResponse>) => void
): UseQueryResult<V1WhoAmIResponse, AxiosError<ApiError>> => {
  const queryFn = (sdk: AccelByteSDK, input: Parameters<typeof usePlaytesthubServiceApi_GetPlayerMe>[1]) => async () => {
    const response = await PlaytesthubServiceApi(sdk, { coreConfig: input.coreConfig, axiosConfig: input.axiosConfig }).getPlayerMe()
    callback?.(response)
    return response.data
  }

  return useQuery<V1WhoAmIResponse, AxiosError<ApiError>>({
    queryKey: [Key_PlaytesthubService.PlayerMe, input],
    queryFn: queryFn(sdk, input),
    ...options
  })
}

/**
 * Returns environment-derived client config that both the admin and player frontends need to construct cross-app URLs. player_base_url is the public origin of the player Svelte bundle (from backend env PLAYER_BASE_URL); empty string when unset.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubService.Config, input]
 * }
 * ```
 */
export const usePlaytesthubServiceApi_GetConfig = (
  sdk: AccelByteSDK,
  input: SdkSetConfigParam,
  options?: Omit<UseQueryOptions<V1GetPublicConfigResponse, AxiosError<ApiError>>, 'queryKey'>,
  callback?: (data: AxiosResponse<V1GetPublicConfigResponse>) => void
): UseQueryResult<V1GetPublicConfigResponse, AxiosError<ApiError>> => {
  const queryFn = (sdk: AccelByteSDK, input: Parameters<typeof usePlaytesthubServiceApi_GetConfig>[1]) => async () => {
    const response = await PlaytesthubServiceApi(sdk, { coreConfig: input.coreConfig, axiosConfig: input.axiosConfig }).getConfig()
    callback?.(response)
    return response.data
  }

  return useQuery<V1GetPublicConfigResponse, AxiosError<ApiError>>({
    queryKey: [Key_PlaytesthubService.Config, input],
    queryFn: queryFn(sdk, input),
    ...options
  })
}

/**
 * Player runs Discord OAuth directly (Discord developer portal owns the redirect-URI allowlist). The resulting Discord authorization code is POSTed here; the backend authenticates with confidential AGS IAM credentials and calls /iam/v3/oauth/platforms/discord/token (platform-token grant). AGS auto-creates the Justice platform account on first call and returns AGS access + refresh tokens, which we forward verbatim. Replaces the auth-code federation flow attempted in STATUS.md M1 phase 9.2 — that flow's /iam/v3/oauth/token step always failed with invalid_grant in game namespaces because the auth-code path skips Justice-platform-account creation.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubService.PlayerDiscordExchange, input]
 * }
 * ```
 */
export const usePlaytesthubServiceApi_CreatePlayerDiscordExchangeMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<V1ExchangeDiscordCodeResponse, AxiosError<ApiError>, SdkSetConfigParam & { data: V1ExchangeDiscordCodeRequest }>,
    'mutationKey'
  >,
  callback?: (data: V1ExchangeDiscordCodeResponse) => void
): UseMutationResult<V1ExchangeDiscordCodeResponse, AxiosError<ApiError>, SdkSetConfigParam & { data: V1ExchangeDiscordCodeRequest }> => {
  const mutationFn = async (input: SdkSetConfigParam & { data: V1ExchangeDiscordCodeRequest }) => {
    const response = await PlaytesthubServiceApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).createPlayerDiscordExchange(input.data)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubService.PlayerDiscordExchange],
    mutationFn,
    ...options
  })
}

/**
 * Returns the player-visible field set including NDA text and currentNdaVersionHash.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubService.PlayerPlaytest_BySlug, input]
 * }
 * ```
 */
export const usePlaytesthubServiceApi_GetPlayerPlaytest_BySlug = (
  sdk: AccelByteSDK,
  input: SdkSetConfigParam & { slug: string },
  options?: Omit<UseQueryOptions<V1GetPlaytestForPlayerResponse, AxiosError<ApiError>>, 'queryKey'>,
  callback?: (data: AxiosResponse<V1GetPlaytestForPlayerResponse>) => void
): UseQueryResult<V1GetPlaytestForPlayerResponse, AxiosError<ApiError>> => {
  const queryFn = (sdk: AccelByteSDK, input: Parameters<typeof usePlaytesthubServiceApi_GetPlayerPlaytest_BySlug>[1]) => async () => {
    const response = await PlaytesthubServiceApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).getPlayerPlaytest_BySlug(input.slug)
    callback?.(response)
    return response.data
  }

  return useQuery<V1GetPlaytestForPlayerResponse, AxiosError<ApiError>>({
    queryKey: [Key_PlaytesthubService.PlayerPlaytest_BySlug, input],
    queryFn: queryFn(sdk, input),
    ...options
  })
}

/**
 * Returns the public field subset for an OPEN playtest. NotFound for DRAFT, CLOSED, or soft-deleted.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubService.Playtest_BySlug, input]
 * }
 * ```
 */
export const usePlaytesthubServiceApi_GetPlaytest_BySlug = (
  sdk: AccelByteSDK,
  input: SdkSetConfigParam & { slug: string },
  options?: Omit<UseQueryOptions<V1GetPublicPlaytestResponse, AxiosError<ApiError>>, 'queryKey'>,
  callback?: (data: AxiosResponse<V1GetPublicPlaytestResponse>) => void
): UseQueryResult<V1GetPublicPlaytestResponse, AxiosError<ApiError>> => {
  const queryFn = (sdk: AccelByteSDK, input: Parameters<typeof usePlaytesthubServiceApi_GetPlaytest_BySlug>[1]) => async () => {
    const response = await PlaytesthubServiceApi(sdk, { coreConfig: input.coreConfig, axiosConfig: input.axiosConfig }).getPlaytest_BySlug(
      input.slug
    )
    callback?.(response)
    return response.data
  }

  return useQuery<V1GetPublicPlaytestResponse, AxiosError<ApiError>>({
    queryKey: [Key_PlaytesthubService.Playtest_BySlug, input],
    queryFn: queryFn(sdk, input),
    ...options
  })
}

export const usePlaytesthubServiceApi_CreateSignupPlayer_BySlugMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<V1SignupResponse, AxiosError<ApiError>, SdkSetConfigParam & { slug: string; data: PlaytesthubServiceSignupBody }>,
    'mutationKey'
  >,
  callback?: (data: V1SignupResponse) => void
): UseMutationResult<V1SignupResponse, AxiosError<ApiError>, SdkSetConfigParam & { slug: string; data: PlaytesthubServiceSignupBody }> => {
  const mutationFn = async (input: SdkSetConfigParam & { slug: string; data: PlaytesthubServiceSignupBody }) => {
    const response = await PlaytesthubServiceApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).createSignupPlayer_BySlug(input.slug, input.data)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubService.SignupPlayer_BySlug],
    mutationFn,
    ...options
  })
}

export const usePlaytesthubServiceApi_GetApplicantPlayer_BySlug = (
  sdk: AccelByteSDK,
  input: SdkSetConfigParam & { slug: string },
  options?: Omit<UseQueryOptions<V1GetApplicantStatusResponse, AxiosError<ApiError>>, 'queryKey'>,
  callback?: (data: AxiosResponse<V1GetApplicantStatusResponse>) => void
): UseQueryResult<V1GetApplicantStatusResponse, AxiosError<ApiError>> => {
  const queryFn = (sdk: AccelByteSDK, input: Parameters<typeof usePlaytesthubServiceApi_GetApplicantPlayer_BySlug>[1]) => async () => {
    const response = await PlaytesthubServiceApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).getApplicantPlayer_BySlug(input.slug)
    callback?.(response)
    return response.data
  }

  return useQuery<V1GetApplicantStatusResponse, AxiosError<ApiError>>({
    queryKey: [Key_PlaytesthubService.ApplicantPlayer_BySlug, input],
    queryFn: queryFn(sdk, input),
    ...options
  })
}

/**
 * Returns the version pointed at by Playtest.surveyId. NotFound when the playtest has no survey.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubService.SurveyPlayer_ByPlaytestId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceApi_GetSurveyPlayer_ByPlaytestId = (
  sdk: AccelByteSDK,
  input: SdkSetConfigParam & { playtestId: string },
  options?: Omit<UseQueryOptions<V1GetSurveyResponse, AxiosError<ApiError>>, 'queryKey'>,
  callback?: (data: AxiosResponse<V1GetSurveyResponse>) => void
): UseQueryResult<V1GetSurveyResponse, AxiosError<ApiError>> => {
  const queryFn = (sdk: AccelByteSDK, input: Parameters<typeof usePlaytesthubServiceApi_GetSurveyPlayer_ByPlaytestId>[1]) => async () => {
    const response = await PlaytesthubServiceApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).getSurveyPlayer_ByPlaytestId(input.playtestId)
    callback?.(response)
    return response.data
  }

  return useQuery<V1GetSurveyResponse, AxiosError<ApiError>>({
    queryKey: [Key_PlaytesthubService.SurveyPlayer_ByPlaytestId, input],
    queryFn: queryFn(sdk, input),
    ...options
  })
}

/**
 * Idempotent on (userId, playtestId, ndaVersionHash) per PRD §4.7. Backend recomputes the current hash from the live playtest row; second accept on the same tuple returns the existing NDAAcceptance, no error.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubService.PlayerPlaytest_ByPlaytestIdAcceptNda, input]
 * }
 * ```
 */
export const usePlaytesthubServiceApi_CreatePlayerPlaytest_ByPlaytestIdAcceptNdaMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<
      V1AcceptNdaResponse,
      AxiosError<ApiError>,
      SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceAcceptNdaBody }
    >,
    'mutationKey'
  >,
  callback?: (data: V1AcceptNdaResponse) => void
): UseMutationResult<
  V1AcceptNdaResponse,
  AxiosError<ApiError>,
  SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceAcceptNdaBody }
> => {
  const mutationFn = async (input: SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceAcceptNdaBody }) => {
    const response = await PlaytesthubServiceApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).createPlayerPlaytest_ByPlaytestIdAcceptNda(input.playtestId, input.data)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubService.PlayerPlaytest_ByPlaytestIdAcceptNda],
    mutationFn,
    ...options
  })
}

/**
 * FailedPrecondition for non-ADT playtests. URL re-minted on every call so the player always sees a fresh (possibly per-applicant) URL.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubService.AdtDownloadPlayer_ByPlaytestId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceApi_GetAdtDownloadPlayer_ByPlaytestId = (
  sdk: AccelByteSDK,
  input: SdkSetConfigParam & { playtestId: string },
  options?: Omit<UseQueryOptions<V1GetAdtDownloadInfoResponse, AxiosError<ApiError>>, 'queryKey'>,
  callback?: (data: AxiosResponse<V1GetAdtDownloadInfoResponse>) => void
): UseQueryResult<V1GetAdtDownloadInfoResponse, AxiosError<ApiError>> => {
  const queryFn =
    (sdk: AccelByteSDK, input: Parameters<typeof usePlaytesthubServiceApi_GetAdtDownloadPlayer_ByPlaytestId>[1]) => async () => {
      const response = await PlaytesthubServiceApi(sdk, {
        coreConfig: input.coreConfig,
        axiosConfig: input.axiosConfig
      }).getAdtDownloadPlayer_ByPlaytestId(input.playtestId)
      callback?.(response)
      return response.data
    }

  return useQuery<V1GetAdtDownloadInfoResponse, AxiosError<ApiError>>({
    queryKey: [Key_PlaytesthubService.AdtDownloadPlayer_ByPlaytestId, input],
    queryFn: queryFn(sdk, input),
    ...options
  })
}

/**
 * NotFound for any soft-deleted playtest regardless of applicant state (PRD §5.1 / errors.md). FailedPrecondition for ADT playtests — use GetADTDownloadInfo.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubService.GrantedCodePlayer_ByPlaytestId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceApi_GetGrantedCodePlayer_ByPlaytestId = (
  sdk: AccelByteSDK,
  input: SdkSetConfigParam & { playtestId: string },
  options?: Omit<UseQueryOptions<V1GetGrantedCodeResponse, AxiosError<ApiError>>, 'queryKey'>,
  callback?: (data: AxiosResponse<V1GetGrantedCodeResponse>) => void
): UseQueryResult<V1GetGrantedCodeResponse, AxiosError<ApiError>> => {
  const queryFn =
    (sdk: AccelByteSDK, input: Parameters<typeof usePlaytesthubServiceApi_GetGrantedCodePlayer_ByPlaytestId>[1]) => async () => {
      const response = await PlaytesthubServiceApi(sdk, {
        coreConfig: input.coreConfig,
        axiosConfig: input.axiosConfig
      }).getGrantedCodePlayer_ByPlaytestId(input.playtestId)
      callback?.(response)
      return response.data
    }

  return useQuery<V1GetGrantedCodeResponse, AxiosError<ApiError>>({
    queryKey: [Key_PlaytesthubService.GrantedCodePlayer_ByPlaytestId, input],
    queryFn: queryFn(sdk, input),
    ...options
  })
}

/**
 * Natural-key on (playtestId, userId). Second submit returns gRPC AlreadyExists with empty body per errors.md row 31. The server records against the survey_id the client submitted — a concurrent EditSurvey does not invalidate an in-flight submit (PRD §5.6).
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubService.SurveySubmitPlayer_ByPlaytestId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceApi_CreateSurveySubmitPlayer_ByPlaytestIdMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<
      V1SubmitSurveyResponseResponse,
      AxiosError<ApiError>,
      SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceSubmitSurveyResponseBody }
    >,
    'mutationKey'
  >,
  callback?: (data: V1SubmitSurveyResponseResponse) => void
): UseMutationResult<
  V1SubmitSurveyResponseResponse,
  AxiosError<ApiError>,
  SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceSubmitSurveyResponseBody }
> => {
  const mutationFn = async (input: SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceSubmitSurveyResponseBody }) => {
    const response = await PlaytesthubServiceApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).createSurveySubmitPlayer_ByPlaytestId(input.playtestId, input.data)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubService.SurveySubmitPlayer_ByPlaytestId],
    mutationFn,
    ...options
  })
}
