/*
 * Copyright (c) 2022-2026 AccelByte Inc. All Rights Reserved
 * This is licensed software from AccelByte Inc, for limitations
 * and restrictions contact your company contract manager.
 */
import { z } from 'zod'
import { V1ApplicantStatus } from './V1ApplicantStatus.js'

export const V1ParticipantRow = z.object({
  applicantId: z.string().nullish(),
  userId: z.string().nullish(),
  discordHandle: z.string().nullish(),
  signupAt: z.string().nullish(),
  ndaAcceptedAt: z.string().nullish(),
  codeSentAt: z.string().nullish(),
  status: V1ApplicantStatus.nullish(),
  autoApproved: z.boolean().nullish(),
  adtDownloadAt: z.string().nullish(),
  adtTotalPlaytimeSeconds: z.number().int().nullish(),
  adtHardwareSpecsJson: z.string().nullish(),
  adtCrashCount: z.number().int().nullish()
})

export interface V1ParticipantRow extends z.TypeOf<typeof V1ParticipantRow> {}
