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
import { PlaytesthubServiceApproveApplicantBody } from '../generated-definitions/PlaytesthubServiceApproveApplicantBody.js'
import { PlaytesthubServiceCompleteAdtLinkBody } from '../generated-definitions/PlaytesthubServiceCompleteAdtLinkBody.js'
import { PlaytesthubServiceCreateAnnouncementBody } from '../generated-definitions/PlaytesthubServiceCreateAnnouncementBody.js'
import { PlaytesthubServiceCreatePlaytestBody } from '../generated-definitions/PlaytesthubServiceCreatePlaytestBody.js'
import { PlaytesthubServiceCreateSurveyBody } from '../generated-definitions/PlaytesthubServiceCreateSurveyBody.js'
import { PlaytesthubServiceEditPlaytestBody } from '../generated-definitions/PlaytesthubServiceEditPlaytestBody.js'
import { PlaytesthubServiceEditSurveyBody } from '../generated-definitions/PlaytesthubServiceEditSurveyBody.js'
import { PlaytesthubServiceRecoverAdtLinkageBody } from '../generated-definitions/PlaytesthubServiceRecoverAdtLinkageBody.js'
import { PlaytesthubServiceRejectApplicantBody } from '../generated-definitions/PlaytesthubServiceRejectApplicantBody.js'
import { PlaytesthubServiceRetryDmBody } from '../generated-definitions/PlaytesthubServiceRetryDmBody.js'
import { PlaytesthubServiceRetryFailedDmsBody } from '../generated-definitions/PlaytesthubServiceRetryFailedDmsBody.js'
import { PlaytesthubServiceStartAdtLinkBody } from '../generated-definitions/PlaytesthubServiceStartAdtLinkBody.js'
import { PlaytesthubServiceSyncFromAgsBody } from '../generated-definitions/PlaytesthubServiceSyncFromAgsBody.js'
import { PlaytesthubServiceTopUpCodesBody } from '../generated-definitions/PlaytesthubServiceTopUpCodesBody.js'
import { PlaytesthubServiceTransitionPlaytestStatusBody } from '../generated-definitions/PlaytesthubServiceTransitionPlaytestStatusBody.js'
import { PlaytesthubServiceUploadCodesBody } from '../generated-definitions/PlaytesthubServiceUploadCodesBody.js'
import { V1AdminGetPlaytestResponse } from '../generated-definitions/V1AdminGetPlaytestResponse.js'
import { V1ApproveApplicantResponse } from '../generated-definitions/V1ApproveApplicantResponse.js'
import { V1CompleteAdtLinkResponse } from '../generated-definitions/V1CompleteAdtLinkResponse.js'
import { V1CreateAnnouncementResponse } from '../generated-definitions/V1CreateAnnouncementResponse.js'
import { V1CreatePlaytestResponse } from '../generated-definitions/V1CreatePlaytestResponse.js'
import { V1CreateSurveyResponse } from '../generated-definitions/V1CreateSurveyResponse.js'
import { V1EditPlaytestResponse } from '../generated-definitions/V1EditPlaytestResponse.js'
import { V1EditSurveyResponse } from '../generated-definitions/V1EditSurveyResponse.js'
import { V1GetAdtClientDiagnosticsResponse } from '../generated-definitions/V1GetAdtClientDiagnosticsResponse.js'
import { V1GetCodePoolResponse } from '../generated-definitions/V1GetCodePoolResponse.js'
import { V1GetPlaytestParticipantsResponse } from '../generated-definitions/V1GetPlaytestParticipantsResponse.js'
import { V1GetWorkerHealthResponse } from '../generated-definitions/V1GetWorkerHealthResponse.js'
import { V1ListAdtBuildsResponse } from '../generated-definitions/V1ListAdtBuildsResponse.js'
import { V1ListAdtGamesResponse } from '../generated-definitions/V1ListAdtGamesResponse.js'
import { V1ListAdtLinkagesResponse } from '../generated-definitions/V1ListAdtLinkagesResponse.js'
import { V1ListAnnouncementsResponse } from '../generated-definitions/V1ListAnnouncementsResponse.js'
import { V1ListApplicantsResponse } from '../generated-definitions/V1ListApplicantsResponse.js'
import { V1ListAuditLogResponse } from '../generated-definitions/V1ListAuditLogResponse.js'
import { V1ListPlaytestsResponse } from '../generated-definitions/V1ListPlaytestsResponse.js'
import { V1ListSurveyResponsesResponse } from '../generated-definitions/V1ListSurveyResponsesResponse.js'
import { V1RecoverAdtLinkageResponse } from '../generated-definitions/V1RecoverAdtLinkageResponse.js'
import { V1RejectApplicantResponse } from '../generated-definitions/V1RejectApplicantResponse.js'
import { V1RetryDmResponse } from '../generated-definitions/V1RetryDmResponse.js'
import { V1RetryFailedDmsResponse } from '../generated-definitions/V1RetryFailedDmsResponse.js'
import { V1SoftDeletePlaytestResponse } from '../generated-definitions/V1SoftDeletePlaytestResponse.js'
import { V1StartAdtLinkResponse } from '../generated-definitions/V1StartAdtLinkResponse.js'
import { V1SyncFromAgsResponse } from '../generated-definitions/V1SyncFromAgsResponse.js'
import { V1TopUpCodesResponse } from '../generated-definitions/V1TopUpCodesResponse.js'
import { V1TransitionPlaytestStatusResponse } from '../generated-definitions/V1TransitionPlaytestStatusResponse.js'
import { V1UnlinkAdtResponse } from '../generated-definitions/V1UnlinkAdtResponse.js'
import { V1UploadCodesResponse } from '../generated-definitions/V1UploadCodesResponse.js'
import { PlaytesthubServiceAdmin$ } from './endpoints/PlaytesthubServiceAdmin$.js'

