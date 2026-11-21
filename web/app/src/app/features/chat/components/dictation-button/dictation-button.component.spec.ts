import { HttpErrorResponse } from '@angular/common/http';
import { provideZonelessChangeDetection, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { of, throwError } from 'rxjs';

import { DictationClip, DictationModel } from '../../../../core/models/speech.model';
import { SpeechService } from '../../../../core/services/speech.service';
import { ChatSendGate } from '../../services/chat-send-gate';
import { RecorderDeps } from './clip-recorder';
import { DictationButtonComponent } from './dictation-button.component';

/** A browser whose microphone and Web Audio are stand-ins. */
function fakeBrowser(opts: { deny?: boolean } = {}) {
  let now = 0;
  const track = { stop: vi.fn() };
  const node = { connect: vi.fn(), disconnect: vi.fn() };
  let grant: (() => void) | null = null;
  const stream = { getTracks: () => [track] } as unknown as MediaStream;
  const deps: RecorderDeps = {
    getUserMedia: vi.fn(() => {
      if (opts.deny) return Promise.reject(new DOMException('no', 'NotAllowedError'));
      return new Promise<MediaStream>(resolve => (grant = () => resolve(stream)));
    }),
    createAudioContext: vi.fn(
      () =>
        ({
          sampleRate: 16000,
          destination: {},
          createMediaStreamSource: () => node,
          createAnalyser: () => ({ ...node, fftSize: 0, getFloatTimeDomainData: () => undefined }),
          createScriptProcessor: () => ({ ...node, onaudioprocess: null }),
          close: () => Promise.resolve(),
        }) as unknown as AudioContext,
    ),
    now: () => now,
  };
  return {
    deps,
    track,
    advance: (ms: number) => (now += ms),
    /** Answers the permission prompt; until then the recorder is starting. */
    grant: () => grant?.(),
  };
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
    component.browserCanDictate = () => true;
    fixture.componentRef.setInput('chatId', 'chat-1');
    fixture.detectChanges();
    await fixture.whenStable();
    return browser;
  }

  const button = () => fixture.nativeElement.querySelector('[data-testid="dictation-button"]') as HTMLButtonElement | null;
  const MODEL: DictationModel = { available: true, model: 'gpt-transcribe', formats: ['wav'], max_seconds: 300 };

  /** Starts recording, granting the microphone. */
  async function record(browser: ReturnType<typeof fakeBrowser>) {
    const started = component.begin();
    await Promise.resolve();
    browser.grant();
    await started;
  }

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

    await record(browser);
    expect(component.state()).toBe('recording');
    expect(started).toBe(1);
    expect(browser.deps.createAudioContext).toHaveBeenCalledWith({ sampleRate: 16000 });
    browser.advance(4200);
    await component.finish();

    expect(component.state()).toBe('idle');
    expect(texts).toEqual(['Pick up milk']);
    const [clip, chatId] = speech.transcribe.mock.calls[0] as [DictationClip, string];
    expect(clip.format).toBe('wav');
    expect(clip.audio.type).toBe('audio/wav');
    expect(clip.seconds).toBeCloseTo(4.2);
    expect(chatId).toBe('chat-1');
    expect(browser.track.stop).toHaveBeenCalled();
  });

  it('discards on Escape, sending nothing', async () => {
    const browser = await render(MODEL);
    await record(browser);
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    expect(component.state()).toBe('idle');
    expect(speech.transcribe).not.toHaveBeenCalled();
    expect(browser.track.stop).toHaveBeenCalled();
  });

  it('leaves an Escape something else already took', async () => {
    const browser = await render(MODEL);
    await record(browser);
    const event = new KeyboardEvent('keydown', { key: 'Escape', cancelable: true });
    event.preventDefault();
    document.dispatchEvent(event);
    expect(component.state()).toBe('recording');
    component.discard();
  });

  it('gives the microphone back when discarded while the browser is still asking', async () => {
    const browser = await render(MODEL);
    const started = component.begin();
    expect(component.state()).toBe('starting');
    fixture.destroy(); // the composer went away
    browser.grant();
    await started;
    expect(browser.track.stop).toHaveBeenCalled();
    expect(browser.deps.createAudioContext).not.toHaveBeenCalled();
    expect(component.state()).toBe('idle');
  });

  it('drops a transcript that comes back after the chat changed', async () => {
    const browser = await render(MODEL);
    const texts: string[] = [];
    component.transcribed.subscribe(t => texts.push(t));
    await record(browser);
    browser.advance(2000);
    fixture.componentRef.setInput('chatId', 'chat-2');
    await component.finish();
    expect(speech.transcribe.mock.calls[0][1]).toBe('chat-1');
    expect(texts).toEqual([]);
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
    await record(browser);
    browser.advance(2000);
    await component.finish();
    expect(gate.handleSendError).toHaveBeenCalled();
    expect(component.error()).toContain('out of credits');
  });

  it('is disabled while the send gate blocks, but a recording can always be stopped', async () => {
    const browser = await render(MODEL, fakeBrowser(), true);
    expect(component.blocked()).toBe(true);
    expect(button()!.disabled).toBe(true);

    await record(browser);
    fixture.detectChanges();
    expect(button()!.disabled).toBe(false);
    component.discard();
  });

  it('stops at the longest clip the model takes', async () => {
    const browser = await render({ ...MODEL, max_seconds: 30 });
    await record(browser);
    browser.advance(30_500);
    (component as unknown as { tick(): void }).tick();
    await fixture.whenStable();
    await vi.waitFor(() => expect(speech.transcribe).toHaveBeenCalled());
  });
});
