import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { of, throwError } from 'rxjs';

import { Model } from '../../../core/models/model.model';
import { Personality } from '../../../core/models/personality.model';
import { ModelService } from '../../../core/services/model.service';
import { SpeechPlayerService } from '../../../core/services/speech-player.service';
import { SpeechService } from '../../../core/services/speech.service';
import { PersonalityVoiceComponent, orderVoiceModels } from './personality-voice.component';

const GEMINI: Model = {
  id: 'g',
  name: 'gemini-3.8-flash-tts',
  display_name: 'Gemini 3.8 Flash TTS',
  description: '',
  tool_support: false,
  kind: 'speech',
  subscription_tier: 'high',
  speech_style: true,
  speech_voices: [
    { id: 'Kore', name: 'Kore' },
    { id: 'Puck', name: 'Puck' },
  ],
};
const QWEN: Model = {
  id: 'q',
  name: 'qwen-audio-3.1-tts-flash',
  display_name: 'Qwen Audio 3.1',
  description: '',
  tool_support: false,
  kind: 'speech',
  subscription_tier: 'low',
  speech_style: true,
  speech_voices: [{ id: 'Emily_v3.1', name: 'Emily' }],
};
const LOCAL: Model = {
  id: 'k',
  name: 'kokoro',
  display_name: 'Kokoro',
  description: '',
  tool_support: false,
  kind: 'speech',
  speech_voices: [{ id: 'af_bella', name: 'Bella' }],
};

describe('orderVoiceModels', () => {
  it('puts the lowest grade first, ungraded last, then by name', () => {
    expect(orderVoiceModels([LOCAL, GEMINI, QWEN]).map(m => m.id)).toEqual(['q', 'g', 'k']);
  });
});

describe('PersonalityVoiceComponent', () => {
  let fixture: ComponentFixture<PersonalityVoiceComponent>;
  let component: PersonalityVoiceComponent;
  let speech: { setVoice: ReturnType<typeof vi.fn>; clearVoice: ReturnType<typeof vi.fn>; preview: ReturnType<typeof vi.fn> };
  let player: { playAudio: ReturnType<typeof vi.fn>; stop: ReturnType<typeof vi.fn> };
  const ADA = { id: 'p1', name: 'Ada', voice: null } as unknown as Personality;

  async function render(personality: Personality, models: Model[] = [GEMINI, QWEN]) {
    speech = {
      setVoice: vi.fn((_: string, v) => of({ ...personality, voice: v })),
      clearVoice: vi.fn(() => of({ ...personality, voice: null })),
      preview: vi.fn(() => of(new ArrayBuffer(4))),
    };
    player = { playAudio: vi.fn(() => Promise.resolve()), stop: vi.fn() };
    await TestBed.configureTestingModule({
      imports: [PersonalityVoiceComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: ModelService, useValue: { getAudioModels: vi.fn(() => of(models)) } },
        { provide: SpeechService, useValue: speech },
        { provide: SpeechPlayerService, useValue: player },
      ],
    }).compileComponents();
    fixture = TestBed.createComponent(PersonalityVoiceComponent);
    fixture.componentRef.setInput('personality', personality);
    component = fixture.componentInstance;
    fixture.detectChanges();
    await new Promise(r => setTimeout(r));
    fixture.detectChanges();
  }

  it('starts a new voice on the cheapest model and its first voice, and saves it', async () => {
    await render(ADA);
    component.edit();
    expect(component.modelId()).toBe('q');
    expect(component.voiceId()).toBe('Emily_v3.1');
    expect(component.sample()).toContain("Hi, I'm Ada.");
    component.style.set('  warm  ');
    const saved: Personality[] = [];
    component.changed.subscribe(p => saved.push(p));
    await component.save();
    expect(speech.setVoice).toHaveBeenCalledWith('p1', { model_id: 'q', voice: 'Emily_v3.1', style: 'warm' });
    expect(saved[0].voice).toEqual({ model_id: 'q', voice: 'Emily_v3.1', style: 'warm' });
    expect(component.editing()).toBe(false);
  });

  it("keeps the voice when the model has it, else takes the new one's first", async () => {
    await render(ADA);
    component.edit();
    component.pickModel('g');
    expect(component.voiceId()).toBe('Kore');
  });

  it('previews the unsaved voice', async () => {
    await render(ADA);
    component.edit();
    component.sample.set('Testing, one two.');
    await component.preview();
    expect(speech.preview).toHaveBeenCalledWith({ model_id: 'q', voice: 'Emily_v3.1', style: '' }, 'Testing, one two.');
    expect(player.playAudio).toHaveBeenCalled();
  });

  it('sends no style to a model that takes none', async () => {
    await render(ADA, [LOCAL]);
    component.edit();
    component.style.set('ignored');
    await component.save();
    expect(speech.setVoice).toHaveBeenCalledWith('p1', { model_id: 'k', voice: 'af_bella', style: '' });
  });

  it('says when the saved voice is no longer available', async () => {
    await render({ ...ADA, voice: { model_id: 'gone', voice: 'x', style: '' } } as Personality);
    expect(component.unusable()).toBe(true);
    component.edit();
    expect(component.modelId()).toBe('q'); // an unusable voice opens on the default
  });

  it('removes the voice', async () => {
    await render({ ...ADA, voice: { model_id: 'q', voice: 'Emily_v3.1', style: '' } } as Personality);
    await component.remove();
    expect(speech.clearVoice).toHaveBeenCalledWith('p1');
  });

  it('keeps the fields open and says why when saving fails', async () => {
    await render(ADA);
    speech.setVoice.mockReturnValue(throwError(() => new Error('no')));
    component.edit();
    await component.save();
    expect(component.editing()).toBe(true);
    expect(component.error()).not.toBe('');
  });
});
