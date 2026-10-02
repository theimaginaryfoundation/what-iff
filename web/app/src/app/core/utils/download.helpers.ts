const MAX_FILENAME_LENGTH = 200;
/** Path separators plus the characters Windows forbids, so a saved file moves cleanly between OSes. */
const UNSAFE_FILENAME_CHARS = '/\\<>:"|?*';

/**
 * Pulls the file name out of a Content-Disposition header, handling both the plain
 * `filename="x"` form and the RFC 5987 `filename*=utf-8''x` form the API uses for non-ASCII names.
 * The name is sanitized for use as a download name (control characters and path separators removed,
 * length capped); `fallback` is returned when the header is missing or carries no usable name.
 */
export function filenameFromContentDisposition(header: string | null | undefined, fallback: string): string {
  return sanitizeFilename(rawFilename(header)) || fallback;
}

function sanitizeFilename(name: string): string {
  const withoutUnsafe = Array.from(name)
    .filter(ch => {
      const code = ch.codePointAt(0) ?? 0;
      return code > 0x1f && code !== 0x7f && !UNSAFE_FILENAME_CHARS.includes(ch);
    })
    .join('')
    .trim();
  return withoutUnsafe.slice(0, MAX_FILENAME_LENGTH).trim();
}

function rawFilename(header: string | null | undefined): string {
  if (!header) return '';

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
  return plain?.[1]?.trim() ?? '';
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
