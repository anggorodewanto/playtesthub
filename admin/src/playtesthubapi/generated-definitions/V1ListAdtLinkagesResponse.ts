/*
 * Copyright (c) 2022-2026 AccelByte Inc. All Rights Reserved
 * This is licensed software from AccelByte Inc, for limitations
 * and restrictions contact your company contract manager.
 */
import { z } from 'zod'
import { V1AdtLinkage } from './V1AdtLinkage.js'

export const V1ListAdtLinkagesResponse = z.object({ linkages: z.array(V1AdtLinkage).nullish() })

export interface V1ListAdtLinkagesResponse extends z.TypeOf<typeof V1ListAdtLinkagesResponse> {}
