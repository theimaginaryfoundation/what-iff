import { DOCUMENT } from '@angular/common';
import { provideZonelessChangeDetection, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { of, throwError } from 'rxjs';

import { SpeechTrack } from '../models/speech.model';
import { SpeechAutoplayService } from './speech-autoplay.service';
import { SpeechPlayerService } from './speech-player.service';
import { SpeechService } from './speech.service';

function pendingTrack(id: string): SpeechTrack {
  return { id, chat_id: 'c1', status: 'open', autoplay: 'pending', segments: [] };
}

describe('SpeechAutoplayService', () => {
  let autoplay: SpeechAutoplayService;
  let speech: {
    claim: ReturnType<typeof vi.fn>;
    skip: ReturnType<typeof vi.fn>;
    pending: ReturnType<typeof vi.fn>;
    setChatAutoplay: ReturnType<typeof vi.fn>;
  };
  let player: {
    unlocked: ReturnType<typeof signal<boolean>>;
    recording: ReturnType<typeof signal<boolean>>;
    playing: ReturnType<typeof signal<string | null>>;
    loading: ReturnType<typeof signal<string | null>>;
    unlock: ReturnType<typeof vi.fn>;
    soundReady: () => boolean;
    playTrack: ReturnType<typeof vi.fn>;
  };
  let visibility: DocumentVisibilityState;

  beforeEach(() => {
    visibility = 'visible';
    speech = {
      claim: vi.fn((id: string) => of({ claimed: true, track: pendingTrack(id) })),
      skip: vi.fn(() => of(undefined)),
      pending: vi.fn(() => of([])),
      setChatAutoplay: vi.fn(() => of({})),
    };
    player = {
      unlocked: signal(true),
      recording: signal(false),
      playing: signal<string | null>(null),
      loading: signal<string | null>(null),
      unlock: vi.fn(() => player.unlocked.set(true)),
      soundReady: () => player.unlocked(),
      playTrack: vi.fn(() => Promise.resolve()),
    };
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => visibility });
    TestBed.configureTestingModule({
      providers: [
        provideZonelessChangeDetection(),
        { provide: SpeechService, useValue: speech },
        { provide: SpeechPlayerService, useValue: player },
        { provide: DOCUMENT, useValue: document },
      ],
    });
    autoplay = TestBed.inject(SpeechAutoplayService);
  });

  it("claims and plays the watched turn's reply, once", async () => {
    autoplay.setOpenChat('c1');
    autoplay.onTurnTrack('c1', 't1');
    autoplay.onTurnTrack('c1', 't1');
    await vi.waitFor(() => expect(player.playTrack).toHaveBeenCalledTimes(1));
    expect(speech.claim).toHaveBeenCalledTimes(1);
  });

  it("plays nothing a client didn't get", async () => {
    speech.claim.mockReturnValue(of({ claimed: false, track: pendingTrack('t1') }));
    autoplay.setOpenChat('c1');
    autoplay.onTurnTrack('c1', 't1');
    await vi.waitFor(() => expect(speech.claim).toHaveBeenCalled());
    expect(player.playTrack).not.toHaveBeenCalled();
  });

  it('waits for a tap before any sound is unlocked', async () => {
    player.unlocked.set(false);
    autoplay.setOpenChat('c1');
    autoplay.onTurnTrack('c1', 't1');
    expect(speech.claim).not.toHaveBeenCalled();
    expect(autoplay.tapToListen()).toEqual({ trackId: 't1', chatId: 'c1' });
    await autoplay.tapToPlay();
    expect(player.unlock).toHaveBeenCalled();
    expect(player.playTrack).toHaveBeenCalled();
  });

  it('leaves a reply that arrived while away pending, and plays it on return', async () => {
    autoplay.setOpenChat('c1');
    visibility = 'hidden';
    autoplay.onTurnTrack('c1', 't1');
    expect(speech.claim).not.toHaveBeenCalled();

    visibility = 'visible';
    speech.pending.mockReturnValue(of([pendingTrack('t1')]));
    document.dispatchEvent(new Event('visibilitychange'));
    await vi.waitFor(() => expect(player.playTrack).toHaveBeenCalled());
  });

  it('plays the newest pending reply when a chat opens, and skips the rest', async () => {
    speech.pending.mockReturnValue(of([pendingTrack('new'), pendingTrack('old')]));
    autoplay.setOpenChat('c1');
    await vi.waitFor(() => expect(speech.claim).toHaveBeenCalledWith('new'));
    expect(speech.skip).toHaveBeenCalledWith('old');
    expect(speech.claim).not.toHaveBeenCalledWith('old');
  });

  it("doesn't start while dictation records or another reply plays", () => {
    autoplay.setOpenChat('c1');
    player.recording.set(true);
    autoplay.onTurnTrack('c1', 't1');
    player.recording.set(false);
    player.playing.set('m0');
    autoplay.onTurnTrack('c1', 't2');
    expect(speech.claim).not.toHaveBeenCalled();
  });

  it('works out the chat setting, and puts it back when saving fails', async () => {
    autoplay.setChatAutoplay('c1', { personality: false, override: null });
    expect(autoplay.effective('c1')).toBe(false);
    await autoplay.setOverride('c1', true);
    expect(autoplay.effective('c1')).toBe(true);
    expect(speech.setChatAutoplay).toHaveBeenCalledWith('c1', true);

    speech.setChatAutoplay.mockReturnValue(throwError(() => new Error('no')));
    await expect(autoplay.setOverride('c1', null)).rejects.toThrow();
    expect(autoplay.chatAutoplay('c1')).toEqual({ personality: false, override: true });
  });
});
