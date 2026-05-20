/*
 * Copyright (c) 2022-2026 AccelByte Inc. All Rights Reserved
 * This is licensed software from AccelByte Inc, for limitations
 * and restrictions contact your company contract manager.
 */
import { z } from 'zod'

export const V1AnnouncementStatus = z.any()

export interface V1AnnouncementStatus extends z.TypeOf<typeof V1AnnouncementStatus> {}
