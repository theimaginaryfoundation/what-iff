/** An audio container (ADR 0x052): `webm` is Opus in WebM, `mp4` AAC in MP4 or M4A. */
export type AudioFormat = 'mp3' | 'wav' | 'webm' | 'mp4' | 'ogg' | 'flac';

/** The account's dictation model, or why there is none (GET /speech/dictation). */
export interface DictationModel {
  available: boolean;
  /** Why it isn't available: utility_model_not_set, utility_role_off, utility_model_unavailable or forbidden. */
  code?: string;
  model?: string;
  display_name?: string;
  /** Always `['wav']`: dictation sends 16 kHz mono PCM WAV only. */
  formats?: AudioFormat[];
  /** The longest clip it takes, in seconds. */
  max_seconds?: number;
}

/** One recorded clip, ready to transcribe: 16 kHz mono PCM WAV. */
export interface DictationClip {
  audio: Blob;
  format: 'wav';
  /** How long it ran, for skipping a press too short to hold speech; the server bills from the data. */
  seconds: number;
}
