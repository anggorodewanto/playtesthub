export function formatDateRange(startsAt?: string, endsAt?: string): string {
  const start = formatOne(startsAt);
  const end = formatOne(endsAt);
  if (start && end) return `${start} – ${end}`;
  return start || end || 'Dates not yet announced';
}

function formatOne(raw?: string): string | null {
  if (!raw) return null;
  const date = new Date(raw);
  if (Number.isNaN(date.getTime())) return null;
  return date.toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  });
}
