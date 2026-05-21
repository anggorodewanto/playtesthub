/*
 * Copyright (c) 2022-2026 AccelByte Inc. All Rights Reserved
 * This is licensed software from AccelByte Inc, for limitations
 * and restrictions contact your company contract manager.
 */
import { z } from 'zod'

export const V1GetAdtClientDiagnosticsResponse = z.object({
  adtClientKind: z.string().nullish(),
  authEnabled: z.boolean().nullish(),
  adtBaseUrlSet: z.boolean().nullish(),
  agsBaseUrlSet: z.boolean().nullish(),
  agsIamClientIdSet: z.boolean().nullish(),
  agsIamClientSecretSet: z.boolean().nullish()
})

export interface V1GetAdtClientDiagnosticsResponse extends z.TypeOf<typeof V1GetAdtClientDiagnosticsResponse> {}
