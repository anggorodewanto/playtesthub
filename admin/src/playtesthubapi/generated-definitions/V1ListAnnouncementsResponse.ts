/*
 * Copyright (c) 2022-2026 AccelByte Inc. All Rights Reserved
 * This is licensed software from AccelByte Inc, for limitations
 * and restrictions contact your company contract manager.
 */
import { z } from 'zod'
import { V1Announcement } from './V1Announcement.js'

export const V1ListAnnouncementsResponse = z.object({ announcements: z.array(V1Announcement).nullish() })

export interface V1ListAnnouncementsResponse extends z.TypeOf<typeof V1ListAnnouncementsResponse> {}
