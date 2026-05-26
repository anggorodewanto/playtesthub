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
import { PlaytesthubServiceApproveApplicantBody } from '../../generated-definitions/PlaytesthubServiceApproveApplicantBody.js'
import { PlaytesthubServiceChangeAdtBuildBody } from '../../generated-definitions/PlaytesthubServiceChangeAdtBuildBody.js'
import { PlaytesthubServiceCheckAdtBuildBody } from '../../generated-definitions/PlaytesthubServiceCheckAdtBuildBody.js'
import { PlaytesthubServiceCompleteAdtLinkBody } from '../../generated-definitions/PlaytesthubServiceCompleteAdtLinkBody.js'
import { PlaytesthubServiceCreateAnnouncementBody } from '../../generated-definitions/PlaytesthubServiceCreateAnnouncementBody.js'
import { PlaytesthubServiceCreatePlaytestBody } from '../../generated-definitions/PlaytesthubServiceCreatePlaytestBody.js'
import { PlaytesthubServiceCreateSurveyBody } from '../../generated-definitions/PlaytesthubServiceCreateSurveyBody.js'
import { PlaytesthubServiceEditPlaytestBody } from '../../generated-definitions/PlaytesthubServiceEditPlaytestBody.js'
import { PlaytesthubServiceEditSurveyBody } from '../../generated-definitions/PlaytesthubServiceEditSurveyBody.js'
import { PlaytesthubServiceRecoverAdtLinkageBody } from '../../generated-definitions/PlaytesthubServiceRecoverAdtLinkageBody.js'
import { PlaytesthubServiceRejectApplicantBody } from '../../generated-definitions/PlaytesthubServiceRejectApplicantBody.js'
import { PlaytesthubServiceRetryDmBody } from '../../generated-definitions/PlaytesthubServiceRetryDmBody.js'
import { PlaytesthubServiceRetryFailedDmsBody } from '../../generated-definitions/PlaytesthubServiceRetryFailedDmsBody.js'
import { PlaytesthubServiceStartAdtLinkBody } from '../../generated-definitions/PlaytesthubServiceStartAdtLinkBody.js'
import { PlaytesthubServiceSyncFromAgsBody } from '../../generated-definitions/PlaytesthubServiceSyncFromAgsBody.js'
import { PlaytesthubServiceTopUpCodesBody } from '../../generated-definitions/PlaytesthubServiceTopUpCodesBody.js'
import { PlaytesthubServiceTransitionPlaytestStatusBody } from '../../generated-definitions/PlaytesthubServiceTransitionPlaytestStatusBody.js'
import { PlaytesthubServiceUploadCodesBody } from '../../generated-definitions/PlaytesthubServiceUploadCodesBody.js'
import { V1AdminGetPlaytestResponse } from '../../generated-definitions/V1AdminGetPlaytestResponse.js'
import { V1ApproveApplicantResponse } from '../../generated-definitions/V1ApproveApplicantResponse.js'
import { V1ChangeAdtBuildResponse } from '../../generated-definitions/V1ChangeAdtBuildResponse.js'
import { V1CheckAdtBuildResponse } from '../../generated-definitions/V1CheckAdtBuildResponse.js'
import { V1CompleteAdtLinkResponse } from '../../generated-definitions/V1CompleteAdtLinkResponse.js'
import { V1CreateAnnouncementResponse } from '../../generated-definitions/V1CreateAnnouncementResponse.js'
import { V1CreatePlaytestResponse } from '../../generated-definitions/V1CreatePlaytestResponse.js'
import { V1CreateSurveyResponse } from '../../generated-definitions/V1CreateSurveyResponse.js'
import { V1EditPlaytestResponse } from '../../generated-definitions/V1EditPlaytestResponse.js'
import { V1EditSurveyResponse } from '../../generated-definitions/V1EditSurveyResponse.js'
import { V1GetAdtClientDiagnosticsResponse } from '../../generated-definitions/V1GetAdtClientDiagnosticsResponse.js'
import { V1GetCodePoolResponse } from '../../generated-definitions/V1GetCodePoolResponse.js'
import { V1GetPlaytestParticipantsResponse } from '../../generated-definitions/V1GetPlaytestParticipantsResponse.js'
import { V1GetWorkerHealthResponse } from '../../generated-definitions/V1GetWorkerHealthResponse.js'
import { V1ListAdtBuildsResponse } from '../../generated-definitions/V1ListAdtBuildsResponse.js'
import { V1ListAdtGamesResponse } from '../../generated-definitions/V1ListAdtGamesResponse.js'
import { V1ListAdtLinkagesResponse } from '../../generated-definitions/V1ListAdtLinkagesResponse.js'
import { V1ListAnnouncementsResponse } from '../../generated-definitions/V1ListAnnouncementsResponse.js'
import { V1ListApplicantsResponse } from '../../generated-definitions/V1ListApplicantsResponse.js'
import { V1ListAuditLogResponse } from '../../generated-definitions/V1ListAuditLogResponse.js'
import { V1ListPlaytestsResponse } from '../../generated-definitions/V1ListPlaytestsResponse.js'
import { V1ListSurveyResponsesResponse } from '../../generated-definitions/V1ListSurveyResponsesResponse.js'
import { V1RecoverAdtLinkageResponse } from '../../generated-definitions/V1RecoverAdtLinkageResponse.js'
import { V1RejectApplicantResponse } from '../../generated-definitions/V1RejectApplicantResponse.js'
import { V1RetryDmResponse } from '../../generated-definitions/V1RetryDmResponse.js'
import { V1RetryFailedDmsResponse } from '../../generated-definitions/V1RetryFailedDmsResponse.js'
import { V1SoftDeletePlaytestResponse } from '../../generated-definitions/V1SoftDeletePlaytestResponse.js'
import { V1StartAdtLinkResponse } from '../../generated-definitions/V1StartAdtLinkResponse.js'
import { V1SyncFromAgsResponse } from '../../generated-definitions/V1SyncFromAgsResponse.js'
import { V1TopUpCodesResponse } from '../../generated-definitions/V1TopUpCodesResponse.js'
import { V1TransitionPlaytestStatusResponse } from '../../generated-definitions/V1TransitionPlaytestStatusResponse.js'
import { V1UnlinkAdtResponse } from '../../generated-definitions/V1UnlinkAdtResponse.js'
import { V1UploadCodesResponse } from '../../generated-definitions/V1UploadCodesResponse.js'

