/** File extension from the stored gallery name (includes leading dot). */
export function galleryFileExtension(name: string): string {
  const dot = name.lastIndexOf('.');
  if (dot <= 0) {
    return '';
  }
  return name.slice(dot);
}

/** Display name without extension for rename UI. */
export function galleryDisplayBaseName(name: string): string {
  const ext = galleryFileExtension(name);
  if (!ext) {
    return name;
  }
  return name.slice(0, name.length - ext.length);
}

/** Rebuilds stored filename, preserving the original extension when present. */
export function galleryFilenameFromBaseName(storedName: string, baseName: string): string {
  const trimmed = baseName.trim();
  if (!trimmed) {
    return storedName;
  }
  const ext = galleryFileExtension(storedName);
  if (!ext) {
    return trimmed;
  }
  if (trimmed.toLowerCase().endsWith(ext.toLowerCase())) {
    return trimmed;
  }
  return `${trimmed}${ext}`;
}
