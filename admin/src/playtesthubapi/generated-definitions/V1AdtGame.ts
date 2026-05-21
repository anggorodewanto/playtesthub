/*
 * Copyright (c) 2022-2026 AccelByte Inc. All Rights Reserved
 * This is licensed software from AccelByte Inc, for limitations
 * and restrictions contact your company contract manager.
 */
import { z } from 'zod'

export const V1AdtGame = z.object({ id: z.string().nullish(), name: z.string().nullish(), createdAt: z.string().nullish() })

export interface V1AdtGame extends z.TypeOf<typeof V1AdtGame> {}
