/*
 * Copyright (c) 2022-2026 AccelByte Inc. All Rights Reserved
 * This is licensed software from AccelByte Inc, for limitations
 * and restrictions contact your company contract manager.
 */
import { z } from 'zod'
import { V1AdtGame } from './V1AdtGame.js'

export const V1ListAdtGamesResponse = z.object({ games: z.array(V1AdtGame).nullish() })

export interface V1ListAdtGamesResponse extends z.TypeOf<typeof V1ListAdtGamesResponse> {}
