export function initials(name: string | null | undefined): string {
  const trimmed = (name ?? '').trim();

  if (!trimmed) {
    return '?';
  }

  return trimmed
    .split(/\s+/)
    .map(part => Array.from(part)[0] ?? '')
    .join('')
    .toUpperCase();
}

export function shortName(name: string | null | undefined): string {
  const trimmed = (name ?? '').trim();

  if (!trimmed) {
    return '';
  }

  const parts = trimmed.split(/\s+/);

  if (parts.length <= 1) {
    return trimmed;
  }

  const [first, ...rest] = parts;
  return `${first} ${rest.map(part => `${Array.from(part)[0] ?? ''}.`).join(' ')}`;
}
