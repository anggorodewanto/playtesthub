/*
 * Copyright (c) 2022-2026 AccelByte Inc. All Rights Reserved
 * This is licensed software from AccelByte Inc, for limitations
 * and restrictions contact your company contract manager.
 */
import { z } from 'zod'
import { V1ApplicantStatus } from './V1ApplicantStatus.js'
import { V1DmStatus } from './V1DmStatus.js'
import { V1Platform } from './V1Platform.js'

export const V1Applicant = z.object({
  id: z.string().nullish(),
  playtestId: z.string().nullish(),
  userId: z.string().nullish(),
  discordHandle: z.string().nullish(),
  platforms: z.array(V1Platform).nullish(),
  ndaVersionHash: z.string().nullish(),
  status: V1ApplicantStatus.nullish(),
  grantedCodeId: z.string().nullish(),
  approvedAt: z.string().nullish(),
  rejectionReason: z.string().nullish(),
  lastDmStatus: V1DmStatus.nullish(),
  lastDmAttemptAt: z.string().nullish(),
  lastDmError: z.string().nullish(),
  createdAt: z.string().nullish(),
  autoApproved: z.boolean().nullish(),
  surveyResponseSubmittedAt: z.string().nullish()
})

export interface V1Applicant extends z.TypeOf<typeof V1Applicant> {}
