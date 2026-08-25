import { FileAttachment } from '../../../core/models/file-attachment.model';
import { formatSource, GalleryImageSource, sourceForImage } from './image-source.helpers';

export type GalleryDateRange = 'any' | '7d' | '30d' | '90d';
export type GallerySourceFilter = GalleryImageSource | 'all';

export interface GalleryFilters {
  query: string;
  personalityId: string | 'all';
  source: GallerySourceFilter;
  dateRange: GalleryDateRange;
}

export interface GalleryTileVm {
  id: string;
  name: string;
  createdAt: string;
  personalityId: string | null;
  personalityName: string | null;
  personalityNames: string[];
  source: GalleryImageSource;
  sourceLabel: string;
  thumbnailUrl: string;
  fullUrl: string;
}

export interface GalleryDateGroup {
  label: string;
  items: GalleryTileVm[];
}

export const DEFAULT_GALLERY_FILTERS: GalleryFilters = {
  query: '',
  personalityId: 'all',
  source: 'all',
  dateRange: 'any',
};

export function toGalleryTileVm(
  image: FileAttachment,
  getImageUrl: (id: string, size: 'thumbnail' | 'full') => string,
  personalityNames: Record<string, string>,
): GalleryTileVm {
  const source = sourceForImage(image);
  const associatedPersonalities = image.personalities ?? [];
  const personalityIds = associatedPersonalities.map(ref => ref.id);
  const resolvedNames = associatedPersonalities
    .map(ref => personalityNames[ref.id] ?? ref.name)
    .filter((name): name is string => !!name?.trim());
  const personalityId = image.personality_id ?? personalityIds[0] ?? null;
  const personalityName = resolvedNames[0] ?? (personalityId ? personalityNames[personalityId] ?? null : null);
  const personalityNameList = resolvedNames.length > 0
    ? resolvedNames
    : personalityName
      ? [personalityName]
      : [];
  return {
    id: image.id,
    name: image.name || 'Untitled image',
    createdAt: image.created_at,
    personalityId,
    personalityName,
    personalityNames: personalityNameList,
    source,
    sourceLabel: formatSource(source),
    thumbnailUrl: getImageUrl(image.id, 'thumbnail'),
    fullUrl: getImageUrl(image.id, 'full'),
  };
}

export function applyGalleryFilters(images: FileAttachment[], filters: GalleryFilters): FileAttachment[] {
  const now = Date.now();
  return images.filter(image => {
    // Query filtering is handled server-side so search can match related entities
    // (description, chat name, personality name) without frontend false negatives.
    if (filters.personalityId !== 'all') {
      const matchesDirect = image.personality_id === filters.personalityId;
      const matchesAssociation = (image.personalities ?? []).some(ref => ref.id === filters.personalityId);
      if (!matchesDirect && !matchesAssociation) {
        return false;
      }
    }
    if (filters.source !== 'all' && sourceForImage(image) !== filters.source) {
      return false;
    }
    if (filters.dateRange !== 'any' && !matchesDateRange(image.created_at, filters.dateRange, now)) {
      return false;
    }
    return true;
  });
}

export function groupByDate(items: GalleryTileVm[]): GalleryDateGroup[] {
  const buckets = new Map<string, GalleryTileVm[]>();
  for (const item of items) {
    const label = toDateGroupLabel(item.createdAt);
    const existing = buckets.get(label);
    if (existing) {
      existing.push(item);
    } else {
      buckets.set(label, [item]);
    }
  }
  return [...buckets.entries()].map(([label, groupedItems]) => ({ label, items: groupedItems }));
}

function matchesDateRange(createdAt: string, range: GalleryDateRange, nowMs: number): boolean {
  const createdMs = Date.parse(createdAt);
  if (Number.isNaN(createdMs)) {
    return false;
  }
  const dayMs = 24 * 60 * 60 * 1000;
  switch (range) {
    case '7d':
      return nowMs - createdMs <= 7 * dayMs;
    case '30d':
      return nowMs - createdMs <= 30 * dayMs;
    case '90d':
      return nowMs - createdMs <= 90 * dayMs;
    default:
      return true;
  }
}

function toDateGroupLabel(dateIso: string): string {
  const date = new Date(dateIso);
  if (Number.isNaN(date.getTime())) {
    return 'Unknown date';
  }
  return date.toLocaleDateString(undefined, { year: 'numeric', month: 'long' });
}
