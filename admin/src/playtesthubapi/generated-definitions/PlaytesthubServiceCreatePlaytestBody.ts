/*
 * Copyright (c) 2022-2026 AccelByte Inc. All Rights Reserved
 * This is licensed software from AccelByte Inc, for limitations
 * and restrictions contact your company contract manager.
 */
import { z } from 'zod'
import { V1DistributionModel } from './V1DistributionModel.js'
import { V1Platform } from './V1Platform.js'

export const PlaytesthubServiceCreatePlaytestBody = z.object({
  slug: z.string().nullish(),
  title: z.string().nullish(),
  description: z.string().nullish(),
  bannerImageUrl: z.string().nullish(),
  platforms: z.array(V1Platform).nullish(),
  startsAt: z.string().nullish(),
  endsAt: z.string().nullish(),
  ndaRequired: z.boolean().nullish(),
  ndaText: z.string().nullish(),
  distributionModel: V1DistributionModel.nullish(),
  initialCodeQuantity: z.number().int().nullish(),
  autoApprove: z.boolean().nullish(),
  autoApproveLimit: z.number().int().nullish()
})

export interface PlaytesthubServiceCreatePlaytestBody extends z.TypeOf<typeof PlaytesthubServiceCreatePlaytestBody> {}
