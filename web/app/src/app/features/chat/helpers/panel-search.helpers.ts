/**
 * Case-insensitive substring match for the context panel's search box.
 * An empty (or whitespace-only) query matches everything.
 */
export function matchesQuery(query: string, ...fields: ReadonlyArray<string | null | undefined>): boolean {
  const needle = query.trim().toLowerCase();
  if (!needle) return true;
  return fields.some(field => (field ?? '').toLowerCase().includes(needle));
}
