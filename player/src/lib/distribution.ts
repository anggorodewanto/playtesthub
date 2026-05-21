import type { DistributionModel } from './api';

const LABELS: Record<DistributionModel, string> = {
  DISTRIBUTION_MODEL_UNSPECIFIED: 'Unspecified',
  DISTRIBUTION_MODEL_STEAM_KEYS: 'Steam keys',
  DISTRIBUTION_MODEL_AGS_CAMPAIGN: 'In-game code',
  DISTRIBUTION_MODEL_ADT: 'Direct Download',
};

export function distributionLabel(model: DistributionModel | undefined): string {
  if (!model) return LABELS.DISTRIBUTION_MODEL_UNSPECIFIED;
  return LABELS[model] ?? LABELS.DISTRIBUTION_MODEL_UNSPECIFIED;
}
