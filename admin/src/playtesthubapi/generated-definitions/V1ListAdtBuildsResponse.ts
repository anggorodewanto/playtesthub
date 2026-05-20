/*
 * Copyright (c) 2022-2026 AccelByte Inc. All Rights Reserved
 * This is licensed software from AccelByte Inc, for limitations
 * and restrictions contact your company contract manager.
 */
import { z } from 'zod'
import { V1AdtBuild } from './V1AdtBuild.js'

export const V1ListAdtBuildsResponse = z.object({ builds: z.array(V1AdtBuild).nullish() })

export interface V1ListAdtBuildsResponse extends z.TypeOf<typeof V1ListAdtBuildsResponse> {}
