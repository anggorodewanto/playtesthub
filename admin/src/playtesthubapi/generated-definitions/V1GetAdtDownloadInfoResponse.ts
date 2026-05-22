/*
 * Copyright (c) 2022-2026 AccelByte Inc. All Rights Reserved
 * This is licensed software from AccelByte Inc, for limitations
 * and restrictions contact your company contract manager.
 */
import { z } from 'zod'

export const V1GetAdtDownloadInfoResponse = z.object({
  urls: z.array(z.string()).nullish(),
  expiresAt: z.string().nullish(),
  source: z.string().nullish()
})

export interface V1GetAdtDownloadInfoResponse extends z.TypeOf<typeof V1GetAdtDownloadInfoResponse> {}
