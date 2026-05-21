/*
 * Copyright (c) 2022-2026 AccelByte Inc. All Rights Reserved
 * This is licensed software from AccelByte Inc, for limitations
 * and restrictions contact your company contract manager.
 */
/**
 * AUTO GENERATED
 */
import type { Response } from '@accelbyte/sdk'
import { Validate } from '@accelbyte/sdk'
import type { AxiosInstance, AxiosRequestConfig } from 'axios'
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

export class PlaytesthubService$ {
  private axiosInstance: AxiosInstance
  private useSchemaValidation: boolean

  constructor(axiosInstance: AxiosInstance, _namespace: string, useSchemaValidation = true) {
    this.axiosInstance = axiosInstance
    this.useSchemaValidation = useSchemaValidation
  }

  /**
   * Returns the caller's AGS user id plus the best-effort Discord handle resolved via the same bot-token lookup the signup flow uses. discord_handle is empty when the caller is not Discord-federated or the lookup fails — the field is informational; callers must not treat it as authoritative identity.
   */
  getPlayerMe(): Promise<Response<V1WhoAmIResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/player/me'
    const resultPromise = this.axiosInstance.get(url, { params })

    return Validate.validateOrReturnResponse(this.useSchemaValidation, () => resultPromise, V1WhoAmIResponse, 'V1WhoAmIResponse')
  }
  /**
   * Returns environment-derived client config that both the admin and player frontends need to construct cross-app URLs. player_base_url is the public origin of the player Svelte bundle (from backend env PLAYER_BASE_URL); empty string when unset.
   */
  getConfig(): Promise<Response<V1GetPublicConfigResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/public/config'
    const resultPromise = this.axiosInstance.get(url, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1GetPublicConfigResponse,
      'V1GetPublicConfigResponse'
    )
  }
  /**
   * Player runs Discord OAuth directly (Discord developer portal owns the redirect-URI allowlist). The resulting Discord authorization code is POSTed here; the backend authenticates with confidential AGS IAM credentials and calls /iam/v3/oauth/platforms/discord/token (platform-token grant). AGS auto-creates the Justice platform account on first call and returns AGS access + refresh tokens, which we forward verbatim. Replaces the auth-code federation flow attempted in STATUS.md M1 phase 9.2 — that flow's /iam/v3/oauth/token step always failed with invalid_grant in game namespaces because the auth-code path skips Justice-platform-account creation.
   */
  createPlayerDiscordExchange(data: V1ExchangeDiscordCodeRequest): Promise<Response<V1ExchangeDiscordCodeResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/player/discord/exchange'
    const resultPromise = this.axiosInstance.post(url, data, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1ExchangeDiscordCodeResponse,
      'V1ExchangeDiscordCodeResponse'
    )
  }
  /**
   * Returns the player-visible field set including NDA text and currentNdaVersionHash.
   */
  getPlayerPlaytest_BySlug(slug: string): Promise<Response<V1GetPlaytestForPlayerResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/player/playtests/{slug}'.replace('{slug}', slug)
    const resultPromise = this.axiosInstance.get(url, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1GetPlaytestForPlayerResponse,
      'V1GetPlaytestForPlayerResponse'
    )
  }
  /**
   * Returns the public field subset for an OPEN playtest. NotFound for DRAFT, CLOSED, or soft-deleted.
   */
  getPlaytest_BySlug(slug: string): Promise<Response<V1GetPublicPlaytestResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/public/playtests/{slug}'.replace('{slug}', slug)
    const resultPromise = this.axiosInstance.get(url, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1GetPublicPlaytestResponse,
      'V1GetPublicPlaytestResponse'
    )
  }

  createSignupPlayer_BySlug(slug: string, data: PlaytesthubServiceSignupBody): Promise<Response<V1SignupResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/player/playtests/{slug}/signup'.replace('{slug}', slug)
    const resultPromise = this.axiosInstance.post(url, data, { params })

    return Validate.validateOrReturnResponse(this.useSchemaValidation, () => resultPromise, V1SignupResponse, 'V1SignupResponse')
  }

  getApplicantPlayer_BySlug(slug: string): Promise<Response<V1GetApplicantStatusResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/player/playtests/{slug}/applicant'.replace('{slug}', slug)
    const resultPromise = this.axiosInstance.get(url, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1GetApplicantStatusResponse,
      'V1GetApplicantStatusResponse'
    )
  }
  /**
   * Returns the version pointed at by Playtest.surveyId. NotFound when the playtest has no survey.
   */
  getSurveyPlayer_ByPlaytestId(playtestId: string): Promise<Response<V1GetSurveyResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/player/playtests/{playtestId}/survey'.replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.get(url, { params })

    return Validate.validateOrReturnResponse(this.useSchemaValidation, () => resultPromise, V1GetSurveyResponse, 'V1GetSurveyResponse')
  }
  /**
   * Idempotent on (userId, playtestId, ndaVersionHash) per PRD §4.7. Backend recomputes the current hash from the live playtest row; second accept on the same tuple returns the existing NDAAcceptance, no error.
   */
  createPlayerPlaytest_ByPlaytestIdAcceptNda(
    playtestId: string,
    data: PlaytesthubServiceAcceptNdaBody
  ): Promise<Response<V1AcceptNdaResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/player/playtests/{playtestId}:acceptNda'.replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.post(url, data, { params })

    return Validate.validateOrReturnResponse(this.useSchemaValidation, () => resultPromise, V1AcceptNdaResponse, 'V1AcceptNdaResponse')
  }
  /**
   * FailedPrecondition for non-ADT playtests. URL re-minted on every call so the player always sees a fresh (possibly per-applicant) URL.
   */
  getAdtDownloadPlayer_ByPlaytestId(playtestId: string): Promise<Response<V1GetAdtDownloadInfoResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/player/playtests/{playtestId}/adtDownload'.replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.get(url, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1GetAdtDownloadInfoResponse,
      'V1GetAdtDownloadInfoResponse'
    )
  }
  /**
   * NotFound for any soft-deleted playtest regardless of applicant state (PRD §5.1 / errors.md). FailedPrecondition for ADT playtests — use GetADTDownloadInfo.
   */
  getGrantedCodePlayer_ByPlaytestId(playtestId: string): Promise<Response<V1GetGrantedCodeResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/player/playtests/{playtestId}/grantedCode'.replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.get(url, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1GetGrantedCodeResponse,
      'V1GetGrantedCodeResponse'
    )
  }
  /**
   * Natural-key on (playtestId, userId). Second submit returns gRPC AlreadyExists with empty body per errors.md row 31. The server records against the survey_id the client submitted — a concurrent EditSurvey does not invalidate an in-flight submit (PRD §5.6).
   */
  createSurveySubmitPlayer_ByPlaytestId(
    playtestId: string,
    data: PlaytesthubServiceSubmitSurveyResponseBody
  ): Promise<Response<V1SubmitSurveyResponseResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/player/playtests/{playtestId}/survey:submit'.replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.post(url, data, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1SubmitSurveyResponseResponse,
      'V1SubmitSurveyResponseResponse'
    )
  }
}
