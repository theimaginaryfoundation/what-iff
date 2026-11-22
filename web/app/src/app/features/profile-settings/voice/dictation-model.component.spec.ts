import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { of, throwError } from 'rxjs';

import { Model } from '../../../core/models/model.model';
import { UtilityModels } from '../../../core/models/utility-model.model';
import { ModelService } from '../../../core/services/model.service';
import { SpeechService } from '../../../core/services/speech.service';
import { UtilityModelsService } from '../../../core/services/utility-models.service';
import { DictationModelComponent } from './dictation-model.component';

const EARS: Model = {
  id: 'm-ears',
  name: 'gpt-transcribe',
  display_name: 'GPT Transcribe',
  description: '',
  tool_support: false,
  kind: 'transcription',
};

function utility(account: UtilityModels['roles'][number]['account'], accountsChoose = true): UtilityModels {
  return { accounts_choose: accountsChoose, roles: [], speech_roles: [{ role: 'transcription', account, deployment: null }] };
}

describe('DictationModelComponent', () => {
  let fixture: ComponentFixture<DictationModelComponent>;
  let utilityModels: { get: ReturnType<typeof vi.fn>; choose: ReturnType<typeof vi.fn>; switchOff: ReturnType<typeof vi.fn> };
  let speech: { loadDictation: ReturnType<typeof vi.fn> };

  async function render(initial: UtilityModels, models: Model[] = [EARS]) {
    utilityModels = {
      get: vi.fn(() => of(initial)),
      choose: vi.fn(() => of(utility({ off: false, model_id: 'm-ears', reasoning_effort: null, service_tier: null }))),
      switchOff: vi.fn(() => of(utility({ off: true, model_id: null, reasoning_effort: null, service_tier: null }))),
    };
    speech = { loadDictation: vi.fn(() => Promise.resolve({ available: true })) };
    await TestBed.configureTestingModule({
      imports: [DictationModelComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: UtilityModelsService, useValue: utilityModels },
        { provide: ModelService, useValue: { getAudioModels: vi.fn(() => of(models)) } },
        { provide: SpeechService, useValue: speech },
      ],
    }).compileComponents();
    fixture = TestBed.createComponent(DictationModelComponent);
    fixture.detectChanges();
    await new Promise(resolve => setTimeout(resolve));
    fixture.detectChanges();
  }

  const select = () => fixture.nativeElement.querySelector('[data-testid="dictation-model-select"]') as HTMLSelectElement | null;

  it('is Off until a dictation model is chosen, and choosing one turns the microphone on', async () => {
    await render(utility(null));
    expect(select()!.value).toBe('off');
    select()!.value = 'm-ears';
    await fixture.componentInstance.onChange(select()!);
    expect(utilityModels.choose).toHaveBeenCalledWith('transcription', { model_id: 'm-ears' });
    expect(speech.loadDictation).toHaveBeenCalledWith(true);
  });

  it('switches dictation off', async () => {
    await render(utility({ off: false, model_id: 'm-ears', reasoning_effort: null, service_tier: null }));
    expect(select()!.value).toBe('m-ears');
    select()!.value = 'off';
    await fixture.componentInstance.onChange(select()!);
    expect(utilityModels.switchOff).toHaveBeenCalledWith('transcription');
  });

  it('says where to enable a dictation model when the account has none', async () => {
    await render(utility(null), []);
    expect(select()).toBeNull();
    expect(fixture.nativeElement.querySelector('[data-testid="dictation-none"]')).not.toBeNull();
  });

  it("isn't shown where the deployment chooses", async () => {
    await render(utility(null, false));
    expect(fixture.nativeElement.querySelector('[data-testid="dictation-model"]')).toBeNull();
  });

  it('keeps the choice and says so when saving fails', async () => {
    await render(utility(null));
    utilityModels.choose.mockReturnValue(throwError(() => new Error('nope')));
    select()!.value = 'm-ears';
    await fixture.componentInstance.onChange(select()!);
    fixture.detectChanges();
    expect(fixture.componentInstance.error()).not.toBe('');
    expect(select()!.value).toBe('off');
  });
});
