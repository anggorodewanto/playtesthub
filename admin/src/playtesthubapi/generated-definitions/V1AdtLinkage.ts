/*
 * Copyright (c) 2022-2026 AccelByte Inc. All Rights Reserved
 * This is licensed software from AccelByte Inc, for limitations
 * and restrictions contact your company contract manager.
 */
import { z } from 'zod'

export const V1AdtLinkage = z.object({
  id: z.string().nullish(),
  studioNamespace: z.string().nullish(),
  adtNamespace: z.string().nullish(),
  linkedByUserId: z.string().nullish(),
  linkedAt: z.string().nullish(),
  deletedAt: z.string().nullish()
})

export interface V1AdtLinkage extends z.TypeOf<typeof V1AdtLinkage> {}
