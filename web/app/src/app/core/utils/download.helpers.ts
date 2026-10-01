/**
 * Pulls the file name out of a Content-Disposition header, handling both the plain
 * `filename="x"` form and the RFC 5987 `filename*=utf-8''x` form the API uses for non-ASCII names.
 * Returns `fallback` when the header is missing or carries no usable name.
 */
export function filenameFromContentDisposition(header: string | null | undefined, fallback: string): string {
  if (!header) return fallback;

  const encoded = /filename\*\s*=\s*(?:utf-8|UTF-8)''([^;]+)/.exec(header);
  if (encoded?.[1]) {
    try {
      const decoded = decodeURIComponent(encoded[1].trim());
      if (decoded) return decoded;
    } catch {
      // Malformed percent-encoding: fall through to the plain form.
    }
  }

  const plain = /filename\s*=\s*"([^"]+)"/.exec(header) ?? /filename\s*=\s*([^;]+)/.exec(header);
  return plain?.[1]?.trim() || fallback;
}

/** Saves `blob` to the user's machine as `filename` by clicking a temporary link. */
export function saveBlobAsFile(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
}
