export interface PersonalityGenFlow {
  id: string;
  status: 'in_progress' | 'generated' | 'accepted';
  current_step: number;
  answers: Record<string, string>;
  generated_prompt: string;
  generated_about_me: string;
  generated_names: string[];
  personality_id?: string;
  /** Image style hint chosen on page 1 (e.g. 'auto', 'anime', 'none'). */
  image_style: string;
  /** UUID of the reference image uploaded by the user. */
  reference_image_id?: string;
  /** Authenticated URL for the reference image. */
  reference_image_url?: string;
  created_at: string;
  updated_at: string;
}

export interface UpdateFlowRequest {
  current_step: number;
  answers: Record<string, string>;
  /** Image style hint for portrait/expression generation. */
  image_style: string;
  /** Optional reference image file attachment UUID. */
  reference_image_id?: string;
}

/** Canonical list of image style options for the personality wizard. */
export const IMAGE_STYLE_AUTO = 'auto';
export const IMAGE_STYLE_NONE = 'none';
export const IMAGE_STYLE_OTHER = 'other';
export const CUSTOM_IMAGE_STYLE_MAX_LEN = 20;

export const IMAGE_STYLES = [
  { value: IMAGE_STYLE_AUTO, label: 'Auto', emphasis: 'auto' as const },
  { value: IMAGE_STYLE_NONE, label: 'No Image', emphasis: 'none' as const },
  { value: '3d-cartoon', label: '3D Cartoon' },
  { value: 'anime', label: 'Anime' },
  { value: 'comic-book', label: 'Comic Book' },
  { value: 'watercolor', label: 'Watercolor' },
  { value: 'pixel-art', label: 'Pixel Art' },
  { value: 'photorealistic', label: 'Photo Realistic' },
  { value: 'flat-vector', label: 'Flat Vector' },
  { value: 'pencil-sketch', label: 'Pencil Sketch' },
  { value: IMAGE_STYLE_OTHER, label: 'Other', emphasis: 'other' as const },
] as const;

export type ImageStyleValue = typeof IMAGE_STYLES[number]['value'];

const PRESET_STYLE_VALUES = new Set<string>(IMAGE_STYLES.map(s => s.value));

/** Maps a stored image_style from the API back to wizard selection + optional custom text. */
export function parseStoredImageStyle(stored: string | undefined | null): { selection: string; custom: string } {
  const value = (stored ?? '').trim() || IMAGE_STYLE_AUTO;
  if (PRESET_STYLE_VALUES.has(value)) {
    return { selection: value, custom: '' };
  }
  return {
    selection: IMAGE_STYLE_OTHER,
    custom: value.slice(0, CUSTOM_IMAGE_STYLE_MAX_LEN),
  };
}

/** Value persisted to the backend for portrait/expression generation hints. */
export function effectiveImageStyle(selection: string, custom: string): string {
  if (selection === IMAGE_STYLE_OTHER) {
    const trimmed = custom.trim().slice(0, CUSTOM_IMAGE_STYLE_MAX_LEN);
    return trimmed || IMAGE_STYLE_AUTO;
  }
  return selection;
}

export interface AcceptFlowRequest {
  name: string;
  cover_image_id?: string;
}