export class PlaytesthubServiceAdmin$ {
  private axiosInstance: AxiosInstance
  private namespace: string
  private useSchemaValidation: boolean

  constructor(axiosInstance: AxiosInstance, namespace: string, useSchemaValidation = true) {
    this.axiosInstance = axiosInstance
    this.namespace = namespace
    this.useSchemaValidation = useSchemaValidation
  }

  getPlaytests(): Promise<Response<V1ListPlaytestsResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/playtests'.replace('{namespace}', this.namespace)
    const resultPromise = this.axiosInstance.get(url, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1ListPlaytestsResponse,
      'V1ListPlaytestsResponse'
    )
  }
  /**
   * Create a playtest with distribution_model STEAM_KEYS, AGS_CAMPAIGN, or ADT.
   */
  createPlaytest(data: PlaytesthubServiceCreatePlaytestBody): Promise<Response<V1CreatePlaytestResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/playtests'.replace('{namespace}', this.namespace)
    const resultPromise = this.axiosInstance.post(url, data, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1CreatePlaytestResponse,
      'V1CreatePlaytestResponse'
    )
  }
  /**
   * Scoped to the caller's studio namespace (union_namespace ?? namespace). Returns identity columns only — no credential bytes exist (PRD §4.8.2).
   */
  getAdtLinkages(): Promise<Response<V1ListAdtLinkagesResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/adt/linkages'.replace('{namespace}', this.namespace)
    const resultPromise = this.axiosInstance.get(url, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1ListAdtLinkagesResponse,
      'V1ListAdtLinkagesResponse'
    )
  }
  /**
   * Returns one entry per registered background worker (reclaim_worker, window_worker). stale := now > expires_at + 2*tick_interval. Missing rows surface as lease_holder='' with stale=true so a never-ticked worker is unmissable. Reads leader_lease directly — no new table.
   */
  getWorkersHealth(): Promise<Response<V1GetWorkerHealthResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/workers/health'.replace('{namespace}', this.namespace)
    const resultPromise = this.axiosInstance.get(url, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1GetWorkerHealthResponse,
      'V1GetWorkerHealthResponse'
    )
  }
  /**
   * Mints a 32-byte CSRF state, persists adt_link_pending, returns linkUrl that the admin UI redirects to. studio_namespace is derived server-side from the caller's token. No credential is exchanged (PRD §4.8.2).
   */
  createAdtLinkagesStart(data: PlaytesthubServiceStartAdtLinkBody): Promise<Response<V1StartAdtLinkResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/adt/linkages:start'.replace('{namespace}', this.namespace)
    const resultPromise = this.axiosInstance.post(url, data, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1StartAdtLinkResponse,
      'V1StartAdtLinkResponse'
    )
  }
  /**
   * Operator-recovery surface for the 2026-05-21 orphan-flag bug: when ADT still carries a linkage flag but no local adt_linkage row exists, StartADTLink + the redirect dance fail with 409 / already_linked. RecoverADTLinkage probes ADT (ListGames) to confirm the orphan flag, then inserts the local row directly. No OAuth round-trip. AlreadyExists when a live row for (studio, adtNamespace) is already present; FailedPrecondition when ADT reports no flag for the pair; Unavailable on ADT transient errors.
   */
  createAdtLinkagesRecover(data: PlaytesthubServiceRecoverAdtLinkageBody): Promise<Response<V1RecoverAdtLinkageResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/adt/linkages:recover'.replace('{namespace}', this.namespace)
    const resultPromise = this.axiosInstance.post(url, data, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1RecoverAdtLinkageResponse,
      'V1RecoverAdtLinkageResponse'
    )
  }
  /**
   * Consumes the adt_link_pending row matching `state` (not expired); inserts the adt_linkage identity row with `adt_namespace` echoed by ADT on the callback URL. No outbound ADT call — tampering is self-defeating because the first downstream service-JWT call would 401 (PRD §4.8.2).
   */
  createAdtLinkagesComplete(data: PlaytesthubServiceCompleteAdtLinkBody): Promise<Response<V1CompleteAdtLinkResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/adt/linkages:complete'.replace('{namespace}', this.namespace)
    const resultPromise = this.axiosInstance.post(url, data, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1CompleteAdtLinkResponse,
      'V1CompleteAdtLinkResponse'
    )
  }

  getPlaytest_ByPlaytestId(playtestId: string): Promise<Response<V1AdminGetPlaytestResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/playtests/{playtestId}'
      .replace('{namespace}', this.namespace)
      .replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.get(url, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1AdminGetPlaytestResponse,
      'V1AdminGetPlaytestResponse'
    )
  }

  deletePlaytest_ByPlaytestId(playtestId: string): Promise<Response<V1SoftDeletePlaytestResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/playtests/{playtestId}'
      .replace('{namespace}', this.namespace)
      .replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.delete(url, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1SoftDeletePlaytestResponse,
      'V1SoftDeletePlaytestResponse'
    )
  }
  /**
   * Editable: title, description, bannerImageUrl, platforms, startsAt, endsAt, ndaRequired, ndaText. Immutable fields → InvalidArgument.
   */
  patchPlaytest_ByPlaytestId(playtestId: string, data: PlaytesthubServiceEditPlaytestBody): Promise<Response<V1EditPlaytestResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/playtests/{playtestId}'
      .replace('{namespace}', this.namespace)
      .replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.patch(url, data, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1EditPlaytestResponse,
      'V1EditPlaytestResponse'
    )
  }
  /**
   * Idempotent re-unlink against an already soft-deleted row is a no-op success. Linkage absent for the caller's studio → NotFound (PRD §4.8). Best-effort calls ADT's DELETE /linkage in the same flow so the ADT-side flag and the local row drop together.
   */
  deleteAdtLinkage_ByAdtLinkageId(adtLinkageId: string): Promise<Response<V1UnlinkAdtResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/adt/linkages/{adtLinkageId}'
      .replace('{namespace}', this.namespace)
      .replace('{adtLinkageId}', adtLinkageId)
    const resultPromise = this.axiosInstance.delete(url, { params })

    return Validate.validateOrReturnResponse(this.useSchemaValidation, () => resultPromise, V1UnlinkAdtResponse, 'V1UnlinkAdtResponse')
  }
  /**
   * Diagnostic surface for the 2026-05-21 silent-fallback bug: the bootapp gate that selects HTTP-backed vs in-memory ADT client requires ALL of AuthEnabled + ADT_BASE_URL + AGS_BASE_URL + AGS_IAM_CLIENT_ID + AGS_IAM_CLIENT_SECRET. When any one is empty the gate silently falls to the in-memory MemClient and UnlinkADT's ADT-side propagation becomes a no-op. This RPC returns the gate decision ("http" | "mem") plus a boolean presence flag for each env var so the operator can pinpoint the missing one without needing the boot log. Secret values are NEVER returned — only booleans.
   */
  getDiagnosticsAdtClientKind(): Promise<Response<V1GetAdtClientDiagnosticsResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/diagnostics/adt-client-kind'.replace('{namespace}', this.namespace)
    const resultPromise = this.axiosInstance.get(url, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1GetAdtClientDiagnosticsResponse,
      'V1GetAdtClientDiagnosticsResponse'
    )
  }
  /**
   * Returns aggregate counts plus the full code list including raw values — admin surfaces are exempt from the §6 log-redaction rule (PRD §5.7).
   */
  getCodes_ByPlaytestId(playtestId: string): Promise<Response<V1GetCodePoolResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/playtests/{playtestId}/codes'
      .replace('{namespace}', this.namespace)
      .replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.get(url, { params })

    return Validate.validateOrReturnResponse(this.useSchemaValidation, () => resultPromise, V1GetCodePoolResponse, 'V1GetCodePoolResponse')
  }
  /**
   * Natural-key on playtest_id. Server mints question UUIDs and multi-choice option UUIDs. Bounds: ≤50 questions, prompt ≤1,000 chars, multi-choice 2–20 options with label ≤200 chars (schema.md §"Survey entity spec").
   */
  createSurvey_ByPlaytestId(playtestId: string, data: PlaytesthubServiceCreateSurveyBody): Promise<Response<V1CreateSurveyResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/playtests/{playtestId}/survey'
      .replace('{namespace}', this.namespace)
      .replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.post(url, data, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1CreateSurveyResponse,
      'V1CreateSurveyResponse'
    )
  }
  /**
   * Always creates a new Survey row with version = previous + 1. Question UUIDs are preserved for kept questions (client passes the existing id) and minted for new ones (id empty). Multi-choice option ids likewise — keeps histogram aggregation keys stable across edits per schema.md.
   */
  patchSurvey_ByPlaytestId(playtestId: string, data: PlaytesthubServiceEditSurveyBody): Promise<Response<V1EditSurveyResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/playtests/{playtestId}/survey'
      .replace('{namespace}', this.namespace)
      .replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.patch(url, data, { params })

    return Validate.validateOrReturnResponse(this.useSchemaValidation, () => resultPromise, V1EditSurveyResponse, 'V1EditSurveyResponse')
  }
  /**
   * Re-reject returns the existing row (natural-key idempotency). rejection_reason is admin-visible (max 500 chars per schema.md).
   */
  createApplicant_ByApplicantIdReject(
    applicantId: string,
    data: PlaytesthubServiceRejectApplicantBody
  ): Promise<Response<V1RejectApplicantResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/applicants/{applicantId}:reject'
      .replace('{namespace}', this.namespace)
      .replace('{applicantId}', applicantId)
    const resultPromise = this.axiosInstance.post(url, data, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1RejectApplicantResponse,
      'V1RejectApplicantResponse'
    )
  }
  /**
   * actor_filter='system' maps to actorUserId IS NULL per PRD §4.7. action_filter is exact-match on the action string. before_json / after_json carry the JSONB columns verbatim — the client renders the diff.
   */
  getAuditLog_ByPlaytestId(
    playtestId: string,
    queryParams?: { actorFilter?: string | null; actionFilter?: string | null; pageToken?: string | null; pageSize?: number }
  ): Promise<Response<V1ListAuditLogResponse>> {
    const params = { ...queryParams } as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/playtests/{playtestId}/auditLog'
      .replace('{namespace}', this.namespace)
      .replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.get(url, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1ListAuditLogResponse,
      'V1ListAuditLogResponse'
    )
  }
  /**
   * Re-approve on an already-APPROVED applicant returns the existing row (natural-key idempotency). Errors per docs/errors.md ApproveApplicant rows.
   */
  createApplicant_ByApplicantIdApprove(
    applicantId: string,
    data: PlaytesthubServiceApproveApplicantBody
  ): Promise<Response<V1ApproveApplicantResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/applicants/{applicantId}:approve'
      .replace('{namespace}', this.namespace)
      .replace('{applicantId}', applicantId)
    const resultPromise = this.axiosInstance.post(url, data, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1ApproveApplicantResponse,
      'V1ApproveApplicantResponse'
    )
  }
  /**
   * No cooldown — double-click sends two DMs (PRD §5.4). Returns the updated Applicant row with refreshed DM fields.
   */
  createApplicant_ByApplicantIdRetryDm(applicantId: string, data: PlaytesthubServiceRetryDmBody): Promise<Response<V1RetryDmResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/applicants/{applicantId}:retryDm'
      .replace('{namespace}', this.namespace)
      .replace('{applicantId}', applicantId)
    const resultPromise = this.axiosInstance.post(url, data, { params })

    return Validate.validateOrReturnResponse(this.useSchemaValidation, () => resultPromise, V1RetryDmResponse, 'V1RetryDmResponse')
  }
  /**
   * Proxies adt.Client.ListGames keyed on the studio derived from the caller's token. Drives the create-playtest build-picker's top-level dropdown (STATUS_M5.md B12 + Addendum 2026-05-21). Returns FailedPrecondition when ADT reports the linkage flag missing.
   */
  getGamesAdt_ByAdtLinkageId(adtLinkageId: string): Promise<Response<V1ListAdtGamesResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/adt/linkages/{adtLinkageId}/games'
      .replace('{namespace}', this.namespace)
      .replace('{adtLinkageId}', adtLinkageId)
    const resultPromise = this.axiosInstance.get(url, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1ListAdtGamesResponse,
      'V1ListAdtGamesResponse'
    )
  }
  /**
   * Order: createdAt DESC. Filters: status_filter (UNSPECIFIED → no filter), dm_failed_filter (true → only lastDmStatus='failed'). page_token is opaque; absent → start of stream. page_size 0 → server default (50).
   */
  getApplicants_ByPlaytestId(
    playtestId: string,
    queryParams?: {
      statusFilter?: 'APPLICANT_STATUS_UNSPECIFIED' | 'APPLICANT_STATUS_PENDING' | 'APPLICANT_STATUS_APPROVED' | 'APPLICANT_STATUS_REJECTED'
      dmFailedFilter?: boolean | null
      pageToken?: string | null
      pageSize?: number
    }
  ): Promise<Response<V1ListApplicantsResponse>> {
    const params = { statusFilter: 'APPLICANT_STATUS_UNSPECIFIED', ...queryParams } as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/playtests/{playtestId}/applicants'
      .replace('{namespace}', this.namespace)
      .replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.get(url, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1ListApplicantsResponse,
      'V1ListApplicantsResponse'
    )
  }
  /**
   * Proxies adt.Client.ListBuilds keyed on the studio derived from the caller's token. Returns FailedPrecondition when ADT reports the linkage flag missing.
   */
  getBuildsAdt_ByAdtLinkageId(
    adtLinkageId: string,
    queryParams?: { adtGameId?: string | null }
  ): Promise<Response<V1ListAdtBuildsResponse>> {
    const params = { ...queryParams } as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/adt/linkages/{adtLinkageId}/builds'
      .replace('{namespace}', this.namespace)
      .replace('{adtLinkageId}', adtLinkageId)
    const resultPromise = this.axiosInstance.get(url, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1ListAdtBuildsResponse,
      'V1ListAdtBuildsResponse'
    )
  }
  /**
   * Each call generates a fresh batch via the AGS Campaign API. Per docs/ags-failure-modes.md the call is not transactional; partial fulfillment commits the codes received. STEAM_KEYS playtests reject with FailedPrecondition.
   */
  createCodesTopUp_ByPlaytestId(playtestId: string, data: PlaytesthubServiceTopUpCodesBody): Promise<Response<V1TopUpCodesResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/playtests/{playtestId}/codes:topUp'
      .replace('{namespace}', this.namespace)
      .replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.post(url, data, { params })

    return Validate.validateOrReturnResponse(this.useSchemaValidation, () => resultPromise, V1TopUpCodesResponse, 'V1TopUpCodesResponse')
  }
  /**
   * PRD §4.3: UTF-8, charset [A-Za-z0-9._-], 1–128 chars/code, file ≤10 MB, ≤50,000 codes, file-level + cross-row dedup. On any violation the response carries per-line rejection details and 0 codes are inserted.
   */
  createCodesUpload_ByPlaytestId(playtestId: string, data: PlaytesthubServiceUploadCodesBody): Promise<Response<V1UploadCodesResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/playtests/{playtestId}/codes:upload'
      .replace('{namespace}', this.namespace)
      .replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.post(url, data, { params })

    return Validate.validateOrReturnResponse(this.useSchemaValidation, () => resultPromise, V1UploadCodesResponse, 'V1UploadCodesResponse')
  }
  /**
   * Read joins applicant + the latest dm.sent audit row to derive code_sent_at for STEAM_KEYS / AGS_CAMPAIGN rows; ADT rows return NULL code_sent_at. Four ADT telemetry cache fields ship in the response shape but stay NULL/zero across M5.C.
   */
  getParticipants_ByPlaytestId(
    playtestId: string,
    queryParams?: {
      statusFilter?: 'APPLICANT_STATUS_UNSPECIFIED' | 'APPLICANT_STATUS_PENDING' | 'APPLICANT_STATUS_APPROVED' | 'APPLICANT_STATUS_REJECTED'
    }
  ): Promise<Response<V1GetPlaytestParticipantsResponse>> {
    const params = { statusFilter: 'APPLICANT_STATUS_UNSPECIFIED', ...queryParams } as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/playtests/{playtestId}/participants'
      .replace('{namespace}', this.namespace)
      .replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.get(url, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1GetPlaytestParticipantsResponse,
      'V1GetPlaytestParticipantsResponse'
    )
  }
  /**
   * Per-row status aggregated from announcement_recipient.dm_status: SENT (all sent), SENDING (any queued), PARTIAL (mix sent + failed), FAILED (all failed).
   */
  getAnnouncements_ByPlaytestId(playtestId: string): Promise<Response<V1ListAnnouncementsResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/playtests/{playtestId}/announcements'
      .replace('{namespace}', this.namespace)
      .replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.get(url, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1ListAnnouncementsResponse,
      'V1ListAnnouncementsResponse'
    )
  }
  /**
   * Resolves recipients at call time (NOT a stored snapshot). Subject + message are PII-sensitive and are never written to audit JSONB or structured logs.
   */
  createAnnouncement_ByPlaytestId(
    playtestId: string,
    data: PlaytesthubServiceCreateAnnouncementBody
  ): Promise<Response<V1CreateAnnouncementResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/playtests/{playtestId}/announcements'
      .replace('{namespace}', this.namespace)
      .replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.post(url, data, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1CreateAnnouncementResponse,
      'V1CreateAnnouncementResponse'
    )
  }
  /**
   * Issues a download URL for the playtest's current adt_build_id (same call as ApproveApplicant) and persists adt_build_status: 'OK' when a URL was minted, 'UNAVAILABLE' when ADT returns build-not-found. Non-ADT playtest → FailedPrecondition. Linkage missing / ADT unreachable → FailedPrecondition / Unavailable (status not overwritten). Side effect: a throwaway download URL is minted on success.
   */
  createAdtBuildCheck_ByPlaytestId(
    playtestId: string,
    data: PlaytesthubServiceCheckAdtBuildBody
  ): Promise<Response<V1CheckAdtBuildResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/playtests/{playtestId}/adt/build:check'
      .replace('{namespace}', this.namespace)
      .replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.post(url, data, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1CheckAdtBuildResponse,
      'V1CheckAdtBuildResponse'
    )
  }

  createPlaytest_ByPlaytestIdTransitionStatu(
    playtestId: string,
    data: PlaytesthubServiceTransitionPlaytestStatusBody
  ): Promise<Response<V1TransitionPlaytestStatusResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/playtests/{playtestId}:transitionStatus'
      .replace('{namespace}', this.namespace)
      .replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.post(url, data, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1TransitionPlaytestStatusResponse,
      'V1TransitionPlaytestStatusResponse'
    )
  }
  /**
   * Mutates adt_game_id + adt_build_id on an ADT playtest after verifying the pair against the linked ADT namespace via ListBuilds. adt_namespace is immutable (relink instead). Non-ADT playtest → FailedPrecondition. Build absent from the (namespace, game) pair → InvalidArgument. Already-approved applicants keep the download URL already DM'd; future approvals + RetryDM re-mint against the new build (PRD §4.8.3).
   */
  createAdtBuildChange_ByPlaytestId(
    playtestId: string,
    data: PlaytesthubServiceChangeAdtBuildBody
  ): Promise<Response<V1ChangeAdtBuildResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/playtests/{playtestId}/adt/build:change'
      .replace('{namespace}', this.namespace)
      .replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.post(url, data, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1ChangeAdtBuildResponse,
      'V1ChangeAdtBuildResponse'
    )
  }
  /**
   * Default page_size 50, max 200. Optional survey_id_filter narrows to a single Survey version for per-version aggregate split.
   */
  getSurveyResponses_ByPlaytestId(
    playtestId: string,
    queryParams?: { surveyIdFilter?: string | null; pageToken?: string | null; pageSize?: number }
  ): Promise<Response<V1ListSurveyResponsesResponse>> {
    const params = { ...queryParams } as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/playtests/{playtestId}/survey/responses'
      .replace('{namespace}', this.namespace)
      .replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.get(url, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1ListSurveyResponsesResponse,
      'V1ListSurveyResponsesResponse'
    )
  }
  /**
   * Fetch-only recovery for the case where AGS holds codes our DB never persisted. STEAM_KEYS playtests reject with FailedPrecondition.
   */
  createCodesSyncFromAg_ByPlaytestId(
    playtestId: string,
    data: PlaytesthubServiceSyncFromAgsBody
  ): Promise<Response<V1SyncFromAgsResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/playtests/{playtestId}/codes:syncFromAgs'
      .replace('{namespace}', this.namespace)
      .replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.post(url, data, { params })

    return Validate.validateOrReturnResponse(this.useSchemaValidation, () => resultPromise, V1SyncFromAgsResponse, 'V1SyncFromAgsResponse')
  }
  /**
   * Walks every applicant with last_dm_status=FAILED for the playtest and enqueues each through the same DM-queue path as approve, respecting the 10k cap and configured drain rate. Overflowed rows stay FAILED with last_dm_error='dm_queue_overflow' (PRD §5.5).
   */
  createApplicantsRetryFailedDm_ByPlaytestId(
    playtestId: string,
    data: PlaytesthubServiceRetryFailedDmsBody
  ): Promise<Response<V1RetryFailedDmsResponse>> {
    const params = {} as AxiosRequestConfig
    const url = '/v1/admin/namespaces/{namespace}/playtests/{playtestId}/applicants:retryFailedDms'
      .replace('{namespace}', this.namespace)
      .replace('{playtestId}', playtestId)
    const resultPromise = this.axiosInstance.post(url, data, { params })

    return Validate.validateOrReturnResponse(
      this.useSchemaValidation,
      () => resultPromise,
      V1RetryFailedDmsResponse,
      'V1RetryFailedDmsResponse'
    )
  }
}
