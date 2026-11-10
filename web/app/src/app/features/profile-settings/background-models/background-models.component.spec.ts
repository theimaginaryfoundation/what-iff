import { HttpErrorResponse } from '@angular/common/http';
import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { BehaviorSubject, of, throwError } from 'rxjs';

import { Model } from '../../../core/models/model.model';
import { UtilityChoice, UtilityModels, UtilityRole } from '../../../core/models/utility-model.model';
import { ModelService } from '../../../core/services/model.service';
import { UtilityModelsService } from '../../../core/services/utility-models.service';
import { BackgroundModelsComponent, OFF_VALUE } from './background-models.component';

const MODELS: Model[] = [
  { id: 'luna', name: 'gpt-5.6-luna', display_name: 'GPT-5.6 Luna', description: '', tool_support: true, provider: 'openai', reasoning_support: 'optional' },
  { id: 'haiku', name: 'claude-haiku-4-5', display_name: 'Claude Haiku 4.5', description: '', tool_support: true, provider: 'anthropic', reasoning_support: 'optional' },
  {
    id: 'qwen',
    name: 'qwen3:0.6b',
    display_name: 'Qwen3 0.6B',
    description: '',
    tool_support: true,
    provider: 'ollama',
    connection_id: 'c-ollama',
    connection_name: 'Ollama',
    reasoning_support: 'none',
  },
];

const ROLES: UtilityRole[] = ['memory', 'memory_merge', 'simple', 'personality'];

function model(id: string, extra: Partial<UtilityChoice> = {}): UtilityChoice {
  return { off: false, model_id: id, reasoning_effort: null, service_tier: null, ...extra };
}
const OFF: UtilityChoice = { off: true, model_id: null, reasoning_effort: null, service_tier: null };

function utility(account: Partial<Record<UtilityRole, UtilityChoice>> = {}, deployment: Partial<Record<UtilityRole, UtilityChoice>> = {}): UtilityModels {
  return {
    accounts_choose: true,
    roles: ROLES.map(role => ({ role, account: account[role] ?? null, deployment: deployment[role] ?? null })),
  };
}

