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
import { PlaytesthubServiceAdminApi } from '../PlaytesthubServiceAdminApi.js'

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

export const Key_PlaytesthubServiceAdmin = {
  Playtests: 'Playtesthubapi.PlaytesthubServiceAdmin.Playtests',
  Playtest: 'Playtesthubapi.PlaytesthubServiceAdmin.Playtest',
  AdtLinkages: 'Playtesthubapi.PlaytesthubServiceAdmin.AdtLinkages',
  WorkersHealth: 'Playtesthubapi.PlaytesthubServiceAdmin.WorkersHealth',
  AdtLinkagesStart: 'Playtesthubapi.PlaytesthubServiceAdmin.AdtLinkagesStart',
  AdtLinkagesRecover: 'Playtesthubapi.PlaytesthubServiceAdmin.AdtLinkagesRecover',
  AdtLinkagesComplete: 'Playtesthubapi.PlaytesthubServiceAdmin.AdtLinkagesComplete',
  Playtest_ByPlaytestId: 'Playtesthubapi.PlaytesthubServiceAdmin.Playtest_ByPlaytestId',
  AdtLinkage_ByAdtLinkageId: 'Playtesthubapi.PlaytesthubServiceAdmin.AdtLinkage_ByAdtLinkageId',
  DiagnosticsAdtClientKind: 'Playtesthubapi.PlaytesthubServiceAdmin.DiagnosticsAdtClientKind',
  Codes_ByPlaytestId: 'Playtesthubapi.PlaytesthubServiceAdmin.Codes_ByPlaytestId',
  Survey_ByPlaytestId: 'Playtesthubapi.PlaytesthubServiceAdmin.Survey_ByPlaytestId',
  Applicant_ByApplicantIdReject: 'Playtesthubapi.PlaytesthubServiceAdmin.Applicant_ByApplicantIdReject',
  AuditLog_ByPlaytestId: 'Playtesthubapi.PlaytesthubServiceAdmin.AuditLog_ByPlaytestId',
  Applicant_ByApplicantIdApprove: 'Playtesthubapi.PlaytesthubServiceAdmin.Applicant_ByApplicantIdApprove',
  Applicant_ByApplicantIdRetryDm: 'Playtesthubapi.PlaytesthubServiceAdmin.Applicant_ByApplicantIdRetryDm',
  GamesAdt_ByAdtLinkageId: 'Playtesthubapi.PlaytesthubServiceAdmin.GamesAdt_ByAdtLinkageId',
  Applicants_ByPlaytestId: 'Playtesthubapi.PlaytesthubServiceAdmin.Applicants_ByPlaytestId',
  BuildsAdt_ByAdtLinkageId: 'Playtesthubapi.PlaytesthubServiceAdmin.BuildsAdt_ByAdtLinkageId',
  CodesTopUp_ByPlaytestId: 'Playtesthubapi.PlaytesthubServiceAdmin.CodesTopUp_ByPlaytestId',
  CodesUpload_ByPlaytestId: 'Playtesthubapi.PlaytesthubServiceAdmin.CodesUpload_ByPlaytestId',
  Participants_ByPlaytestId: 'Playtesthubapi.PlaytesthubServiceAdmin.Participants_ByPlaytestId',
  Announcements_ByPlaytestId: 'Playtesthubapi.PlaytesthubServiceAdmin.Announcements_ByPlaytestId',
  Announcement_ByPlaytestId: 'Playtesthubapi.PlaytesthubServiceAdmin.Announcement_ByPlaytestId',
  AdtBuildCheck_ByPlaytestId: 'Playtesthubapi.PlaytesthubServiceAdmin.AdtBuildCheck_ByPlaytestId',
  Playtest_ByPlaytestIdTransitionStatu: 'Playtesthubapi.PlaytesthubServiceAdmin.Playtest_ByPlaytestIdTransitionStatu',
  AdtBuildChange_ByPlaytestId: 'Playtesthubapi.PlaytesthubServiceAdmin.AdtBuildChange_ByPlaytestId',
  SurveyResponses_ByPlaytestId: 'Playtesthubapi.PlaytesthubServiceAdmin.SurveyResponses_ByPlaytestId',
  CodesSyncFromAg_ByPlaytestId: 'Playtesthubapi.PlaytesthubServiceAdmin.CodesSyncFromAg_ByPlaytestId',
  ApplicantsRetryFailedDm_ByPlaytestId: 'Playtesthubapi.PlaytesthubServiceAdmin.ApplicantsRetryFailedDm_ByPlaytestId'
} as const

export const usePlaytesthubServiceAdminApi_GetPlaytests = (
  sdk: AccelByteSDK,
  input: SdkSetConfigParam,
  options?: Omit<UseQueryOptions<V1ListPlaytestsResponse, AxiosError<ApiError>>, 'queryKey'>,
  callback?: (data: AxiosResponse<V1ListPlaytestsResponse>) => void
): UseQueryResult<V1ListPlaytestsResponse, AxiosError<ApiError>> => {
  const queryFn = (sdk: AccelByteSDK, input: Parameters<typeof usePlaytesthubServiceAdminApi_GetPlaytests>[1]) => async () => {
    const response = await PlaytesthubServiceAdminApi(sdk, { coreConfig: input.coreConfig, axiosConfig: input.axiosConfig }).getPlaytests()
    callback?.(response)
    return response.data
  }

  return useQuery<V1ListPlaytestsResponse, AxiosError<ApiError>>({
    queryKey: [Key_PlaytesthubServiceAdmin.Playtests, input],
    queryFn: queryFn(sdk, input),
    ...options
  })
}

