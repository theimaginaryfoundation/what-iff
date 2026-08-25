import { FileAttachment } from '../../../core/models/file-attachment.model';

export type GalleryImageSource = 'generated' | 'uploaded' | 'reference' | 'unknown';

/** Expression grid cells use `expression-{key}.png` (see `expression_grid_generation.go`). */
function isExpressionGridImageName(name: string): boolean {
  return /^expression-[a-z0-9_-]+\.png$/i.test(name.trim());
}

export function sourceForImage(image: FileAttachment): GalleryImageSource {
  const key = (image.s3_key ?? '').toLowerCase();
  const name = (image.name ?? '').toLowerCase();
  if (key.includes('/reference/') || key.includes('_ref_') || name.startsWith('reference-')) {
    return 'reference';
  }
  if (image.chat_message_id) {
    return 'generated';
  }
  const fileType = (image.file_type ?? '').toLowerCase();
  const isImage = fileType.startsWith('image/');
  if (isImage && isExpressionGridImageName(image.name ?? '')) {
    return 'generated';
  }
  if (image.personality_id) {
    return 'uploaded';
  }
  return 'unknown';
}

export function formatSource(source: GalleryImageSource): string {
  switch (source) {
    case 'generated':
      return 'Generated';
    case 'uploaded':
      return 'Uploaded';
    case 'reference':
      return 'Reference';
    default:
      return 'Unknown';
  }
}

export function iconForSource(source: GalleryImageSource): string {
  switch (source) {
    case 'generated':
      return 'sparkles';
    case 'uploaded':
      return 'upload';
    case 'reference':
      return 'link';
    default:
      return 'image';
  }
}
