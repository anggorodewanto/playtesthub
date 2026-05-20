/*
 * Copyright (c) 2022-2026 AccelByte Inc. All Rights Reserved
 * This is licensed software from AccelByte Inc, for limitations
 * and restrictions contact your company contract manager.
 */
import { z } from 'zod'

export const V1AdtBuild = z.object({
  id: z.string().nullish(),
  name: z.string().nullish(),
  version: z.string().nullish(),
  uploadedAt: z.string().nullish(),
  platform: z.string().nullish()
})

export interface V1AdtBuild extends z.TypeOf<typeof V1AdtBuild> {}
