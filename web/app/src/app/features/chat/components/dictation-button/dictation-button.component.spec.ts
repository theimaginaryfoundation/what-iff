import { HttpErrorResponse } from '@angular/common/http';
import { provideZonelessChangeDetection, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { of, throwError } from 'rxjs';

import { DictationClip, DictationModel } from '../../../../core/models/speech.model';
import { SpeechService } from '../../../../core/services/speech.service';
import { ChatSendGate } from '../../services/chat-send-gate';
import { RecorderDeps } from './clip-recorder';
import { DictationButtonComponent } from './dictation-button.component';

/** A browser whose microphone, Web Audio and MediaRecorder are stand-ins. */
function fakeBrowser(opts: { deny?: boolean } = {}) {
  let now = 0;
  const recorder = {
    state: 'inactive',
    ondataavailable: null as ((e: { data: Blob }) => void) | null,
    onstop: null as (() => void) | null,
    start: vi.fn(function (this: typeof recorder) {
      this.state = 'recording';
    }),
    stop: vi.fn(function (this: typeof recorder) {
      this.state = 'inactive';
      this.ondataavailable?.({ data: new Blob(['opus-bytes']) });
      this.onstop?.();
    }),
  };
  const track = { stop: vi.fn() };
  const node = { connect: vi.fn(), disconnect: vi.fn() };
  const deps: RecorderDeps = {
    getUserMedia: vi.fn(() =>
      opts.deny
        ? Promise.reject(new DOMException('no', 'NotAllowedError'))
        : Promise.resolve({ getTracks: () => [track] } as unknown as MediaStream),
    ),
    createAudioContext: vi.fn(
      () =>
        ({
          sampleRate: 48000,
          destination: {},
          createMediaStreamSource: () => node,
          createAnalyser: () => ({ ...node, fftSize: 0, getFloatTimeDomainData: () => undefined }),
          close: () => Promise.resolve(),
        }) as unknown as AudioContext,
    ),
    createMediaRecorder: vi.fn(() => recorder as unknown as MediaRecorder),
    now: () => now,
  };
  return { deps, recorder, track, advance: (ms: number) => (now += ms) };
}

describe('DictationButtonComponent', () => {
  let fixture: ComponentFixture<DictationButtonComponent>;
  let component: DictationButtonComponent;
  let speech: {
    dictation: ReturnType<typeof signal<DictationModel | null>>;
    loadDictation: ReturnType<typeof vi.fn>;
    transcribe: ReturnType<typeof vi.fn>;
  };
  let gate: { blocked: ReturnType<typeof vi.fn>; handleSendError: ReturnType<typeof vi.fn> };

  async function render(model: DictationModel | null, browser = fakeBrowser(), blocked = false) {
    speech = {
      dictation: signal(model),
      loadDictation: vi.fn(() => Promise.resolve(model)),
      transcribe: vi.fn(() => of('Pick up milk')),
    };
    gate = { blocked: vi.fn(() => blocked), handleSendError: vi.fn() };
    await TestBed.configureTestingModule({
      imports: [DictationButtonComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: SpeechService, useValue: speech },
        { provide: ChatSendGate, useValue: gate },
      ],
    }).compileComponents();
    fixture = TestBed.createComponent(DictationButtonComponent);
    component = fixture.componentInstance;
    component.deps = browser.deps;
    component.canRecord = t => t.startsWith('audio/webm');
    component.browserCanDictate = () => true;
    fixture.componentRef.setInput('chatId', 'chat-1');
    fixture.detectChanges();
    await fixture.whenStable();
    return browser;
  }

  const button = () => fixture.nativeElement.querySelector('[data-testid="dictation-button"]') as HTMLButtonElement | null;
  const MODEL: DictationModel = { available: true, model: 'gpt-transcribe', formats: ['webm', 'wav'], max_seconds: 300 };

  it('shows no microphone without a dictation model', async () => {
    await render({ available: false, code: 'utility_model_not_set' });
    expect(button()).toBeNull();
    expect(speech.loadDictation).toHaveBeenCalled();
  });

  it('records, transcribes, and hands the text to the composer without sending', async () => {
    const browser = await render(MODEL);
    const texts: string[] = [];
    component.transcribed.subscribe(t => texts.push(t));
    let started = 0;
    component.recordingStarted.subscribe(() => started++);

    await component.begin();
    expect(component.state()).toBe('recording');
    expect(started).toBe(1);
    expect(browser.deps.createMediaRecorder).toHaveBeenCalledWith(expect.anything(), 'audio/webm;codecs=opus');
    browser.advance(4200);
    await component.finish();

    expect(component.state()).toBe('idle');
    expect(texts).toEqual(['Pick up milk']);
    const [clip, chatId] = speech.transcribe.mock.calls[0] as [DictationClip, string];
    expect(clip.format).toBe('webm');
    expect(clip.seconds).toBeCloseTo(4.2);
    expect(chatId).toBe('chat-1');
    expect(browser.track.stop).toHaveBeenCalled();
  });

  it('records WAV for a model that takes none of the browser formats', async () => {
    const browser = await render({ ...MODEL, formats: ['wav', 'mp3'] });
    await component.begin();
    expect(browser.deps.createMediaRecorder).not.toHaveBeenCalled();
    expect(browser.deps.createAudioContext).toHaveBeenCalledWith({ sampleRate: 16000 });
    component.discard();
  });

  it('discards on Escape, sending nothing', async () => {
    await render(MODEL);
    await component.begin();
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    expect(component.state()).toBe('idle');
    expect(speech.transcribe).not.toHaveBeenCalled();
  });

  it('says to allow the microphone when permission is refused', async () => {
    await render(MODEL, fakeBrowser({ deny: true }));
    await component.begin();
    expect(component.state()).toBe('idle');
    expect(component.error()).toContain('Allow the microphone');
  });

  it('hands an out-of-credits refusal to the send gate', async () => {
    const browser = await render(MODEL);
    speech.transcribe.mockReturnValue(throwError(() => new HttpErrorResponse({ status: 429, error: { code: 'quota_exceeded' } })));
    await component.begin();
    browser.advance(2000);
    await component.finish();
    expect(gate.handleSendError).toHaveBeenCalled();
    expect(component.error()).toContain('out of credits');
  });

  it('is disabled while the send gate blocks', async () => {
    await render(MODEL, fakeBrowser(), true);
    expect(component.blocked()).toBe(true);
    expect(button()!.disabled).toBe(true);
  });

  it('stops at the longest clip the model takes', async () => {
    const browser = await render({ ...MODEL, max_seconds: 30 });
    await component.begin();
    browser.advance(30_500);
    (component as unknown as { tick(): void }).tick();
    await fixture.whenStable();
    await vi.waitFor(() => expect(speech.transcribe).toHaveBeenCalled());
  });
});
