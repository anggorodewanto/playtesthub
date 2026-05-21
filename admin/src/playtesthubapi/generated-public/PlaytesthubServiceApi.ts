/*
 * Copyright (c) 2022-2026 AccelByte Inc. All Rights Reserved
 * This is licensed software from AccelByte Inc, for limitations
 * and restrictions contact your company contract manager.
 */
/**
 * AUTO GENERATED
 */
import type { AccelByteSDK, SdkSetConfigParam } from '@accelbyte/sdk'
import { ApiUtils, Network } from '@accelbyte/sdk'
import type { AxiosRequestConfig, AxiosResponse } from 'axios'
import { PlaytesthubServiceAcceptNdaBody } from '../generated-definitions/PlaytesthubServiceAcceptNdaBody.js'
import { PlaytesthubServiceSignupBody } from '../generated-definitions/PlaytesthubServiceSignupBody.js'
import { PlaytesthubServiceSubmitSurveyResponseBody } from '../generated-definitions/PlaytesthubServiceSubmitSurveyResponseBody.js'
import { V1AcceptNdaResponse } from '../generated-definitions/V1AcceptNdaResponse.js'
import { V1ExchangeDiscordCodeRequest } from '../generated-definitions/V1ExchangeDiscordCodeRequest.js'
import { V1ExchangeDiscordCodeResponse } from '../generated-definitions/V1ExchangeDiscordCodeResponse.js'
import { V1GetAdtDownloadInfoResponse } from '../generated-definitions/V1GetAdtDownloadInfoResponse.js'
import { V1GetApplicantStatusResponse } from '../generated-definitions/V1GetApplicantStatusResponse.js'
import { V1GetGrantedCodeResponse } from '../generated-definitions/V1GetGrantedCodeResponse.js'
import { V1GetPlaytestForPlayerResponse } from '../generated-definitions/V1GetPlaytestForPlayerResponse.js'
import { V1GetPublicConfigResponse } from '../generated-definitions/V1GetPublicConfigResponse.js'
import { V1GetPublicPlaytestResponse } from '../generated-definitions/V1GetPublicPlaytestResponse.js'
import { V1GetSurveyResponse } from '../generated-definitions/V1GetSurveyResponse.js'
import { V1SignupResponse } from '../generated-definitions/V1SignupResponse.js'
import { V1SubmitSurveyResponseResponse } from '../generated-definitions/V1SubmitSurveyResponseResponse.js'
import { V1WhoAmIResponse } from '../generated-definitions/V1WhoAmIResponse.js'
import { PlaytesthubService$ } from './endpoints/PlaytesthubService$.js'

