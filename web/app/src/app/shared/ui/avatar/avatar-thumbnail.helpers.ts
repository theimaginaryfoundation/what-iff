import { PersonalityThumbnailCircle } from '../../../core/models/personality.model';

export interface ThumbnailImageStyle {
  objectPosition: string;
  transform: string;
}

export function thumbnailCircleToImageStyle(
  circle: PersonalityThumbnailCircle | null | undefined,
): ThumbnailImageStyle | null {
  if (!circle || circle.r <= 0) return null;
  const scale = Math.max(1, Math.min(4, 0.5 / circle.r));
  return {
    objectPosition: `${circle.cx * 100}% ${circle.cy * 100}%`,
    transform: `scale(${scale.toFixed(4)})`,
  };
}

export function thumbnailCircleToCirclePreviewTransform(
  circle: PersonalityThumbnailCircle | null | undefined,
  sourceWidth: number,
  sourceHeight: number,
  targetSize: number,
): string | null {
  if (!circle || circle.r <= 0) return null;
  const radiusPx = circle.r * Math.min(sourceWidth, sourceHeight);
  if (radiusPx <= 0) return null;
  const scale = (targetSize / 2) / radiusPx;
  const tx = targetSize / 2 - circle.cx * sourceWidth * scale;
  const ty = targetSize / 2 - circle.cy * sourceHeight * scale;
  return `translate(${tx.toFixed(2)}px, ${ty.toFixed(2)}px) scale(${scale.toFixed(4)})`;
}