export function PlaytesthubServiceAdminApi(sdk: AccelByteSDK, args?: SdkSetConfigParam) {
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

  async function getPlaytests(): Promise<AxiosResponse<V1ListPlaytestsResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.getPlaytests()
    if (resp.error) throw resp.error
    return resp.response
  }

  async function createPlaytest(data: PlaytesthubServiceCreatePlaytestBody): Promise<AxiosResponse<V1CreatePlaytestResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.createPlaytest(data)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function getAdtLinkages(): Promise<AxiosResponse<V1ListAdtLinkagesResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.getAdtLinkages()
    if (resp.error) throw resp.error
    return resp.response
  }

  async function getWorkersHealth(): Promise<AxiosResponse<V1GetWorkerHealthResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.getWorkersHealth()
    if (resp.error) throw resp.error
    return resp.response
  }

  async function createAdtLinkagesStart(data: PlaytesthubServiceStartAdtLinkBody): Promise<AxiosResponse<V1StartAdtLinkResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.createAdtLinkagesStart(data)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function createAdtLinkagesRecover(
    data: PlaytesthubServiceRecoverAdtLinkageBody
  ): Promise<AxiosResponse<V1RecoverAdtLinkageResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.createAdtLinkagesRecover(data)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function createAdtLinkagesComplete(data: PlaytesthubServiceCompleteAdtLinkBody): Promise<AxiosResponse<V1CompleteAdtLinkResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.createAdtLinkagesComplete(data)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function getPlaytest_ByPlaytestId(playtestId: string): Promise<AxiosResponse<V1AdminGetPlaytestResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.getPlaytest_ByPlaytestId(playtestId)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function deletePlaytest_ByPlaytestId(playtestId: string): Promise<AxiosResponse<V1SoftDeletePlaytestResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.deletePlaytest_ByPlaytestId(playtestId)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function patchPlaytest_ByPlaytestId(
    playtestId: string,
    data: PlaytesthubServiceEditPlaytestBody
  ): Promise<AxiosResponse<V1EditPlaytestResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.patchPlaytest_ByPlaytestId(playtestId, data)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function deleteAdtLinkage_ByAdtLinkageId(adtLinkageId: string): Promise<AxiosResponse<V1UnlinkAdtResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.deleteAdtLinkage_ByAdtLinkageId(adtLinkageId)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function getDiagnosticsAdtClientKind(): Promise<AxiosResponse<V1GetAdtClientDiagnosticsResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.getDiagnosticsAdtClientKind()
    if (resp.error) throw resp.error
    return resp.response
  }

  async function getCodes_ByPlaytestId(playtestId: string): Promise<AxiosResponse<V1GetCodePoolResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.getCodes_ByPlaytestId(playtestId)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function createSurvey_ByPlaytestId(
    playtestId: string,
    data: PlaytesthubServiceCreateSurveyBody
  ): Promise<AxiosResponse<V1CreateSurveyResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.createSurvey_ByPlaytestId(playtestId, data)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function patchSurvey_ByPlaytestId(
    playtestId: string,
    data: PlaytesthubServiceEditSurveyBody
  ): Promise<AxiosResponse<V1EditSurveyResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.patchSurvey_ByPlaytestId(playtestId, data)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function createApplicant_ByApplicantIdReject(
    applicantId: string,
    data: PlaytesthubServiceRejectApplicantBody
  ): Promise<AxiosResponse<V1RejectApplicantResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.createApplicant_ByApplicantIdReject(applicantId, data)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function getAuditLog_ByPlaytestId(
    playtestId: string,
    queryParams?: { actorFilter?: string | null; actionFilter?: string | null; pageToken?: string | null; pageSize?: number }
  ): Promise<AxiosResponse<V1ListAuditLogResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.getAuditLog_ByPlaytestId(playtestId, queryParams)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function createApplicant_ByApplicantIdApprove(
    applicantId: string,
    data: PlaytesthubServiceApproveApplicantBody
  ): Promise<AxiosResponse<V1ApproveApplicantResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.createApplicant_ByApplicantIdApprove(applicantId, data)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function createApplicant_ByApplicantIdRetryDm(
    applicantId: string,
    data: PlaytesthubServiceRetryDmBody
  ): Promise<AxiosResponse<V1RetryDmResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.createApplicant_ByApplicantIdRetryDm(applicantId, data)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function getGamesAdt_ByAdtLinkageId(adtLinkageId: string): Promise<AxiosResponse<V1ListAdtGamesResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.getGamesAdt_ByAdtLinkageId(adtLinkageId)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function getApplicants_ByPlaytestId(
    playtestId: string,
    queryParams?: {
      statusFilter?: 'APPLICANT_STATUS_UNSPECIFIED' | 'APPLICANT_STATUS_PENDING' | 'APPLICANT_STATUS_APPROVED' | 'APPLICANT_STATUS_REJECTED'
      dmFailedFilter?: boolean | null
      pageToken?: string | null
      pageSize?: number
    }
  ): Promise<AxiosResponse<V1ListApplicantsResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.getApplicants_ByPlaytestId(playtestId, queryParams)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function getBuildsAdt_ByAdtLinkageId(
    adtLinkageId: string,
    queryParams?: { adtGameId?: string | null }
  ): Promise<AxiosResponse<V1ListAdtBuildsResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.getBuildsAdt_ByAdtLinkageId(adtLinkageId, queryParams)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function createCodesTopUp_ByPlaytestId(
    playtestId: string,
    data: PlaytesthubServiceTopUpCodesBody
  ): Promise<AxiosResponse<V1TopUpCodesResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.createCodesTopUp_ByPlaytestId(playtestId, data)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function createCodesUpload_ByPlaytestId(
    playtestId: string,
    data: PlaytesthubServiceUploadCodesBody
  ): Promise<AxiosResponse<V1UploadCodesResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.createCodesUpload_ByPlaytestId(playtestId, data)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function getParticipants_ByPlaytestId(
    playtestId: string,
    queryParams?: {
      statusFilter?: 'APPLICANT_STATUS_UNSPECIFIED' | 'APPLICANT_STATUS_PENDING' | 'APPLICANT_STATUS_APPROVED' | 'APPLICANT_STATUS_REJECTED'
    }
  ): Promise<AxiosResponse<V1GetPlaytestParticipantsResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.getParticipants_ByPlaytestId(playtestId, queryParams)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function getAnnouncements_ByPlaytestId(playtestId: string): Promise<AxiosResponse<V1ListAnnouncementsResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.getAnnouncements_ByPlaytestId(playtestId)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function createAnnouncement_ByPlaytestId(
    playtestId: string,
    data: PlaytesthubServiceCreateAnnouncementBody
  ): Promise<AxiosResponse<V1CreateAnnouncementResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.createAnnouncement_ByPlaytestId(playtestId, data)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function createPlaytest_ByPlaytestIdTransitionStatu(
    playtestId: string,
    data: PlaytesthubServiceTransitionPlaytestStatusBody
  ): Promise<AxiosResponse<V1TransitionPlaytestStatusResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.createPlaytest_ByPlaytestIdTransitionStatu(playtestId, data)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function getSurveyResponses_ByPlaytestId(
    playtestId: string,
    queryParams?: { surveyIdFilter?: string | null; pageToken?: string | null; pageSize?: number }
  ): Promise<AxiosResponse<V1ListSurveyResponsesResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.getSurveyResponses_ByPlaytestId(playtestId, queryParams)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function createCodesSyncFromAg_ByPlaytestId(
    playtestId: string,
    data: PlaytesthubServiceSyncFromAgsBody
  ): Promise<AxiosResponse<V1SyncFromAgsResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.createCodesSyncFromAg_ByPlaytestId(playtestId, data)
    if (resp.error) throw resp.error
    return resp.response
  }

  async function createApplicantsRetryFailedDm_ByPlaytestId(
    playtestId: string,
    data: PlaytesthubServiceRetryFailedDmsBody
  ): Promise<AxiosResponse<V1RetryFailedDmsResponse>> {
    const $ = new PlaytesthubServiceAdmin$(axiosInstance, namespace, useSchemaValidation)
    const resp = await $.createApplicantsRetryFailedDm_ByPlaytestId(playtestId, data)
    if (resp.error) throw resp.error
    return resp.response
  }

  return {
    getPlaytests,
    /**
     * STEAM_KEYS only in M1; distribution_model=AGS_CAMPAIGN returns Unimplemented until M2.
     */
    createPlaytest,
    /**
     * Scoped to the caller's studio namespace (union_namespace ?? namespace). Returns identity columns only — no credential bytes exist (PRD §4.8.2).
     */
    getAdtLinkages,
    /**
     * Returns one entry per registered background worker (reclaim_worker, window_worker). stale := now > expires_at + 2*tick_interval. Missing rows surface as lease_holder='' with stale=true so a never-ticked worker is unmissable. Reads leader_lease directly — no new table.
     */
    getWorkersHealth,
    /**
     * Mints a 32-byte CSRF state, persists adt_link_pending, returns linkUrl that the admin UI redirects to. studio_namespace is derived server-side from the caller's token. No credential is exchanged (PRD §4.8.2).
     */
    createAdtLinkagesStart,
    /**
     * Operator-recovery surface for the 2026-05-21 orphan-flag bug: when ADT still carries a linkage flag but no local adt_linkage row exists, StartADTLink + the redirect dance fail with 409 / already_linked. RecoverADTLinkage probes ADT (ListGames) to confirm the orphan flag, then inserts the local row directly. No OAuth round-trip. AlreadyExists when a live row for (studio, adtNamespace) is already present; FailedPrecondition when ADT reports no flag for the pair; Unavailable on ADT transient errors.
     */
    createAdtLinkagesRecover,
    /**
     * Consumes the adt_link_pending row matching `state` (not expired); inserts the adt_linkage identity row with `adt_namespace` echoed by ADT on the callback URL. No outbound ADT call — tampering is self-defeating because the first downstream service-JWT call would 401 (PRD §4.8.2).
     */
    createAdtLinkagesComplete,

    getPlaytest_ByPlaytestId,

    deletePlaytest_ByPlaytestId,
    /**
     * Editable: title, description, bannerImageUrl, platforms, startsAt, endsAt, ndaRequired, ndaText. Immutable fields → InvalidArgument.
     */
    patchPlaytest_ByPlaytestId,
    /**
     * Idempotent re-unlink against an already soft-deleted row is a no-op success. Linkage absent for the caller's studio → NotFound (PRD §4.8). Best-effort calls ADT's DELETE /linkage in the same flow so the ADT-side flag and the local row drop together.
     */
    deleteAdtLinkage_ByAdtLinkageId,
    /**
     * Diagnostic surface for the 2026-05-21 silent-fallback bug: the bootapp gate that selects HTTP-backed vs in-memory ADT client requires ALL of AuthEnabled + ADT_BASE_URL + AGS_BASE_URL + AGS_IAM_CLIENT_ID + AGS_IAM_CLIENT_SECRET. When any one is empty the gate silently falls to the in-memory MemClient and UnlinkADT's ADT-side propagation becomes a no-op. This RPC returns the gate decision ("http" | "mem") plus a boolean presence flag for each env var so the operator can pinpoint the missing one without needing the boot log. Secret values are NEVER returned — only booleans.
     */
    getDiagnosticsAdtClientKind,
    /**
     * Returns aggregate counts plus the full code list including raw values — admin surfaces are exempt from the §6 log-redaction rule (PRD §5.7).
     */
    getCodes_ByPlaytestId,
    /**
     * Natural-key on playtest_id. Server mints question UUIDs and multi-choice option UUIDs. Bounds: ≤50 questions, prompt ≤1,000 chars, multi-choice 2–20 options with label ≤200 chars (schema.md §"Survey entity spec").
     */
    createSurvey_ByPlaytestId,
    /**
     * Always creates a new Survey row with version = previous + 1. Question UUIDs are preserved for kept questions (client passes the existing id) and minted for new ones (id empty). Multi-choice option ids likewise — keeps histogram aggregation keys stable across edits per schema.md.
     */
    patchSurvey_ByPlaytestId,
    /**
     * Re-reject returns the existing row (natural-key idempotency). rejection_reason is admin-visible (max 500 chars per schema.md).
     */
    createApplicant_ByApplicantIdReject,
    /**
     * actor_filter='system' maps to actorUserId IS NULL per PRD §4.7. action_filter is exact-match on the action string. before_json / after_json carry the JSONB columns verbatim — the client renders the diff.
     */
    getAuditLog_ByPlaytestId,
    /**
     * Re-approve on an already-APPROVED applicant returns the existing row (natural-key idempotency). Errors per docs/errors.md ApproveApplicant rows.
     */
    createApplicant_ByApplicantIdApprove,
    /**
     * No cooldown — double-click sends two DMs (PRD §5.4). Returns the updated Applicant row with refreshed DM fields.
     */
    createApplicant_ByApplicantIdRetryDm,
    /**
     * Proxies adt.Client.ListGames keyed on the studio derived from the caller's token. Drives the create-playtest build-picker's top-level dropdown (STATUS_M5.md B12 + Addendum 2026-05-21). Returns FailedPrecondition when ADT reports the linkage flag missing.
     */
    getGamesAdt_ByAdtLinkageId,
    /**
     * Order: createdAt DESC. Filters: status_filter (UNSPECIFIED → no filter), dm_failed_filter (true → only lastDmStatus='failed'). page_token is opaque; absent → start of stream. page_size 0 → server default (50).
     */
    getApplicants_ByPlaytestId,
    /**
     * Proxies adt.Client.ListBuilds keyed on the studio derived from the caller's token. Returns FailedPrecondition when ADT reports the linkage flag missing.
     */
    getBuildsAdt_ByAdtLinkageId,
    /**
     * Each call generates a fresh batch via the AGS Campaign API. Per docs/ags-failure-modes.md the call is not transactional; partial fulfillment commits the codes received. STEAM_KEYS playtests reject with FailedPrecondition.
     */
    createCodesTopUp_ByPlaytestId,
    /**
     * PRD §4.3: UTF-8, charset [A-Za-z0-9._-], 1–128 chars/code, file ≤10 MB, ≤50,000 codes, file-level + cross-row dedup. On any violation the response carries per-line rejection details and 0 codes are inserted.
     */
    createCodesUpload_ByPlaytestId,
    /**
     * Read joins applicant + the latest dm.sent audit row to derive code_sent_at for STEAM_KEYS / AGS_CAMPAIGN rows; ADT rows return NULL code_sent_at. Four ADT telemetry cache fields ship in the response shape but stay NULL/zero across M5.C.
     */
    getParticipants_ByPlaytestId,
    /**
     * Per-row status aggregated from announcement_recipient.dm_status: SENT (all sent), SENDING (any queued), PARTIAL (mix sent + failed), FAILED (all failed).
     */
    getAnnouncements_ByPlaytestId,
    /**
     * Resolves recipients at call time (NOT a stored snapshot). Subject + message are PII-sensitive and are never written to audit JSONB or structured logs.
     */
    createAnnouncement_ByPlaytestId,

    createPlaytest_ByPlaytestIdTransitionStatu,
    /**
     * Default page_size 50, max 200. Optional survey_id_filter narrows to a single Survey version for per-version aggregate split.
     */
    getSurveyResponses_ByPlaytestId,
    /**
     * Fetch-only recovery for the case where AGS holds codes our DB never persisted. STEAM_KEYS playtests reject with FailedPrecondition.
     */
    createCodesSyncFromAg_ByPlaytestId,
    /**
     * Walks every applicant with last_dm_status=FAILED for the playtest and enqueues each through the same DM-queue path as approve, respecting the 10k cap and configured drain rate. Overflowed rows stay FAILED with last_dm_error='dm_queue_overflow' (PRD §5.5).
     */
    createApplicantsRetryFailedDm_ByPlaytestId
  }
}
