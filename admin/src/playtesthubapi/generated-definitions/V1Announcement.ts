/*
 * Copyright (c) 2022-2026 AccelByte Inc. All Rights Reserved
 * This is licensed software from AccelByte Inc, for limitations
 * and restrictions contact your company contract manager.
 */
import { z } from 'zod'
import { V1AnnouncementSendToFilter } from './V1AnnouncementSendToFilter.js'
import { V1AnnouncementStatus } from './V1AnnouncementStatus.js'

export const V1Announcement = z.object({
  id: z.string().nullish(),
  playtestId: z.string().nullish(),
  sendToFilter: V1AnnouncementSendToFilter.nullish(),
  subject: z.string().nullish(),
  message: z.string().nullish(),
  status: V1AnnouncementStatus.nullish(),
  recipientsTotal: z.number().int().nullish(),
  recipientsSent: z.number().int().nullish(),
  createdByUserId: z.string().nullish(),
  createdAt: z.string().nullish()
})

export interface V1Announcement extends z.TypeOf<typeof V1Announcement> {}