export function PlaytesthubServiceApi(sdk: AccelByteSDK, args?: SdkSetConfigParam) {
  const sdkAssembly = sdk.assembly()

  const namespace = args?.coreConfig?.namespace ?? sdkAssembly.coreConfig.namespace
  const useSchemaValidation = args?.coreConfig?.useSchemaValidation ?? sdkAssembly.coreConfig.useSchemaValidation

  let axiosInstance = sdkAssembly.axiosInstance
  const requestConfigOverrides = args?.axiosConfig?.request
  const baseURLOverride = args?.coreConfig?.baseURL
  const interceptorsOverride = args?.axiosConfig?.interceptors

  if (requestConfigOverrides || baseURLOverride || interceptorsOverride) {
    const requestConfig = ApiUtils.mergeAxiosConfigs(sdkAssembly.axiosInstance.defaults as AxiosRequestConfig, {
      ...(baseURLOverride ? { baseURL: baseURLOverride } : {}),
      ...requestConfigOverrides
    })
    axiosInstance = Network.create(requestConfig)

    if (interceptorsOverride) {
      for (const interceptor of interceptorsOverride) {
        if (interceptor.type === 'request') {
          axiosInstance.interceptors.request.use(interceptor.onRequest, interceptor.onError)
        }

        if (interceptor.type === 'response') {
          axiosInstance.interceptors.response.use(interceptor.onSuccess, interceptor.onError)
        }
      }
    } else {
      axiosInstance.interceptors = sdkAssembly.axiosInstance.interceptors
    }
  }

  async function getPlayerMe(): Promise<AxiosResponse<V1WhoAmIResponse>> {
    const $ = new PlaytesthubService$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.getPlayerMe()
    if (resp.error) throw resp.error
    return resp.response
  }

  async function getConfig(): Promise<AxiosResponse<V1GetPublicConfigResponse>> {
    const $ = new PlaytesthubService$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.getConfig()
    if (resp.error) throw resp.error
    return resp.response
  }

  async function createPlayerDiscordExchange(data: V1ExchangeDiscordCodeRequest): Promise<AxiosResponse<V1ExchangeDiscordCodeResponse>> {
    const $ = new PlaytesthubService$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.createPlayerDiscordExchange(data)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function getPlayerPlaytest_BySlug(slug: string): Promise<AxiosResponse<V1GetPlaytestForPlayerResponse>> {
    const $ = new PlaytesthubService$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.getPlayerPlaytest_BySlug(slug)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function getPlaytest_BySlug(slug: string): Promise<AxiosResponse<V1GetPublicPlaytestResponse>> {
    const $ = new PlaytesthubService$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.getPlaytest_BySlug(slug)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function createSignupPlayer_BySlug(slug: string, data: PlaytesthubServiceSignupBody): Promise<AxiosResponse<V1SignupResponse>> {
    const $ = new PlaytesthubService$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.createSignupPlayer_BySlug(slug, data)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function getApplicantPlayer_BySlug(slug: string): Promise<AxiosResponse<V1GetApplicantStatusResponse>> {
    const $ = new PlaytesthubService$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.getApplicantPlayer_BySlug(slug)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function getSurveyPlayer_ByPlaytestId(playtestId: string): Promise<AxiosResponse<V1GetSurveyResponse>> {
    const $ = new PlaytesthubService$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.getSurveyPlayer_ByPlaytestId(playtestId)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function createPlayerPlaytest_ByPlaytestIdAcceptNda(
    playtestId: string,
    data: PlaytesthubServiceAcceptNdaBody
  ): Promise<AxiosResponse<V1AcceptNdaResponse>> {
    const $ = new PlaytesthubService$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.createPlayerPlaytest_ByPlaytestIdAcceptNda(playtestId, data)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function getAdtDownloadPlayer_ByPlaytestId(playtestId: string): Promise<AxiosResponse<V1GetAdtDownloadInfoResponse>> {
    const $ = new PlaytesthubService$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.getAdtDownloadPlayer_ByPlaytestId(playtestId)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function getGrantedCodePlayer_ByPlaytestId(playtestId: string): Promise<AxiosResponse<V1GetGrantedCodeResponse>> {
    const $ = new PlaytesthubService$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.getGrantedCodePlayer_ByPlaytestId(playtestId)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function createSurveySubmitPlayer_ByPlaytestId(
    playtestId: string,
    data: PlaytesthubServiceSubmitSurveyResponseBody
  ): Promise<AxiosResponse<V1SubmitSurveyResponseResponse>> {
    const $ = new PlaytesthubService$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.createSurveySubmitPlayer_ByPlaytestId(playtestId, data)
    if (resp.error) throw resp.error
    return resp.response
  }

  return {
    /**
     * Returns the caller's AGS user id plus the best-effort Discord handle resolved via the same bot-token lookup the signup flow uses. discord_handle is empty when the caller is not Discord-federated or the lookup fails — the field is informational; callers must not treat it as authoritative identity.
     */
    getPlayerMe,
    /**
     * Returns environment-derived client config that both the admin and player frontends need to construct cross-app URLs. player_base_url is the public origin of the player Svelte bundle (from backend env PLAYER_BASE_URL); empty string when unset.
     */
    getConfig,
    /**
     * Player runs Discord OAuth directly (Discord developer portal owns the redirect-URI allowlist). The resulting Discord authorization code is POSTed here; the backend authenticates with confidential AGS IAM credentials and calls /iam/v3/oauth/platforms/discord/token (platform-token grant). AGS auto-creates the Justice platform account on first call and returns AGS access + refresh tokens, which we forward verbatim. Replaces the auth-code federation flow attempted in STATUS.md M1 phase 9.2 — that flow's /iam/v3/oauth/token step always failed with invalid_grant in game namespaces because the auth-code path skips Justice-platform-account creation.
     */
    createPlayerDiscordExchange,
    /**
     * Returns the player-visible field set including NDA text and currentNdaVersionHash.
     */
    getPlayerPlaytest_BySlug,
    /**
     * Returns the public field subset for an OPEN playtest. NotFound for DRAFT, CLOSED, or soft-deleted.
     */
    getPlaytest_BySlug,

    createSignupPlayer_BySlug,

    getApplicantPlayer_BySlug,
    /**
     * Returns the version pointed at by Playtest.surveyId. NotFound when the playtest has no survey.
     */
    getSurveyPlayer_ByPlaytestId,
    /**
     * Idempotent on (userId, playtestId, ndaVersionHash) per PRD §4.7. Backend recomputes the current hash from the live playtest row; second accept on the same tuple returns the existing NDAAcceptance, no error.
     */
    createPlayerPlaytest_ByPlaytestIdAcceptNda,
    /**
     * FailedPrecondition for non-ADT playtests. URL re-minted on every call so the player always sees a fresh (possibly per-applicant) URL.
     */
    getAdtDownloadPlayer_ByPlaytestId,
    /**
     * NotFound for any soft-deleted playtest regardless of applicant state (PRD §5.1 / errors.md). FailedPrecondition for ADT playtests — use GetADTDownloadInfo.
     */
    getGrantedCodePlayer_ByPlaytestId,
    /**
     * Natural-key on (playtestId, userId). Second submit returns gRPC AlreadyExists with empty body per errors.md row 31. The server records against the survey_id the client submitted — a concurrent EditSurvey does not invalidate an in-flight submit (PRD §5.6).
     */
    createSurveySubmitPlayer_ByPlaytestId
  }
}
