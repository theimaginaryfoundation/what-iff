import { WAV_SAMPLE_RATE, encodeWav, planRecording, resample } from './clip-recorder';

describe('planRecording', () => {
  const chromium = (t: string) => t.startsWith('audio/webm');
  const safari = (t: string) => t === 'audio/mp4';

  it("records in the browser's own format when the model takes it", () => {
    expect(planRecording(['webm', 'mp4', 'wav'], chromium)).toEqual({ kind: 'media', mimeType: 'audio/webm;codecs=opus', format: 'webm' });
    expect(planRecording(['webm', 'mp4', 'wav'], safari)).toEqual({ kind: 'media', mimeType: 'audio/mp4', format: 'mp4' });
  });

  it("captures WAV for a model that takes none of the browser's formats", () => {
    expect(planRecording(['wav', 'mp3'], chromium)).toEqual({ kind: 'wav' });
    expect(planRecording([], chromium)).toEqual({ kind: 'wav' });
    expect(planRecording(['webm'], () => false)).toEqual({ kind: 'wav' }); // no MediaRecorder: WAV is all there is
  });
});

describe('resample', () => {
  it('is a no-op at the same rate', () => {
    const s = new Float32Array([0.1, 0.2]);
    expect(resample(s, 16000, 16000)).toBe(s);
  });

  it('downsamples 48 kHz to 16 kHz to a third the length, interpolating', () => {
    const s = new Float32Array(48).map((_, i) => i / 48);
    const out = resample(s, 48000, WAV_SAMPLE_RATE);
    expect(out.length).toBe(16);
    expect(out[1]).toBeCloseTo(3 / 48, 5);
  });
});

describe('encodeWav', () => {
  it('writes a 16-bit mono PCM header and clamps samples', () => {
    const buf = encodeWav(new Float32Array([0, 1, -1, 2]), 16000);
    const view = new DataView(buf);
    const text = (o: number, n: number) => String.fromCharCode(...new Uint8Array(buf, o, n));
    expect(text(0, 4)).toBe('RIFF');
    expect(text(8, 4)).toBe('WAVE');
    expect(view.getUint16(22, true)).toBe(1);
    expect(view.getUint32(24, true)).toBe(16000);
    expect(view.getUint32(28, true)).toBe(32000);
    expect(view.getUint32(40, true)).toBe(8);
    expect(view.getInt16(46, true)).toBe(0x7fff);
    expect(view.getInt16(48, true)).toBe(-0x8000);
    expect(view.getInt16(50, true)).toBe(0x7fff); // clamped
  });
});