/**
 * Create a playtest with distribution_model STEAM_KEYS, AGS_CAMPAIGN, or ADT.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.Playtest, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_CreatePlaytestMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<V1CreatePlaytestResponse, AxiosError<ApiError>, SdkSetConfigParam & { data: PlaytesthubServiceCreatePlaytestBody }>,
    'mutationKey'
  >,
  callback?: (data: V1CreatePlaytestResponse) => void
): UseMutationResult<
  V1CreatePlaytestResponse,
  AxiosError<ApiError>,
  SdkSetConfigParam & { data: PlaytesthubServiceCreatePlaytestBody }
> => {
  const mutationFn = async (input: SdkSetConfigParam & { data: PlaytesthubServiceCreatePlaytestBody }) => {
    const response = await PlaytesthubServiceAdminApi(sdk, { coreConfig: input.coreConfig, axiosConfig: input.axiosConfig }).createPlaytest(
      input.data
    )
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubServiceAdmin.Playtest],
    mutationFn,
    ...options
  })
}

/**
 * Scoped to the caller's studio namespace (union_namespace ?? namespace). Returns identity columns only — no credential bytes exist (PRD §4.8.2).
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.AdtLinkages, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_GetAdtLinkages = (
  sdk: AccelByteSDK,
  input: SdkSetConfigParam,
  options?: Omit<UseQueryOptions<V1ListAdtLinkagesResponse, AxiosError<ApiError>>, 'queryKey'>,
  callback?: (data: AxiosResponse<V1ListAdtLinkagesResponse>) => void
): UseQueryResult<V1ListAdtLinkagesResponse, AxiosError<ApiError>> => {
  const queryFn = (sdk: AccelByteSDK, input: Parameters<typeof usePlaytesthubServiceAdminApi_GetAdtLinkages>[1]) => async () => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).getAdtLinkages()
    callback?.(response)
    return response.data
  }

  return useQuery<V1ListAdtLinkagesResponse, AxiosError<ApiError>>({
    queryKey: [Key_PlaytesthubServiceAdmin.AdtLinkages, input],
    queryFn: queryFn(sdk, input),
    ...options
  })
}

/**
 * Returns one entry per registered background worker (reclaim_worker, window_worker). stale := now > expires_at + 2*tick_interval. Missing rows surface as lease_holder='' with stale=true so a never-ticked worker is unmissable. Reads leader_lease directly — no new table.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.WorkersHealth, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_GetWorkersHealth = (
  sdk: AccelByteSDK,
  input: SdkSetConfigParam,
  options?: Omit<UseQueryOptions<V1GetWorkerHealthResponse, AxiosError<ApiError>>, 'queryKey'>,
  callback?: (data: AxiosResponse<V1GetWorkerHealthResponse>) => void
): UseQueryResult<V1GetWorkerHealthResponse, AxiosError<ApiError>> => {
  const queryFn = (sdk: AccelByteSDK, input: Parameters<typeof usePlaytesthubServiceAdminApi_GetWorkersHealth>[1]) => async () => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).getWorkersHealth()
    callback?.(response)
    return response.data
  }

  return useQuery<V1GetWorkerHealthResponse, AxiosError<ApiError>>({
    queryKey: [Key_PlaytesthubServiceAdmin.WorkersHealth, input],
    queryFn: queryFn(sdk, input),
    ...options
  })
}

/**
 * Mints a 32-byte CSRF state, persists adt_link_pending, returns linkUrl that the admin UI redirects to. studio_namespace is derived server-side from the caller's token. No credential is exchanged (PRD §4.8.2).
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.AdtLinkagesStart, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_CreateAdtLinkagesStartMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<V1StartAdtLinkResponse, AxiosError<ApiError>, SdkSetConfigParam & { data: PlaytesthubServiceStartAdtLinkBody }>,
    'mutationKey'
  >,
  callback?: (data: V1StartAdtLinkResponse) => void
): UseMutationResult<V1StartAdtLinkResponse, AxiosError<ApiError>, SdkSetConfigParam & { data: PlaytesthubServiceStartAdtLinkBody }> => {
  const mutationFn = async (input: SdkSetConfigParam & { data: PlaytesthubServiceStartAdtLinkBody }) => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).createAdtLinkagesStart(input.data)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubServiceAdmin.AdtLinkagesStart],
    mutationFn,
    ...options
  })
}

/**
 * Operator-recovery surface for the 2026-05-21 orphan-flag bug: when ADT still carries a linkage flag but no local adt_linkage row exists, StartADTLink + the redirect dance fail with 409 / already_linked. RecoverADTLinkage probes ADT (ListGames) to confirm the orphan flag, then inserts the local row directly. No OAuth round-trip. AlreadyExists when a live row for (studio, adtNamespace) is already present; FailedPrecondition when ADT reports no flag for the pair; Unavailable on ADT transient errors.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.AdtLinkagesRecover, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_CreateAdtLinkagesRecoverMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<
      V1RecoverAdtLinkageResponse,
      AxiosError<ApiError>,
      SdkSetConfigParam & { data: PlaytesthubServiceRecoverAdtLinkageBody }
    >,
    'mutationKey'
  >,
  callback?: (data: V1RecoverAdtLinkageResponse) => void
): UseMutationResult<
  V1RecoverAdtLinkageResponse,
  AxiosError<ApiError>,
  SdkSetConfigParam & { data: PlaytesthubServiceRecoverAdtLinkageBody }
> => {
  const mutationFn = async (input: SdkSetConfigParam & { data: PlaytesthubServiceRecoverAdtLinkageBody }) => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).createAdtLinkagesRecover(input.data)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubServiceAdmin.AdtLinkagesRecover],
    mutationFn,
    ...options
  })
}

/**
 * Consumes the adt_link_pending row matching `state` (not expired); inserts the adt_linkage identity row with `adt_namespace` echoed by ADT on the callback URL. No outbound ADT call — tampering is self-defeating because the first downstream service-JWT call would 401 (PRD §4.8.2).
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.AdtLinkagesComplete, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_CreateAdtLinkagesCompleteMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<
      V1CompleteAdtLinkResponse,
      AxiosError<ApiError>,
      SdkSetConfigParam & { data: PlaytesthubServiceCompleteAdtLinkBody }
    >,
    'mutationKey'
  >,
  callback?: (data: V1CompleteAdtLinkResponse) => void
): UseMutationResult<
  V1CompleteAdtLinkResponse,
  AxiosError<ApiError>,
  SdkSetConfigParam & { data: PlaytesthubServiceCompleteAdtLinkBody }
> => {
  const mutationFn = async (input: SdkSetConfigParam & { data: PlaytesthubServiceCompleteAdtLinkBody }) => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).createAdtLinkagesComplete(input.data)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubServiceAdmin.AdtLinkagesComplete],
    mutationFn,
    ...options
  })
}

export const usePlaytesthubServiceAdminApi_GetPlaytest_ByPlaytestId = (
  sdk: AccelByteSDK,
  input: SdkSetConfigParam & { playtestId: string },
  options?: Omit<UseQueryOptions<V1AdminGetPlaytestResponse, AxiosError<ApiError>>, 'queryKey'>,
  callback?: (data: AxiosResponse<V1AdminGetPlaytestResponse>) => void
): UseQueryResult<V1AdminGetPlaytestResponse, AxiosError<ApiError>> => {
  const queryFn = (sdk: AccelByteSDK, input: Parameters<typeof usePlaytesthubServiceAdminApi_GetPlaytest_ByPlaytestId>[1]) => async () => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).getPlaytest_ByPlaytestId(input.playtestId)
    callback?.(response)
    return response.data
  }

  return useQuery<V1AdminGetPlaytestResponse, AxiosError<ApiError>>({
    queryKey: [Key_PlaytesthubServiceAdmin.Playtest_ByPlaytestId, input],
    queryFn: queryFn(sdk, input),
    ...options
  })
}

export const usePlaytesthubServiceAdminApi_DeletePlaytest_ByPlaytestIdMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<V1SoftDeletePlaytestResponse, AxiosError<ApiError>, SdkSetConfigParam & { playtestId: string }>,
    'mutationKey'
  >,
  callback?: (data: V1SoftDeletePlaytestResponse) => void
): UseMutationResult<V1SoftDeletePlaytestResponse, AxiosError<ApiError>, SdkSetConfigParam & { playtestId: string }> => {
  const mutationFn = async (input: SdkSetConfigParam & { playtestId: string }) => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).deletePlaytest_ByPlaytestId(input.playtestId)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubServiceAdmin.Playtest_ByPlaytestId],
    mutationFn,
    ...options
  })
}

/**
 * Editable: title, description, bannerImageUrl, platforms, startsAt, endsAt, ndaRequired, ndaText. Immutable fields → InvalidArgument.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.Playtest_ByPlaytestId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_PatchPlaytest_ByPlaytestIdMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<
      V1EditPlaytestResponse,
      AxiosError<ApiError>,
      SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceEditPlaytestBody }
    >,
    'mutationKey'
  >,
  callback?: (data: V1EditPlaytestResponse) => void
): UseMutationResult<
  V1EditPlaytestResponse,
  AxiosError<ApiError>,
  SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceEditPlaytestBody }
> => {
  const mutationFn = async (input: SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceEditPlaytestBody }) => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).patchPlaytest_ByPlaytestId(input.playtestId, input.data)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubServiceAdmin.Playtest_ByPlaytestId],
    mutationFn,
    ...options
  })
}

/**
 * Idempotent re-unlink against an already soft-deleted row is a no-op success. Linkage absent for the caller's studio → NotFound (PRD §4.8). Best-effort calls ADT's DELETE /linkage in the same flow so the ADT-side flag and the local row drop together.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.AdtLinkage_ByAdtLinkageId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_DeleteAdtLinkage_ByAdtLinkageIdMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<V1UnlinkAdtResponse, AxiosError<ApiError>, SdkSetConfigParam & { adtLinkageId: string }>,
    'mutationKey'
  >,
  callback?: (data: V1UnlinkAdtResponse) => void
): UseMutationResult<V1UnlinkAdtResponse, AxiosError<ApiError>, SdkSetConfigParam & { adtLinkageId: string }> => {
  const mutationFn = async (input: SdkSetConfigParam & { adtLinkageId: string }) => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).deleteAdtLinkage_ByAdtLinkageId(input.adtLinkageId)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubServiceAdmin.AdtLinkage_ByAdtLinkageId],
    mutationFn,
    ...options
  })
}

/**
 * Diagnostic surface for the 2026-05-21 silent-fallback bug: the bootapp gate that selects HTTP-backed vs in-memory ADT client requires ALL of AuthEnabled + ADT_BASE_URL + AGS_BASE_URL + AGS_IAM_CLIENT_ID + AGS_IAM_CLIENT_SECRET. When any one is empty the gate silently falls to the in-memory MemClient and UnlinkADT's ADT-side propagation becomes a no-op. This RPC returns the gate decision ("http" | "mem") plus a boolean presence flag for each env var so the operator can pinpoint the missing one without needing the boot log. Secret values are NEVER returned — only booleans.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.DiagnosticsAdtClientKind, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_GetDiagnosticsAdtClientKind = (
  sdk: AccelByteSDK,
  input: SdkSetConfigParam,
  options?: Omit<UseQueryOptions<V1GetAdtClientDiagnosticsResponse, AxiosError<ApiError>>, 'queryKey'>,
  callback?: (data: AxiosResponse<V1GetAdtClientDiagnosticsResponse>) => void
): UseQueryResult<V1GetAdtClientDiagnosticsResponse, AxiosError<ApiError>> => {
  const queryFn =
    (sdk: AccelByteSDK, input: Parameters<typeof usePlaytesthubServiceAdminApi_GetDiagnosticsAdtClientKind>[1]) => async () => {
      const response = await PlaytesthubServiceAdminApi(sdk, {
        coreConfig: input.coreConfig,
        axiosConfig: input.axiosConfig
      }).getDiagnosticsAdtClientKind()
      callback?.(response)
      return response.data
    }

  return useQuery<V1GetAdtClientDiagnosticsResponse, AxiosError<ApiError>>({
    queryKey: [Key_PlaytesthubServiceAdmin.DiagnosticsAdtClientKind, input],
    queryFn: queryFn(sdk, input),
    ...options
  })
}

/**
 * Returns aggregate counts plus the full code list including raw values — admin surfaces are exempt from the §6 log-redaction rule (PRD §5.7).
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.Codes_ByPlaytestId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId = (
  sdk: AccelByteSDK,
  input: SdkSetConfigParam & { playtestId: string },
  options?: Omit<UseQueryOptions<V1GetCodePoolResponse, AxiosError<ApiError>>, 'queryKey'>,
  callback?: (data: AxiosResponse<V1GetCodePoolResponse>) => void
): UseQueryResult<V1GetCodePoolResponse, AxiosError<ApiError>> => {
  const queryFn = (sdk: AccelByteSDK, input: Parameters<typeof usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId>[1]) => async () => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).getCodes_ByPlaytestId(input.playtestId)
    callback?.(response)
    return response.data
  }

  return useQuery<V1GetCodePoolResponse, AxiosError<ApiError>>({
    queryKey: [Key_PlaytesthubServiceAdmin.Codes_ByPlaytestId, input],
    queryFn: queryFn(sdk, input),
    ...options
  })
}

/**
 * Natural-key on playtest_id. Server mints question UUIDs and multi-choice option UUIDs. Bounds: ≤50 questions, prompt ≤1,000 chars, multi-choice 2–20 options with label ≤200 chars (schema.md §"Survey entity spec").
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.Survey_ByPlaytestId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_CreateSurvey_ByPlaytestIdMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<
      V1CreateSurveyResponse,
      AxiosError<ApiError>,
      SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceCreateSurveyBody }
    >,
    'mutationKey'
  >,
  callback?: (data: V1CreateSurveyResponse) => void
): UseMutationResult<
  V1CreateSurveyResponse,
  AxiosError<ApiError>,
  SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceCreateSurveyBody }
> => {
  const mutationFn = async (input: SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceCreateSurveyBody }) => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).createSurvey_ByPlaytestId(input.playtestId, input.data)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubServiceAdmin.Survey_ByPlaytestId],
    mutationFn,
    ...options
  })
}

/**
 * Always creates a new Survey row with version = previous + 1. Question UUIDs are preserved for kept questions (client passes the existing id) and minted for new ones (id empty). Multi-choice option ids likewise — keeps histogram aggregation keys stable across edits per schema.md.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.Survey_ByPlaytestId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_PatchSurvey_ByPlaytestIdMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<
      V1EditSurveyResponse,
      AxiosError<ApiError>,
      SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceEditSurveyBody }
    >,
    'mutationKey'
  >,
  callback?: (data: V1EditSurveyResponse) => void
): UseMutationResult<
  V1EditSurveyResponse,
  AxiosError<ApiError>,
  SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceEditSurveyBody }
> => {
  const mutationFn = async (input: SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceEditSurveyBody }) => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).patchSurvey_ByPlaytestId(input.playtestId, input.data)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubServiceAdmin.Survey_ByPlaytestId],
    mutationFn,
    ...options
  })
}

/**
 * Re-reject returns the existing row (natural-key idempotency). rejection_reason is admin-visible (max 500 chars per schema.md).
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.Applicant_ByApplicantIdReject, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRejectMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<
      V1RejectApplicantResponse,
      AxiosError<ApiError>,
      SdkSetConfigParam & { applicantId: string; data: PlaytesthubServiceRejectApplicantBody }
    >,
    'mutationKey'
  >,
  callback?: (data: V1RejectApplicantResponse) => void
): UseMutationResult<
  V1RejectApplicantResponse,
  AxiosError<ApiError>,
  SdkSetConfigParam & { applicantId: string; data: PlaytesthubServiceRejectApplicantBody }
> => {
  const mutationFn = async (input: SdkSetConfigParam & { applicantId: string; data: PlaytesthubServiceRejectApplicantBody }) => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).createApplicant_ByApplicantIdReject(input.applicantId, input.data)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubServiceAdmin.Applicant_ByApplicantIdReject],
    mutationFn,
    ...options
  })
}

/**
 * actor_filter='system' maps to actorUserId IS NULL per PRD §4.7. action_filter is exact-match on the action string. before_json / after_json carry the JSONB columns verbatim — the client renders the diff.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.AuditLog_ByPlaytestId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_GetAuditLog_ByPlaytestId = (
  sdk: AccelByteSDK,
  input: SdkSetConfigParam & {
    playtestId: string
    queryParams?: { actorFilter?: string | null; actionFilter?: string | null; pageToken?: string | null; pageSize?: number }
  },
  options?: Omit<UseQueryOptions<V1ListAuditLogResponse, AxiosError<ApiError>>, 'queryKey'>,
  callback?: (data: AxiosResponse<V1ListAuditLogResponse>) => void
): UseQueryResult<V1ListAuditLogResponse, AxiosError<ApiError>> => {
  const queryFn = (sdk: AccelByteSDK, input: Parameters<typeof usePlaytesthubServiceAdminApi_GetAuditLog_ByPlaytestId>[1]) => async () => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).getAuditLog_ByPlaytestId(input.playtestId, input.queryParams)
    callback?.(response)
    return response.data
  }

  return useQuery<V1ListAuditLogResponse, AxiosError<ApiError>>({
    queryKey: [Key_PlaytesthubServiceAdmin.AuditLog_ByPlaytestId, input],
    queryFn: queryFn(sdk, input),
    ...options
  })
}

/**
 * Re-approve on an already-APPROVED applicant returns the existing row (natural-key idempotency). Errors per docs/errors.md ApproveApplicant rows.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.Applicant_ByApplicantIdApprove, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdApproveMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<
      V1ApproveApplicantResponse,
      AxiosError<ApiError>,
      SdkSetConfigParam & { applicantId: string; data: PlaytesthubServiceApproveApplicantBody }
    >,
    'mutationKey'
  >,
  callback?: (data: V1ApproveApplicantResponse) => void
): UseMutationResult<
  V1ApproveApplicantResponse,
  AxiosError<ApiError>,
  SdkSetConfigParam & { applicantId: string; data: PlaytesthubServiceApproveApplicantBody }
> => {
  const mutationFn = async (input: SdkSetConfigParam & { applicantId: string; data: PlaytesthubServiceApproveApplicantBody }) => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).createApplicant_ByApplicantIdApprove(input.applicantId, input.data)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubServiceAdmin.Applicant_ByApplicantIdApprove],
    mutationFn,
    ...options
  })
}

/**
 * No cooldown — double-click sends two DMs (PRD §5.4). Returns the updated Applicant row with refreshed DM fields.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.Applicant_ByApplicantIdRetryDm, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRetryDmMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<
      V1RetryDmResponse,
      AxiosError<ApiError>,
      SdkSetConfigParam & { applicantId: string; data: PlaytesthubServiceRetryDmBody }
    >,
    'mutationKey'
  >,
  callback?: (data: V1RetryDmResponse) => void
): UseMutationResult<
  V1RetryDmResponse,
  AxiosError<ApiError>,
  SdkSetConfigParam & { applicantId: string; data: PlaytesthubServiceRetryDmBody }
> => {
  const mutationFn = async (input: SdkSetConfigParam & { applicantId: string; data: PlaytesthubServiceRetryDmBody }) => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).createApplicant_ByApplicantIdRetryDm(input.applicantId, input.data)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubServiceAdmin.Applicant_ByApplicantIdRetryDm],
    mutationFn,
    ...options
  })
}

/**
 * Proxies adt.Client.ListGames keyed on the studio derived from the caller's token. Drives the create-playtest build-picker's top-level dropdown (STATUS_M5.md B12 + Addendum 2026-05-21). Returns FailedPrecondition when ADT reports the linkage flag missing.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.GamesAdt_ByAdtLinkageId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_GetGamesAdt_ByAdtLinkageId = (
  sdk: AccelByteSDK,
  input: SdkSetConfigParam & { adtLinkageId: string },
  options?: Omit<UseQueryOptions<V1ListAdtGamesResponse, AxiosError<ApiError>>, 'queryKey'>,
  callback?: (data: AxiosResponse<V1ListAdtGamesResponse>) => void
): UseQueryResult<V1ListAdtGamesResponse, AxiosError<ApiError>> => {
  const queryFn =
    (sdk: AccelByteSDK, input: Parameters<typeof usePlaytesthubServiceAdminApi_GetGamesAdt_ByAdtLinkageId>[1]) => async () => {
      const response = await PlaytesthubServiceAdminApi(sdk, {
        coreConfig: input.coreConfig,
        axiosConfig: input.axiosConfig
      }).getGamesAdt_ByAdtLinkageId(input.adtLinkageId)
      callback?.(response)
      return response.data
    }

  return useQuery<V1ListAdtGamesResponse, AxiosError<ApiError>>({
    queryKey: [Key_PlaytesthubServiceAdmin.GamesAdt_ByAdtLinkageId, input],
    queryFn: queryFn(sdk, input),
    ...options
  })
}

/**
 * Order: createdAt DESC. Filters: status_filter (UNSPECIFIED → no filter), dm_failed_filter (true → only lastDmStatus='failed'). page_token is opaque; absent → start of stream. page_size 0 → server default (50).
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.Applicants_ByPlaytestId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_GetApplicants_ByPlaytestId = (
  sdk: AccelByteSDK,
  input: SdkSetConfigParam & {
    playtestId: string
    queryParams?: {
      statusFilter?: 'APPLICANT_STATUS_UNSPECIFIED' | 'APPLICANT_STATUS_PENDING' | 'APPLICANT_STATUS_APPROVED' | 'APPLICANT_STATUS_REJECTED'
      dmFailedFilter?: boolean | null
      pageToken?: string | null
      pageSize?: number
    }
  },
  options?: Omit<UseQueryOptions<V1ListApplicantsResponse, AxiosError<ApiError>>, 'queryKey'>,
  callback?: (data: AxiosResponse<V1ListApplicantsResponse>) => void
): UseQueryResult<V1ListApplicantsResponse, AxiosError<ApiError>> => {
  const queryFn =
    (sdk: AccelByteSDK, input: Parameters<typeof usePlaytesthubServiceAdminApi_GetApplicants_ByPlaytestId>[1]) => async () => {
      const response = await PlaytesthubServiceAdminApi(sdk, {
        coreConfig: input.coreConfig,
        axiosConfig: input.axiosConfig
      }).getApplicants_ByPlaytestId(input.playtestId, input.queryParams)
      callback?.(response)
      return response.data
    }

  return useQuery<V1ListApplicantsResponse, AxiosError<ApiError>>({
    queryKey: [Key_PlaytesthubServiceAdmin.Applicants_ByPlaytestId, input],
    queryFn: queryFn(sdk, input),
    ...options
  })
}

/**
 * Proxies adt.Client.ListBuilds keyed on the studio derived from the caller's token. Returns FailedPrecondition when ADT reports the linkage flag missing.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.BuildsAdt_ByAdtLinkageId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_GetBuildsAdt_ByAdtLinkageId = (
  sdk: AccelByteSDK,
  input: SdkSetConfigParam & { adtLinkageId: string; queryParams?: { adtGameId?: string | null } },
  options?: Omit<UseQueryOptions<V1ListAdtBuildsResponse, AxiosError<ApiError>>, 'queryKey'>,
  callback?: (data: AxiosResponse<V1ListAdtBuildsResponse>) => void
): UseQueryResult<V1ListAdtBuildsResponse, AxiosError<ApiError>> => {
  const queryFn =
    (sdk: AccelByteSDK, input: Parameters<typeof usePlaytesthubServiceAdminApi_GetBuildsAdt_ByAdtLinkageId>[1]) => async () => {
      const response = await PlaytesthubServiceAdminApi(sdk, {
        coreConfig: input.coreConfig,
        axiosConfig: input.axiosConfig
      }).getBuildsAdt_ByAdtLinkageId(input.adtLinkageId, input.queryParams)
      callback?.(response)
      return response.data
    }

  return useQuery<V1ListAdtBuildsResponse, AxiosError<ApiError>>({
    queryKey: [Key_PlaytesthubServiceAdmin.BuildsAdt_ByAdtLinkageId, input],
    queryFn: queryFn(sdk, input),
    ...options
  })
}

/**
 * Each call generates a fresh batch via the AGS Campaign API. Per docs/ags-failure-modes.md the call is not transactional; partial fulfillment commits the codes received. STEAM_KEYS playtests reject with FailedPrecondition.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.CodesTopUp_ByPlaytestId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_CreateCodesTopUp_ByPlaytestIdMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<
      V1TopUpCodesResponse,
      AxiosError<ApiError>,
      SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceTopUpCodesBody }
    >,
    'mutationKey'
  >,
  callback?: (data: V1TopUpCodesResponse) => void
): UseMutationResult<
  V1TopUpCodesResponse,
  AxiosError<ApiError>,
  SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceTopUpCodesBody }
> => {
  const mutationFn = async (input: SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceTopUpCodesBody }) => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).createCodesTopUp_ByPlaytestId(input.playtestId, input.data)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubServiceAdmin.CodesTopUp_ByPlaytestId],
    mutationFn,
    ...options
  })
}

/**
 * PRD §4.3: UTF-8, charset [A-Za-z0-9._-], 1–128 chars/code, file ≤10 MB, ≤50,000 codes, file-level + cross-row dedup. On any violation the response carries per-line rejection details and 0 codes are inserted.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.CodesUpload_ByPlaytestId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_CreateCodesUpload_ByPlaytestIdMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<
      V1UploadCodesResponse,
      AxiosError<ApiError>,
      SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceUploadCodesBody }
    >,
    'mutationKey'
  >,
  callback?: (data: V1UploadCodesResponse) => void
): UseMutationResult<
  V1UploadCodesResponse,
  AxiosError<ApiError>,
  SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceUploadCodesBody }
> => {
  const mutationFn = async (input: SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceUploadCodesBody }) => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).createCodesUpload_ByPlaytestId(input.playtestId, input.data)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubServiceAdmin.CodesUpload_ByPlaytestId],
    mutationFn,
    ...options
  })
}

/**
 * Read joins applicant + the latest dm.sent audit row to derive code_sent_at for STEAM_KEYS / AGS_CAMPAIGN rows; ADT rows return NULL code_sent_at. Four ADT telemetry cache fields ship in the response shape but stay NULL/zero across M5.C.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.Participants_ByPlaytestId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_GetParticipants_ByPlaytestId = (
  sdk: AccelByteSDK,
  input: SdkSetConfigParam & {
    playtestId: string
    queryParams?: {
      statusFilter?: 'APPLICANT_STATUS_UNSPECIFIED' | 'APPLICANT_STATUS_PENDING' | 'APPLICANT_STATUS_APPROVED' | 'APPLICANT_STATUS_REJECTED'
    }
  },
  options?: Omit<UseQueryOptions<V1GetPlaytestParticipantsResponse, AxiosError<ApiError>>, 'queryKey'>,
  callback?: (data: AxiosResponse<V1GetPlaytestParticipantsResponse>) => void
): UseQueryResult<V1GetPlaytestParticipantsResponse, AxiosError<ApiError>> => {
  const queryFn =
    (sdk: AccelByteSDK, input: Parameters<typeof usePlaytesthubServiceAdminApi_GetParticipants_ByPlaytestId>[1]) => async () => {
      const response = await PlaytesthubServiceAdminApi(sdk, {
        coreConfig: input.coreConfig,
        axiosConfig: input.axiosConfig
      }).getParticipants_ByPlaytestId(input.playtestId, input.queryParams)
      callback?.(response)
      return response.data
    }

  return useQuery<V1GetPlaytestParticipantsResponse, AxiosError<ApiError>>({
    queryKey: [Key_PlaytesthubServiceAdmin.Participants_ByPlaytestId, input],
    queryFn: queryFn(sdk, input),
    ...options
  })
}

/**
 * Per-row status aggregated from announcement_recipient.dm_status: SENT (all sent), SENDING (any queued), PARTIAL (mix sent + failed), FAILED (all failed).
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.Announcements_ByPlaytestId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_GetAnnouncements_ByPlaytestId = (
  sdk: AccelByteSDK,
  input: SdkSetConfigParam & { playtestId: string },
  options?: Omit<UseQueryOptions<V1ListAnnouncementsResponse, AxiosError<ApiError>>, 'queryKey'>,
  callback?: (data: AxiosResponse<V1ListAnnouncementsResponse>) => void
): UseQueryResult<V1ListAnnouncementsResponse, AxiosError<ApiError>> => {
  const queryFn =
    (sdk: AccelByteSDK, input: Parameters<typeof usePlaytesthubServiceAdminApi_GetAnnouncements_ByPlaytestId>[1]) => async () => {
      const response = await PlaytesthubServiceAdminApi(sdk, {
        coreConfig: input.coreConfig,
        axiosConfig: input.axiosConfig
      }).getAnnouncements_ByPlaytestId(input.playtestId)
      callback?.(response)
      return response.data
    }

  return useQuery<V1ListAnnouncementsResponse, AxiosError<ApiError>>({
    queryKey: [Key_PlaytesthubServiceAdmin.Announcements_ByPlaytestId, input],
    queryFn: queryFn(sdk, input),
    ...options
  })
}

/**
 * Resolves recipients at call time (NOT a stored snapshot). Subject + message are PII-sensitive and are never written to audit JSONB or structured logs.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.Announcement_ByPlaytestId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_CreateAnnouncement_ByPlaytestIdMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<
      V1CreateAnnouncementResponse,
      AxiosError<ApiError>,
      SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceCreateAnnouncementBody }
    >,
    'mutationKey'
  >,
  callback?: (data: V1CreateAnnouncementResponse) => void
): UseMutationResult<
  V1CreateAnnouncementResponse,
  AxiosError<ApiError>,
  SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceCreateAnnouncementBody }
> => {
  const mutationFn = async (input: SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceCreateAnnouncementBody }) => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).createAnnouncement_ByPlaytestId(input.playtestId, input.data)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubServiceAdmin.Announcement_ByPlaytestId],
    mutationFn,
    ...options
  })
}

/**
 * Issues a download URL for the playtest's current adt_build_id (same call as ApproveApplicant) and persists adt_build_status: 'OK' when a URL was minted, 'UNAVAILABLE' when ADT returns build-not-found. Non-ADT playtest → FailedPrecondition. Linkage missing / ADT unreachable → FailedPrecondition / Unavailable (status not overwritten). Side effect: a throwaway download URL is minted on success.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.AdtBuildCheck_ByPlaytestId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_CreateAdtBuildCheck_ByPlaytestIdMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<
      V1CheckAdtBuildResponse,
      AxiosError<ApiError>,
      SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceCheckAdtBuildBody }
    >,
    'mutationKey'
  >,
  callback?: (data: V1CheckAdtBuildResponse) => void
): UseMutationResult<
  V1CheckAdtBuildResponse,
  AxiosError<ApiError>,
  SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceCheckAdtBuildBody }
> => {
  const mutationFn = async (input: SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceCheckAdtBuildBody }) => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).createAdtBuildCheck_ByPlaytestId(input.playtestId, input.data)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubServiceAdmin.AdtBuildCheck_ByPlaytestId],
    mutationFn,
    ...options
  })
}

export const usePlaytesthubServiceAdminApi_CreatePlaytest_ByPlaytestIdTransitionStatuMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<
      V1TransitionPlaytestStatusResponse,
      AxiosError<ApiError>,
      SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceTransitionPlaytestStatusBody }
    >,
    'mutationKey'
  >,
  callback?: (data: V1TransitionPlaytestStatusResponse) => void
): UseMutationResult<
  V1TransitionPlaytestStatusResponse,
  AxiosError<ApiError>,
  SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceTransitionPlaytestStatusBody }
> => {
  const mutationFn = async (input: SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceTransitionPlaytestStatusBody }) => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).createPlaytest_ByPlaytestIdTransitionStatu(input.playtestId, input.data)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubServiceAdmin.Playtest_ByPlaytestIdTransitionStatu],
    mutationFn,
    ...options
  })
}

/**
 * Mutates adt_game_id + adt_build_id on an ADT playtest after verifying the pair against the linked ADT namespace via ListBuilds. adt_namespace is immutable (relink instead). Non-ADT playtest → FailedPrecondition. Build absent from the (namespace, game) pair → InvalidArgument. Already-approved applicants keep the download URL already DM'd; future approvals + RetryDM re-mint against the new build (PRD §4.8.3).
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.AdtBuildChange_ByPlaytestId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_CreateAdtBuildChange_ByPlaytestIdMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<
      V1ChangeAdtBuildResponse,
      AxiosError<ApiError>,
      SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceChangeAdtBuildBody }
    >,
    'mutationKey'
  >,
  callback?: (data: V1ChangeAdtBuildResponse) => void
): UseMutationResult<
  V1ChangeAdtBuildResponse,
  AxiosError<ApiError>,
  SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceChangeAdtBuildBody }
> => {
  const mutationFn = async (input: SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceChangeAdtBuildBody }) => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).createAdtBuildChange_ByPlaytestId(input.playtestId, input.data)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubServiceAdmin.AdtBuildChange_ByPlaytestId],
    mutationFn,
    ...options
  })
}

/**
 * Default page_size 50, max 200. Optional survey_id_filter narrows to a single Survey version for per-version aggregate split.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.SurveyResponses_ByPlaytestId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_GetSurveyResponses_ByPlaytestId = (
  sdk: AccelByteSDK,
  input: SdkSetConfigParam & {
    playtestId: string
    queryParams?: { surveyIdFilter?: string | null; pageToken?: string | null; pageSize?: number }
  },
  options?: Omit<UseQueryOptions<V1ListSurveyResponsesResponse, AxiosError<ApiError>>, 'queryKey'>,
  callback?: (data: AxiosResponse<V1ListSurveyResponsesResponse>) => void
): UseQueryResult<V1ListSurveyResponsesResponse, AxiosError<ApiError>> => {
  const queryFn =
    (sdk: AccelByteSDK, input: Parameters<typeof usePlaytesthubServiceAdminApi_GetSurveyResponses_ByPlaytestId>[1]) => async () => {
      const response = await PlaytesthubServiceAdminApi(sdk, {
        coreConfig: input.coreConfig,
        axiosConfig: input.axiosConfig
      }).getSurveyResponses_ByPlaytestId(input.playtestId, input.queryParams)
      callback?.(response)
      return response.data
    }

  return useQuery<V1ListSurveyResponsesResponse, AxiosError<ApiError>>({
    queryKey: [Key_PlaytesthubServiceAdmin.SurveyResponses_ByPlaytestId, input],
    queryFn: queryFn(sdk, input),
    ...options
  })
}

/**
 * Fetch-only recovery for the case where AGS holds codes our DB never persisted. STEAM_KEYS playtests reject with FailedPrecondition.
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.CodesSyncFromAg_ByPlaytestId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_CreateCodesSyncFromAg_ByPlaytestIdMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<
      V1SyncFromAgsResponse,
      AxiosError<ApiError>,
      SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceSyncFromAgsBody }
    >,
    'mutationKey'
  >,
  callback?: (data: V1SyncFromAgsResponse) => void
): UseMutationResult<
  V1SyncFromAgsResponse,
  AxiosError<ApiError>,
  SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceSyncFromAgsBody }
> => {
  const mutationFn = async (input: SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceSyncFromAgsBody }) => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).createCodesSyncFromAg_ByPlaytestId(input.playtestId, input.data)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubServiceAdmin.CodesSyncFromAg_ByPlaytestId],
    mutationFn,
    ...options
  })
}

/**
 * Walks every applicant with last_dm_status=FAILED for the playtest and enqueues each through the same DM-queue path as approve, respecting the 10k cap and configured drain rate. Overflowed rows stay FAILED with last_dm_error='dm_queue_overflow' (PRD §5.5).
 *
 * #### Default Query Options
 * The default options include:
 * ```
 * {
 *    queryKey: [Key_PlaytesthubServiceAdmin.ApplicantsRetryFailedDm_ByPlaytestId, input]
 * }
 * ```
 */
export const usePlaytesthubServiceAdminApi_CreateApplicantsRetryFailedDm_ByPlaytestIdMutation = (
  sdk: AccelByteSDK,
  options?: Omit<
    UseMutationOptions<
      V1RetryFailedDmsResponse,
      AxiosError<ApiError>,
      SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceRetryFailedDmsBody }
    >,
    'mutationKey'
  >,
  callback?: (data: V1RetryFailedDmsResponse) => void
): UseMutationResult<
  V1RetryFailedDmsResponse,
  AxiosError<ApiError>,
  SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceRetryFailedDmsBody }
> => {
  const mutationFn = async (input: SdkSetConfigParam & { playtestId: string; data: PlaytesthubServiceRetryFailedDmsBody }) => {
    const response = await PlaytesthubServiceAdminApi(sdk, {
      coreConfig: input.coreConfig,
      axiosConfig: input.axiosConfig
    }).createApplicantsRetryFailedDm_ByPlaytestId(input.playtestId, input.data)
    callback?.(response.data)
    return response.data
  }

  return useMutation({
    mutationKey: [Key_PlaytesthubServiceAdmin.ApplicantsRetryFailedDm_ByPlaytestId],
    mutationFn,
    ...options
  })
}
