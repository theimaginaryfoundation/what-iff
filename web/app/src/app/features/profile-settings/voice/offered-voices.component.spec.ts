import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { of } from 'rxjs';

import { Model } from '../../../core/models/model.model';
import { ModelService } from '../../../core/services/model.service';
import { ProviderConnectionsService } from '../../../core/services/provider-connections.service';
import { OfferedVoicesComponent, voiceRows } from './offered-voices.component';

const KNOWN: Model = {
  id: 'g',
  name: 'gemini-3.8-flash-tts',
  display_name: 'Gemini TTS',
  description: '',
  tool_support: false,
  kind: 'speech',
  connection_id: 'c1',
  speech_voices: [{ id: 'Kore', name: 'Kore (warm)' }],
  available_voices: [
    { id: 'Kore', name: 'Kore' },
    { id: 'Puck', name: 'Puck' },
  ],
};
const TYPED: Model = {
  id: 'k',
  name: 'kokoro',
  display_name: 'Kokoro',
  description: '',
  tool_support: false,
  kind: 'speech',
  connection_id: 'c2',
  speech_voices: [{ id: 'af_bella', name: 'af_bella' }],
};
const CATALOG: Model = { ...KNOWN, id: 'cat', connection_id: undefined };

describe('voiceRows', () => {
  it('lists every voice, ticking and naming the offered ones', () => {
    expect(voiceRows(KNOWN)).toEqual([
      { id: 'Kore', name: 'Kore (warm)', offered: true },
      { id: 'Puck', name: 'Puck', offered: false },
    ]);
    expect(voiceRows(TYPED)).toEqual([{ id: 'af_bella', name: 'af_bella', offered: true }]);
  });
});

describe('OfferedVoicesComponent', () => {
  it("lists the account's own voice models only, and saves narrowed and renamed voices", async () => {
    const setModels = vi.fn(() => of([]));
    TestBed.configureTestingModule({
      imports: [OfferedVoicesComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: ModelService, useValue: { getAudioModels: () => of([KNOWN, TYPED, CATALOG]) } },
        { provide: ProviderConnectionsService, useValue: { setModels } },
      ],
    });
    const fixture = TestBed.createComponent(OfferedVoicesComponent);
    const c = fixture.componentInstance;
    await c.ngOnInit();
    expect(c.rows().map(r => r.model.id)).toEqual(['g', 'k']);
    expect(c.rows()[1].typed).toBe(true);

    const gemini = c.rows()[0];
    c.toggle(gemini, gemini.voices[1], true);
    c.rename(c.rows()[0], c.rows()[0].voices[1], '  ');
    await c.save(c.rows()[0]);
    expect(setModels).toHaveBeenCalledWith('c1', [
      {
        id: 'gemini-3.8-flash-tts',
        enabled: true,
        display_name: 'Gemini TTS',
        voices: [
          { id: 'Kore', name: 'Kore (warm)' },
          { id: 'Puck', name: 'Puck' },
        ],
      },
    ]);
    expect(c.rows()[0].dirty).toBe(false);

    const kokoro = c.rows()[1];
    c.addVoice(kokoro, ' am_adam ');
    c.toggle(c.rows()[1], c.rows()[1].voices[0], false);
    c.toggle(c.rows()[1], c.rows()[1].voices[1], false);
    await c.save(c.rows()[1]);
    expect(c.error()).toContain('at least one');
  });
});
