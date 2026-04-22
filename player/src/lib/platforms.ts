import type { Platform } from './api';

export type PlatformOption = { value: Platform; label: string };

export const PLATFORM_OPTIONS: PlatformOption[] = [
  { value: 'PLATFORM_STEAM', label: 'Steam' },
  { value: 'PLATFORM_XBOX', label: 'Xbox' },
  { value: 'PLATFORM_PLAYSTATION', label: 'PlayStation' },
  { value: 'PLATFORM_EPIC', label: 'Epic' },
  { value: 'PLATFORM_OTHER', label: 'Other' },
];

export function platformLabel(p: Platform): string {
  const match = PLATFORM_OPTIONS.find((o) => o.value === p);
  return match ? match.label : p;
}
