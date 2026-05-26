/*
 * Copyright (c) 2022-2026 AccelByte Inc. All Rights Reserved
 * This is licensed software from AccelByte Inc, for limitations
 * and restrictions contact your company contract manager.
 */
import { z } from 'zod'

export const PlaytesthubServiceChangeAdtBuildBody = z.object({ adtGameId: z.string().nullish(), adtBuildId: z.string().nullish() })

export interface PlaytesthubServiceChangeAdtBuildBody extends z.TypeOf<typeof PlaytesthubServiceChangeAdtBuildBody> {}
