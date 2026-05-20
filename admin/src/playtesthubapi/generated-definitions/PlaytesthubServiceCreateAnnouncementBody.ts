/*
 * Copyright (c) 2022-2026 AccelByte Inc. All Rights Reserved
 * This is licensed software from AccelByte Inc, for limitations
 * and restrictions contact your company contract manager.
 */
import { z } from 'zod'
import { V1AnnouncementSendToFilter } from './V1AnnouncementSendToFilter.js'

export const PlaytesthubServiceCreateAnnouncementBody = z.object({
  sendToFilter: V1AnnouncementSendToFilter.nullish(),
  subject: z.string().nullish(),
  message: z.string().nullish()
})

export interface PlaytesthubServiceCreateAnnouncementBody extends z.TypeOf<typeof PlaytesthubServiceCreateAnnouncementBody> {}
