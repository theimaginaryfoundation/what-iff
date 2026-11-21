/** An audio container (ADR 0x052): `webm` is Opus in WebM, `mp4` AAC in MP4 or M4A. */
export type AudioFormat = 'mp3' | 'wav' | 'webm' | 'mp4' | 'ogg' | 'flac';

/** The account's dictation model, or why there is none (GET /speech/dictation). */
export interface DictationModel {
  available: boolean;
  /** Why it isn't available: utility_model_not_set, utility_role_off, utility_model_unavailable or forbidden. */
  code?: string;
  model?: string;
  display_name?: string;
  /** The formats it takes; the composer records WAV when it takes none the browser records. */
  formats?: AudioFormat[];
  /** The longest clip it takes, in seconds. */
  max_seconds?: number;
}

/** One recorded clip, ready to transcribe. */
export interface DictationClip {
  audio: Blob;
  format: AudioFormat;
  seconds: number;
}
