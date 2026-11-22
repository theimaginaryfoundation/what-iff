import { HttpErrorResponse } from '@angular/common/http';
import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { of, throwError } from 'rxjs';

import { SpeechTrack } from '../models/speech.model';
import { SpeechPlayerService } from './speech-player.service';
import { SpeechService } from './speech.service';

/** An AudioContext whose clock the test moves, recording what was scheduled when. */
function fakeContext() {
  const started: { at: number; duration: number }[] = [];
  const ctx = {
    state: 'running',
    currentTime: 10,
    destination: {},
    resume: vi.fn(),
    decodeAudioData: vi.fn((data: ArrayBuffer) => Promise.resolve({ duration: data.byteLength } as AudioBuffer)),
    createBufferSource: () => {
      const src = {
        buffer: null as AudioBuffer | null,
        connect: vi.fn(),
        stop: vi.fn(),
        start: (at: number) => started.push({ at, duration: src.buffer!.duration }),
      };
      return src;
    },
  };
  return { ctx, started };
}

function track(status: SpeechTrack['status'], segments: number, ids = 'abcdefgh'): SpeechTrack {
  return {
    id: 't1',
    chat_id: 'c1',
    status,
    segments: Array.from({ length: segments }, (_, seq) => ({ id: ids[seq], seq, format: 'mp3' as const, duration_ms: 0, size_bytes: 2 })),
  };
}

describe('SpeechPlayerService', () => {
  let player: SpeechPlayerService;
  let speech: { listen: ReturnType<typeof vi.fn>; track: ReturnType<typeof vi.fn>; segment: ReturnType<typeof vi.fn> };
  let audio: ReturnType<typeof fakeContext>;

  beforeEach(() => {
    speech = { listen: vi.fn(), track: vi.fn(), segment: vi.fn((_: string, seq: number) => of(new ArrayBuffer(seq + 2))) };
    TestBed.configureTestingModule({ providers: [provideZonelessChangeDetection(), { provide: SpeechService, useValue: speech }] });
    player = TestBed.inject(SpeechPlayerService);
    audio = fakeContext();
    player.createContext = () => audio.ctx as unknown as AudioContext;
    player.wait = () => Promise.resolve();
  });

  it('schedules segments back to back as they arrive, polling while the track is open', async () => {
    speech.listen.mockReturnValue(of(track('open', 1)));
    speech.track.mockReturnValueOnce(of(track('open', 1))).mockReturnValueOnce(of(track('complete', 3)));
    await player.play('m1');
    expect(audio.started).toEqual([
      { at: 10.05, duration: 2 },
      { at: 12.05, duration: 3 },
      { at: 15.05, duration: 4 },
    ]);
    expect(speech.track).toHaveBeenCalledTimes(2);
    expect(player.playing()).toBeNull();
    expect(player.error()).toBeNull();
  });

  it('gives up on a track that stops getting segments, rather than polling forever', async () => {
    speech.listen.mockReturnValue(of(track('open', 0)));
    speech.track.mockReturnValue(of(track('open', 0)));
    await player.play('m1');
    expect(player.error()).toEqual({ messageId: 'm1', text: 'Reading aloud stopped responding. Try again.' });
    expect(speech.track).toHaveBeenCalledTimes(200);
    expect(player.loading()).toBeNull();
  });

  it('says why a track stopped partway', async () => {
    speech.listen.mockReturnValue(of({ ...track('stopped', 1), error_code: 'quota_exceeded' }));
    await player.play('m1');
    expect(player.error()).toEqual({ messageId: 'm1', text: "You're out of credits for voice replies." });
  });

  it('a new reply stops the one playing', async () => {
    speech.listen.mockReturnValue(of(track('open', 1)));
    let resolveTrack!: (t: SpeechTrack) => void;
    speech.track.mockReturnValue({
      subscribe: (o: { next: (t: SpeechTrack) => void; complete: () => void }) => {
        resolveTrack = t => {
          o.next(t);
          o.complete();
        };
        return { unsubscribe() {} };
      },
    });
    const first = player.play('m1');
    await vi.waitFor(() => expect(resolveTrack).toBeDefined());
    player.stop();
    resolveTrack(track('complete', 2));
    await first;
    expect(audio.started).toHaveLength(1);
    expect(player.playing()).toBeNull();
  });

  it('follows a claimed track, and drops segments the server withdrew before they start', async () => {
    const stops: number[] = [];
    const base = audio.ctx.createBufferSource;
    audio.ctx.createBufferSource = () => {
      const src = base();
      const stop = src.stop;
      src.stop = vi.fn(() => {
        stops.push(audio.started.length);
        stop();
      });
      return src;
    };
    // Two segments of a round arrive; the round is withdrawn, and the final round replaces them.
    speech.track
      .mockReturnValueOnce(of(track('open', 2, 'ab')))
      .mockReturnValueOnce(of({ ...track('complete', 2, 'xy'), message_id: 'm9' }));
    await player.playTrack(track('open', 1, 'a'));
    // Neither withdrawn segment had started on this clock, so both are stopped, and the final
    // round's segments take their place from now.
    expect(audio.started.map(s => s.at)).toEqual([10.05, 12.05, 10, 12]);
    expect(stops).toHaveLength(2);
    expect(speech.segment).toHaveBeenCalledTimes(4);
  });

  it('reads the track again when a segment it was told of has been withdrawn', async () => {
    speech.segment
      .mockReturnValueOnce(throwError(() => new HttpErrorResponse({ status: 404 })))
      .mockImplementation((_: string, seq: number) => of(new ArrayBuffer(seq + 2)));
    speech.track.mockReturnValueOnce(of(track('complete', 1, 'x')));
    await player.playTrack(track('open', 1, 'a'));
    expect(player.error()).toBeNull();
    expect(audio.started).toHaveLength(1);
  });

  it('is quiet about a claimed track that is gone (its turn failed)', async () => {
    speech.segment.mockReturnValue(throwError(() => new HttpErrorResponse({ status: 404 })));
    speech.track.mockReturnValue(throwError(() => new HttpErrorResponse({ status: 404 })));
    await player.playTrack(track('open', 1, 'a'));
    expect(player.error()).toBeNull();
  });

  it("knows which chats' personalities have a voice", () => {
    expect(player.chatHasVoice('c1')).toBe(false);
    player.setChatVoice('c1', true);
    expect(player.chatHasVoice('c1')).toBe(true);
    expect(player.chatHasVoice(null)).toBe(false);
  });
});
