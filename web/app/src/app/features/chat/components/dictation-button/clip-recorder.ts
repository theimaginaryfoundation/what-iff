import { AudioFormat, DictationClip } from '../../../../core/models/speech.model';

/**
 * Records one dictation clip (ADR 0x052 §10).
 *
 * Browsers record in their own formats, WebM with Opus in Chromium and Firefox and MP4 with AAC in
 * Safari, and the server transcodes nothing. So the recorder uses the browser's own format when the
 * dictation model takes it, and otherwise captures 16 kHz mono WAV through Web Audio, which every
 * model takes: about 10 MB for five minutes.
 */

/** How a clip is recorded: with MediaRecorder in a browser format, or as WAV through Web Audio. */
export type RecordingPlan = { kind: 'media'; mimeType: string; format: AudioFormat } | { kind: 'wav' };

/** The browser formats, the most widely taken first. */
const MEDIA_FORMATS: { mimeType: string; format: AudioFormat }[] = [
  { mimeType: 'audio/webm;codecs=opus', format: 'webm' },
  { mimeType: 'audio/mp4', format: 'mp4' },
  { mimeType: 'audio/ogg;codecs=opus', format: 'ogg' },
];

/** The sample rate WAV is captured at: what speech models are trained on, and a small upload. */
export const WAV_SAMPLE_RATE = 16000;

/**
 * Picks how to record for a model that takes `accepted`: the browser's own format when the model
 * takes one the browser records, else WAV.
 */
export function planRecording(accepted: readonly AudioFormat[], canRecord: (mimeType: string) => boolean): RecordingPlan {
  for (const f of MEDIA_FORMATS) {
    if (accepted.includes(f.format) && canRecord(f.mimeType)) {
      return { kind: 'media', ...f };
    }
  }
  return { kind: 'wav' };
}

/** Resamples mono samples linearly; enough for speech, and a no-op when the rates match. */
export function resample(samples: Float32Array, from: number, to: number): Float32Array {
  if (from === to || samples.length === 0) {
    return samples;
  }
  const ratio = from / to;
  const out = new Float32Array(Math.floor(samples.length / ratio));
  for (let i = 0; i < out.length; i++) {
    const pos = i * ratio;
    const left = Math.floor(pos);
    const right = Math.min(left + 1, samples.length - 1);
    const frac = pos - left;
    out[i] = samples[left] * (1 - frac) + samples[right] * frac;
  }
  return out;
}

/** Encodes mono samples (-1..1) as 16-bit PCM WAV. */
export function encodeWav(samples: Float32Array, sampleRate: number): ArrayBuffer {
  const buffer = new ArrayBuffer(44 + samples.length * 2);
  const view = new DataView(buffer);
  const text = (offset: number, s: string) => {
    for (let i = 0; i < s.length; i++) view.setUint8(offset + i, s.charCodeAt(i));
  };
  text(0, 'RIFF');
  view.setUint32(4, 36 + samples.length * 2, true);
  text(8, 'WAVE');
  text(12, 'fmt ');
  view.setUint32(16, 16, true);
  view.setUint16(20, 1, true); // PCM
  view.setUint16(22, 1, true); // mono
  view.setUint32(24, sampleRate, true);
  view.setUint32(28, sampleRate * 2, true);
  view.setUint16(32, 2, true);
  view.setUint16(34, 16, true);
  text(36, 'data');
  view.setUint32(40, samples.length * 2, true);
  for (let i = 0; i < samples.length; i++) {
    const s = Math.max(-1, Math.min(1, samples[i]));
    view.setInt16(44 + i * 2, s < 0 ? s * 0x8000 : s * 0x7fff, true);
  }
  return buffer;
}

/** What the recorder needs from the browser, so tests can stand in for it. */
export interface RecorderDeps {
  getUserMedia(constraints: MediaStreamConstraints): Promise<MediaStream>;
  createAudioContext(options?: AudioContextOptions): AudioContext;
  createMediaRecorder(stream: MediaStream, mimeType: string): MediaRecorder;
  now(): number;
}

export const browserRecorderDeps: RecorderDeps = {
  getUserMedia: c => navigator.mediaDevices.getUserMedia(c),
  createAudioContext: o => new AudioContext(o),
  createMediaRecorder: (s, mimeType) => new MediaRecorder(s, { mimeType }),
  now: () => performance.now(),
};

/** Whether this browser can record at all. */
export function canDictate(): boolean {
  return typeof navigator !== 'undefined' && !!navigator.mediaDevices?.getUserMedia && typeof AudioContext !== 'undefined';
}

/** Whether this browser's MediaRecorder records mimeType. */
export function browserCanRecord(mimeType: string): boolean {
  return typeof MediaRecorder !== 'undefined' && MediaRecorder.isTypeSupported(mimeType);
}

