import { DictationClip } from '../../../../core/models/speech.model';

/**
 * Records one dictation clip (ADR 0x052 §10).
 *
 * Clips are 16 kHz mono PCM WAV, captured through Web Audio: about 10 MB for five minutes. Not the
 * browser's own WebM or MP4, though they are smaller: a compressed clip's length is whatever its
 * container says, which the client writes, while a WAV's is in its data, so the server bills what
 * was really sent. Every dictation model takes WAV.
 */

/** The sample rate WAV is captured at: what speech models are trained on, and a small upload. */
export const WAV_SAMPLE_RATE = 16000;

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
  now(): number;
}

export const browserRecorderDeps: RecorderDeps = {
  getUserMedia: c => navigator.mediaDevices.getUserMedia(c),
  createAudioContext: o => new AudioContext(o),
  now: () => performance.now(),
};

/** Whether this browser can record at all. */
export function canDictate(): boolean {
  return typeof navigator !== 'undefined' && !!navigator.mediaDevices?.getUserMedia && typeof AudioContext !== 'undefined';
}

/** start() rejects with this when the recording was cancelled before the microphone came. */
export class RecordingCancelled extends Error {
  constructor() {
    super('recording cancelled');
    this.name = 'RecordingCancelled';
  }
}

/** One recording, from start to a clip or a cancel. */
export class ClipRecorder {
  private stream: MediaStream | null = null;
  private context: AudioContext | null = null;
  private analyser: AnalyserNode | null = null;
  private pcm: Float32Array[] = [];
  private processor: ScriptProcessorNode | null = null;
  private source: MediaStreamAudioSourceNode | null = null;
  /** Samples kept so far, bounded by maxSamples. */
  private pcmSamples = 0;
  private startedAt: number | null = null;
  private levelBuffer: Float32Array<ArrayBuffer> | null = null;
  private cancelled = false;

  constructor(
    private readonly deps: RecorderDeps = browserRecorderDeps,
    /** The longest clip; WAV capture keeps no more than this (plus a second). */
    private readonly maxSeconds = 301,
  ) {}

  /**
   * Asks for the microphone and starts recording; rejects when permission is refused, or with
   * RecordingCancelled when cancel() came while the browser was still asking.
   */
  async start(): Promise<void> {
    const stream = await this.deps.getUserMedia({ audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true } });
    if (this.cancelled) {
      // The microphone came after the recording was abandoned: let it go at once.
      stream.getTracks().forEach(t => t.stop());
      throw new RecordingCancelled();
    }
    this.stream = stream;
    this.context = this.deps.createAudioContext({ sampleRate: WAV_SAMPLE_RATE });
    const source = this.context.createMediaStreamSource(this.stream);
    this.source = source;
    this.analyser = this.context.createAnalyser();
    this.analyser.fftSize = 512;
    source.connect(this.analyser);
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

  /** Stops recording and discards it; a start() still waiting on the microphone gives it back. */
  cancel(): void {
    this.cancelled = true;
    this.release();
  }

  private release(): void {
    this.source?.disconnect();
    this.processor?.disconnect();
    this.stream?.getTracks().forEach(t => t.stop());
    void this.context?.close().catch(() => undefined);
    this.stream = this.context = this.analyser = this.processor = this.source = null;
    this.pcm = [];
    this.pcmSamples = 0;
  }
}
