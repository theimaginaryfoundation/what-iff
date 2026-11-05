import { HttpErrorResponse } from '@angular/common/http';
import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { BehaviorSubject, of, throwError } from 'rxjs';

import { Model } from '../../../core/models/model.model';
import { UtilityModels } from '../../../core/models/utility-model.model';
import { ModelService } from '../../../core/services/model.service';
import { UtilityModelsService } from '../../../core/services/utility-models.service';
import { BackgroundModelsComponent } from './background-models.component';

const MODELS: Model[] = [
  { id: 'luna', name: 'gpt-5.6-luna', display_name: 'GPT-5.6 Luna', description: '', tool_support: true },
  { id: 'gpt51', name: 'gpt-5.1', display_name: 'GPT-5.1', description: '', tool_support: true },
  { id: 'qwen', name: 'qwen3:0.6b', display_name: 'Qwen3 0.6B', description: '', tool_support: true, connection_id: 'c-ollama', connection_name: 'Ollama' },
];

function utility(overrides: Partial<Record<'memory' | 'simple' | 'personality', { account?: string; deployment?: string }>> = {}): UtilityModels {
  const builtIn = { memory: 'gpt-5.6-luna', simple: 'gpt-5.6-luna', personality: 'gpt-5.1' } as const;
  return {
    accounts_choose: true,
    roles: (['memory', 'simple', 'personality'] as const).map(role => ({
      role,
      account_model_id: overrides[role]?.account ?? null,
      deployment_model_id: overrides[role]?.deployment ?? null,
      built_in_model: builtIn[role],
    })),
  };
}

describe('BackgroundModelsComponent', () => {
  let service: { get: ReturnType<typeof vi.fn>; choose: ReturnType<typeof vi.fn>; clear: ReturnType<typeof vi.fn> };
  let models$: BehaviorSubject<Model[]>;
  let fixture: ComponentFixture<BackgroundModelsComponent>;

  async function render(initial: UtilityModels): Promise<HTMLElement> {
    service.get.mockReturnValue(of(initial));
    fixture = TestBed.createComponent(BackgroundModelsComponent);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  function select(el: HTMLElement, role: string): HTMLSelectElement {
    return el.querySelector(`[data-testid="background-model-${role}"]`) as HTMLSelectElement;
  }

  async function change(el: HTMLElement, role: string, value: string): Promise<void> {
    const s = select(el, role);
    s.value = value;
    s.dispatchEvent(new Event('change'));
    await fixture.whenStable();
    fixture.detectChanges();
  }

  beforeEach(async () => {
    service = { get: vi.fn(), choose: vi.fn(), clear: vi.fn() };
    models$ = new BehaviorSubject<Model[]>(MODELS);
    await TestBed.configureTestingModule({
      imports: [BackgroundModelsComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: UtilityModelsService, useValue: service },
        { provide: ModelService, useValue: { models$, getModels: vi.fn(() => of(MODELS)) } },
      ],
    }).compileComponents();
  });

  it('lists each role with what it covers, defaulting to the built-in model by name', async () => {
    const el = await render(utility());
    expect(Array.from(el.querySelectorAll('.background-models__label')).map(l => l.textContent?.trim())).toEqual([
      'Memory and summaries',
      'Simple tasks',
      'Personality generation',
    ]);
    const memory = select(el, 'memory');
    expect(memory.value).toBe('');
    expect(memory.options[0].textContent?.trim()).toBe('Default (GPT-5.6 Luna)');
    expect(select(el, 'personality').options[0].textContent?.trim()).toBe('Default (GPT-5.1)');
    expect(Array.from(memory.options).map(o => o.textContent?.trim())).toContain('Qwen3 0.6B · Ollama');
  });

  it("names the deployment's choice as the default, and selects the account's own", async () => {
    const el = await render(utility({ simple: { deployment: 'gpt51' }, memory: { account: 'qwen' } }));
    expect(select(el, 'simple').options[0].textContent?.trim()).toBe('Default (GPT-5.1)');
    expect(select(el, 'memory').value).toBe('qwen');
  });

  it('saves a choice, and clears it when Default is picked', async () => {
    const el = await render(utility());
    service.choose.mockReturnValue(of(utility({ memory: { account: 'qwen' } })));
    await change(el, 'memory', 'qwen');
    expect(service.choose).toHaveBeenCalledWith('memory', 'qwen');
    expect(select(el, 'memory').value).toBe('qwen');

    service.clear.mockReturnValue(of(utility()));
    await change(el, 'memory', '');
    expect(service.clear).toHaveBeenCalledWith('memory');
    expect(select(el, 'memory').value).toBe('');
  });

  it('puts the select back and says why when a save fails', async () => {
    const el = await render(utility());
    service.choose.mockReturnValue(throwError(() => new HttpErrorResponse({ status: 400, error: { message: 'That model is not available' } })));
    await change(el, 'simple', 'qwen');
    expect(select(el, 'simple').value).toBe('');
    expect(el.querySelector('[data-testid="background-models-error"]')?.textContent).toContain('That model is not available');
  });

  it('says when a chosen model is no longer available, and what runs instead', async () => {
    const el = await render(utility({ memory: { account: 'turned-off' } }));
    const memory = select(el, 'memory');
    expect(memory.selectedOptions[0].textContent?.trim()).toBe('Chosen model is unavailable; using Default (GPT-5.6 Luna)');
  });

  it('shows nothing where the deployment chooses the models', async () => {
    const el = await render({ ...utility(), accounts_choose: false });
    expect(el.querySelector('[data-testid="background-models"]')).toBeNull();
  });
});
