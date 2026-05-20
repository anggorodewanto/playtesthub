/*
 * Copyright (c) 2022-2026 AccelByte Inc. All Rights Reserved
 * This is licensed software from AccelByte Inc, for limitations
 * and restrictions contact your company contract manager.
 */
import { z } from 'zod'
import { V1ParticipantRow } from './V1ParticipantRow.js'

export const V1GetPlaytestParticipantsResponse = z.object({ participants: z.array(V1ParticipantRow).nullish() })

export interface V1GetPlaytestParticipantsResponse extends z.TypeOf<typeof V1GetPlaytestParticipantsResponse> {}
