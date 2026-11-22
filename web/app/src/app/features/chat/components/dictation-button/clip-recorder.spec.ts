import { ClipRecorder, WAV_SAMPLE_RATE, encodeWav, resample } from './clip-recorder';

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

describe('ClipRecorder', () => {
  /** A context at sampleRate whose source node may refuse the stream, as Firefox has at 16 kHz. */
  function fakeContext(sampleRate: number, refuse: boolean) {
    let onaudioprocess: ((e: { inputBuffer: { getChannelData(): Float32Array } }) => void) | null = null;
    const node = () => ({ connect: vi.fn(), disconnect: vi.fn() });
    const context = {
      sampleRate,
      destination: {},
      close: vi.fn(() => Promise.resolve()),
      createMediaStreamSource: vi.fn(() => {
        if (refuse) throw new DOMException('different sample-rate', 'NotSupportedError');
        return node();
      }),
      createAnalyser: vi.fn(() => ({ ...node(), fftSize: 0 })),
      createScriptProcessor: vi.fn(() => {
        const p = node();
        Object.defineProperty(p, 'onaudioprocess', { set: f => (onaudioprocess = f) });
        return p;
      }),
      feed: (samples: Float32Array) => onaudioprocess?.({ inputBuffer: { getChannelData: () => samples } }),
    };
    return context;
  }

  it('records at the device rate when the browser refuses a 16 kHz context, and resamples', async () => {
    const refused = fakeContext(WAV_SAMPLE_RATE, true);
    const native = fakeContext(48000, false);
    const createAudioContext = vi.fn(
      (options?: AudioContextOptions) => (options?.sampleRate ? refused : native) as unknown as AudioContext,
    );
    const stream = { getTracks: () => [{ stop: vi.fn() }] } as unknown as MediaStream;
    const recorder = new ClipRecorder({ getUserMedia: () => Promise.resolve(stream), createAudioContext, now: () => 0 });

    await recorder.start();
    expect(refused.close).toHaveBeenCalled();
    expect(createAudioContext).toHaveBeenLastCalledWith();
    native.feed(new Float32Array(4800));
    const clip = await recorder.stop();

    const header = new DataView(await clip.audio.arrayBuffer());
    expect(header.getUint32(24, true)).toBe(WAV_SAMPLE_RATE);
    expect(header.getUint32(40, true)).toBe(1600 * 2); // 0.1 s at 16 kHz
  });
});