/** One recording, from start to a clip or a cancel. */
export class ClipRecorder {
  private stream: MediaStream | null = null;
  private context: AudioContext | null = null;
  private analyser: AnalyserNode | null = null;
  private media: MediaRecorder | null = null;
  private chunks: Blob[] = [];
  private pcm: Float32Array[] = [];
  private processor: ScriptProcessorNode | null = null;
  private source: MediaStreamAudioSourceNode | null = null;
  /** Samples kept so far, bounded by maxSamples. */
  private pcmSamples = 0;
  private startedAt: number | null = null;
  private levelBuffer: Float32Array<ArrayBuffer> | null = null;

  constructor(
    private readonly plan: RecordingPlan,
    private readonly deps: RecorderDeps = browserRecorderDeps,
    /** The longest clip; WAV capture keeps no more than this (plus a second). */
    private readonly maxSeconds = 301,
  ) {}

  /** Asks for the microphone and starts recording; rejects when permission is refused. */
  async start(): Promise<void> {
    this.stream = await this.deps.getUserMedia({ audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true } });
    const wav = this.plan.kind === 'wav';
    this.context = this.deps.createAudioContext(wav ? { sampleRate: WAV_SAMPLE_RATE } : undefined);
    const source = this.context.createMediaStreamSource(this.stream);
    this.source = source;
    this.analyser = this.context.createAnalyser();
    this.analyser.fftSize = 512;
    source.connect(this.analyser);
    if (this.plan.kind === 'media') {
      this.media = this.deps.createMediaRecorder(this.stream, this.plan.mimeType);
      this.media.ondataavailable = e => {
        if (e.data.size > 0) this.chunks.push(e.data);
      };
      this.media.start(1000);
    } else {
      // ScriptProcessorNode is deprecated, but every browser has it, and an AudioWorklet would need
      // a module the page's script policy may not allow.
      this.processor = this.context.createScriptProcessor(4096, 1, 1);
      this.processor.onaudioprocess = e => {
        // Never more than the longest clip, whatever the browser sends.
        if (!this.context || this.pcmSamples >= this.maxSeconds * this.context.sampleRate) return;
        const chunk = new Float32Array(e.inputBuffer.getChannelData(0));
        this.pcm.push(chunk);
        this.pcmSamples += chunk.length;
      };
      source.connect(this.processor);
      this.processor.connect(this.context.destination); // Chromium only processes a connected node
    }
    this.startedAt = this.deps.now();
  }

  /** Seconds since recording started. */
  elapsed(): number {
    return this.startedAt === null ? 0 : (this.deps.now() - this.startedAt) / 1000;
  }

  /** The input level now, 0 to 1, for a meter. */
  level(): number {
    if (!this.analyser) return 0;
    this.levelBuffer ??= new Float32Array(this.analyser.fftSize);
    this.analyser.getFloatTimeDomainData(this.levelBuffer);
    let sum = 0;
    for (const v of this.levelBuffer) sum += v * v;
    return Math.min(1, Math.sqrt(sum / this.levelBuffer.length) * 4);
  }

  /** Stops recording and returns the clip. */
  async stop(): Promise<DictationClip> {
    const seconds = this.elapsed();
    try {
      if (this.plan.kind === 'media' && this.media) {
        const media = this.media;
        const format = this.plan.format;
        const mimeType = this.plan.mimeType;
        await new Promise<void>(resolve => {
          media.onstop = () => resolve();
          media.stop();
        });
        return { audio: new Blob(this.chunks, { type: mimeType.split(';')[0] }), format, seconds };
      }
      const total = this.pcm.reduce((n, c) => n + c.length, 0);
      const joined = new Float32Array(total);
      let offset = 0;
      for (const c of this.pcm) {
        joined.set(c, offset);
        offset += c.length;
      }
      const rate = this.context?.sampleRate ?? WAV_SAMPLE_RATE;
      const samples = resample(joined, rate, WAV_SAMPLE_RATE);
      return { audio: new Blob([encodeWav(samples, WAV_SAMPLE_RATE)], { type: 'audio/wav' }), format: 'wav', seconds };
    } finally {
      this.release();
    }
  }

  /** Stops recording and discards it. */
  cancel(): void {
    if (this.media && this.media.state !== 'inactive') {
      this.media.onstop = null;
      this.media.stop();
    }
    this.release();
  }

  private release(): void {
    this.source?.disconnect();
    this.processor?.disconnect();
    this.stream?.getTracks().forEach(t => t.stop());
    void this.context?.close().catch(() => undefined);
    this.stream = this.context = this.analyser = this.media = this.processor = this.source = null;
    this.chunks = [];
    this.pcm = [];
    this.pcmSamples = 0;
  }
}