describe('BackgroundModelsComponent', () => {
  let service: { get: ReturnType<typeof vi.fn>; choose: ReturnType<typeof vi.fn>; switchOff: ReturnType<typeof vi.fn> };
  let models$: BehaviorSubject<Model[]>;
  let modelService: { models$: BehaviorSubject<Model[]>; getModels: ReturnType<typeof vi.fn> };
  let fixture: ComponentFixture<BackgroundModelsComponent>;

  async function render(initial: UtilityModels): Promise<HTMLElement> {
    service.get.mockReturnValue(of(initial));
    fixture = TestBed.createComponent(BackgroundModelsComponent);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  function select(el: HTMLElement, control: 'model' | 'effort' | 'tier', role: string): HTMLSelectElement | null {
    return el.querySelector(`[data-testid="background-${control}-${role}"]`) as HTMLSelectElement | null;
  }

  async function change(el: HTMLElement, control: 'model' | 'effort' | 'tier', role: string, value: string): Promise<void> {
    const s = select(el, control, role)!;
    s.value = value;
    s.dispatchEvent(new Event('change'));
    await fixture.whenStable();
    fixture.detectChanges();
  }

  function note(el: HTMLElement, role: string, kind: string): string | undefined {
    return el.querySelector(`[data-testid="background-role-${role}"] [data-testid="background-role-${kind}"]`)?.textContent?.trim();
  }

  beforeEach(async () => {
    service = { get: vi.fn(), choose: vi.fn(), switchOff: vi.fn() };
    models$ = new BehaviorSubject<Model[]>(MODELS);
    modelService = { models$, getModels: vi.fn(() => of(MODELS)) };
    await TestBed.configureTestingModule({
      imports: [BackgroundModelsComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: UtilityModelsService, useValue: service },
        { provide: ModelService, useValue: modelService },
      ],
    }).compileComponents();
  });

  it('lists every role, and says what stops while one has nothing chosen', async () => {
    const el = await render(utility());
    expect(Array.from(el.querySelectorAll('.background-models__label')).map(l => l.textContent?.trim())).toEqual([
      'Memory and summaries',
      'Memory tidying',
      'Simple tasks',
      'Personality generation',
    ]);
    const memory = select(el, 'model', 'memory')!;
    expect(memory.selectedOptions[0].textContent?.trim()).toBe('Choose a model');
    expect(Array.from(memory.options).map(o => o.textContent?.trim())).toEqual(['Choose a model', 'GPT-5.6 Luna', 'Claude Haiku 4.5', 'Qwen3 0.6B · Ollama', 'Off']);
    expect(note(el, 'memory', 'not-set')).toContain("New chats don't add to memory");
  });

  it('offers no way back to nothing once a role is chosen, and says what Off stops', async () => {
    const el = await render(utility({ memory: model('qwen'), simple: OFF }));
    const memory = select(el, 'model', 'memory')!;
    expect(memory.value).toBe('qwen');
    expect(Array.from(memory.options).map(o => o.value)).not.toContain('');
    expect(select(el, 'model', 'simple')!.value).toBe(OFF_VALUE);
    expect(note(el, 'simple', 'off')).toContain("Threads aren't named");
  });

  it("says when the deployment's choice stands in, or the deployment switched the role off", async () => {
    const el = await render(utility({}, { simple: model('luna'), personality: OFF }));
    expect(note(el, 'simple', 'deployment')).toContain("This server's choice is used");
    expect(note(el, 'personality', 'deployment-off')).toContain('Off on this server.');
  });

  it('saves a model, and switches a role off', async () => {
    const el = await render(utility());
    service.choose.mockReturnValue(of(utility({ memory: model('qwen') })));
    await change(el, 'model', 'memory', 'qwen');
    expect(service.choose).toHaveBeenCalledWith('memory', { model_id: 'qwen', reasoning_effort: null, service_tier: null });
    expect(select(el, 'model', 'memory')!.value).toBe('qwen');

    service.switchOff.mockReturnValue(of(utility({ memory: OFF })));
    await change(el, 'model', 'memory', OFF_VALUE);
    expect(service.switchOff).toHaveBeenCalledWith('memory');
    expect(note(el, 'memory', 'off')).toBeTruthy();
  });

  it("offers thinking for a model that takes it, and speed only on OpenAI's own API", async () => {
    // Qwen is a connection model: its effort can't be set from here, whatever its support says.
    const el = await render(
      utility({ memory: model('luna'), simple: model('haiku'), personality: model('qwen') }),
    );
    models$.next(MODELS.map(m => (m.id === 'qwen' ? { ...m, reasoning_support: 'optional' } : m)));
    fixture.detectChanges();
    expect(select(el, 'effort', 'memory')).not.toBeNull();
    expect(select(el, 'tier', 'memory')).not.toBeNull();
    expect(select(el, 'effort', 'simple')).not.toBeNull();
    expect(select(el, 'tier', 'simple')).toBeNull();
    expect(select(el, 'effort', 'personality')).toBeNull();
    expect(select(el, 'tier', 'personality')).toBeNull();
  });

  it('saves thinking and speed with the model, and keeps them when the model can take them', async () => {
    const el = await render(utility({ memory_merge: model('luna') }));
    service.choose.mockReturnValue(of(utility({ memory_merge: model('luna', { reasoning_effort: 'low' }) })));
    await change(el, 'effort', 'memory_merge', 'low');
    expect(service.choose).toHaveBeenLastCalledWith('memory_merge', { model_id: 'luna', reasoning_effort: 'low', service_tier: null });

    service.choose.mockReturnValue(of(utility({ memory_merge: model('luna', { reasoning_effort: 'low', service_tier: 'flex' }) })));
    await change(el, 'tier', 'memory_merge', 'flex');
    expect(service.choose).toHaveBeenLastCalledWith('memory_merge', { model_id: 'luna', reasoning_effort: 'low', service_tier: 'flex' });

    // Haiku takes thinking but no tier: the effort stays, the tier goes.
    service.choose.mockReturnValue(of(utility({ memory_merge: model('haiku', { reasoning_effort: 'low' }) })));
    await change(el, 'model', 'memory_merge', 'haiku');
    expect(service.choose).toHaveBeenLastCalledWith('memory_merge', { model_id: 'haiku', reasoning_effort: 'low', service_tier: null });
  });

  it('puts the select back and says why when a save fails', async () => {
    const el = await render(utility({ simple: model('luna') }));
    service.choose.mockReturnValue(throwError(() => new HttpErrorResponse({ status: 400, error: { message: 'That model is not available' } })));
    await change(el, 'model', 'simple', 'qwen');
    expect(select(el, 'model', 'simple')!.value).toBe('luna');
    expect(el.querySelector('[data-testid="background-models-error"]')?.textContent).toContain('That model is not available');

    await change(el, 'effort', 'simple', 'high');
    // The effort goes back too.
    expect(select(el, 'effort', 'simple')!.value).toBe('');
  });

  it('says where to add the key when the chosen model has none', async () => {
    const el = await render(utility());
    service.choose.mockReturnValue(
      throwError(
        () =>
          new HttpErrorResponse({
            status: 409,
            error: { code: 'provider_not_configured', message: 'No Anthropic API key is set.', details: { provider: 'anthropic' } },
          }),
      ),
    );
    await change(el, 'model', 'memory', 'haiku');
    expect(el.querySelector('[data-testid="background-models-error"]')?.textContent).toContain(
      'No Anthropic API key is set. Add one on the Providers tab, or choose another model.',
    );
  });

  it("says when a chosen model can't be used any more", async () => {
    const el = await render(utility({ memory: model('turned-off') }));
    expect(select(el, 'model', 'memory')!.selectedOptions[0].textContent?.trim()).toBe('Chosen model is unavailable');
    expect(note(el, 'memory', 'unavailable')).toContain('Choose another model.');
  });

  it('shows nothing where the deployment chooses the models', async () => {
    const el = await render({ ...utility(), accounts_choose: false });
    expect(el.querySelector('[data-testid="background-models"]')).toBeNull();
  });

  it("says so when the account's models can't be loaded", async () => {
    models$.next([]);
    modelService.getModels.mockReturnValue(throwError(() => new Error('offline')));
    const el = await render(utility());
    // ngOnInit awaits the roles, then the models, so the failure lands a tick after render.
    await fixture.whenStable();
    fixture.detectChanges();
    expect(el.querySelector('[data-testid="background-models-unloaded"]')?.textContent).toContain('only Off is offered');
    expect(Array.from(select(el, 'model', 'memory')!.options).map(o => o.textContent?.trim())).toEqual(['Choose a model', 'Off']);
  });
});
