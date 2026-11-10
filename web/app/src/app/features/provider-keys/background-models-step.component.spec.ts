import { HttpErrorResponse } from '@angular/common/http';
import { Component, provideZonelessChangeDetection, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { BehaviorSubject, of, throwError } from 'rxjs';

import { Model } from '../../core/models/model.model';
import { UtilityModels } from '../../core/models/utility-model.model';
import { ModelService } from '../../core/services/model.service';
import { UtilityModelsService } from '../../core/services/utility-models.service';
import { BackgroundModelsStepComponent } from './background-models-step.component';

const MODELS: Model[] = [
  { id: 'luna', name: 'gpt-5.6-luna', display_name: 'GPT-5.6 Luna', description: '', tool_support: true },
  { id: 'mini', name: 'gpt-5.4-mini', display_name: 'GPT-5.4 mini', description: '', tool_support: true },
];

function utility(chosen: string[] = []): UtilityModels {
  return {
    accounts_choose: true,
    roles: (['memory', 'memory_merge', 'simple', 'personality'] as const).map(role => ({
      role,
      account: chosen.includes(role) ? { off: false, model_id: 'mini', reasoning_effort: null, service_tier: null } : null,
      deployment: null,
    })),
  };
}

@Component({
  standalone: true,
  imports: [BackgroundModelsStepComponent],
  template: `<app-background-models-step [utility]="utility()" [defaultModelId]="defaultModel()" (saved)="saved = $event" />`,
})
class HostComponent {
  utility = signal(utility());
  defaultModel = signal('luna');
  saved: UtilityModels | null = null;
}

describe('BackgroundModelsStepComponent', () => {
  let service: { choose: ReturnType<typeof vi.fn>; switchOff: ReturnType<typeof vi.fn> };
  let fixture: ComponentFixture<HostComponent>;

  function el(): HTMLElement {
    return fixture.nativeElement as HTMLElement;
  }

  function step(): BackgroundModelsStepComponent {
    return fixture.debugElement.children[0].componentInstance as BackgroundModelsStepComponent;
  }

  function select(role: string): HTMLSelectElement {
    return el().querySelector(`[data-testid="onboarding-background-model-${role}"]`) as HTMLSelectElement;
  }

  beforeEach(async () => {
    service = { choose: vi.fn(() => of(utility(['memory', 'memory_merge', 'simple', 'personality']))), switchOff: vi.fn(() => of(utility())) };
    await TestBed.configureTestingModule({
      imports: [HostComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: UtilityModelsService, useValue: service },
        { provide: ModelService, useValue: { models$: new BehaviorSubject(MODELS), getModels: vi.fn(() => of(MODELS)) } },
      ],
    }).compileComponents();
    fixture = TestBed.createComponent(HostComponent);
    fixture.detectChanges();
    await fixture.whenStable();
  });

  it('starts every role with nothing chosen on the default model, and offers Off', () => {
    for (const role of ['memory', 'memory_merge', 'simple', 'personality']) {
      expect(select(role).value).toBe('luna');
    }
    expect(Array.from(select('memory').options).map(o => o.textContent?.trim())).toEqual(['GPT-5.6 Luna', 'GPT-5.4 mini', 'Off']);
    expect(el().textContent).toContain('Background work can use a smaller, cheaper model.');
  });

  it('shows only the roles still to choose', async () => {
    fixture.componentInstance.utility.set(utility(['memory', 'simple']));
    fixture.detectChanges();
    expect(select('memory')).toBeNull();
    expect(select('memory_merge')).not.toBeNull();
    expect(select('personality')).not.toBeNull();
  });

  it('saves each role as picked, Off included, and hands back the roles after', async () => {
    select('simple').value = 'mini';
    select('simple').dispatchEvent(new Event('change'));
    select('memory_merge').value = 'off';
    select('memory_merge').dispatchEvent(new Event('change'));
    fixture.detectChanges();
    expect(el().querySelector('[data-testid="onboarding-background-off"]')?.textContent).toContain('Duplicates pile up');

    await step().save();
    expect(service.choose).toHaveBeenCalledWith('memory', { model_id: 'luna' });
    expect(service.choose).toHaveBeenCalledWith('simple', { model_id: 'mini' });
    expect(service.choose).toHaveBeenCalledWith('personality', { model_id: 'luna' });
    expect(service.switchOff).toHaveBeenCalledWith('memory_merge');
    expect(fixture.componentInstance.saved?.roles.every(r => r.account)).toBe(true);
  });

  it('says why and stops when a save fails, handing back nothing', async () => {
    service.choose.mockReturnValueOnce(
      throwError(() => new HttpErrorResponse({ status: 409, error: { code: 'provider_not_configured', message: 'No Anthropic API key is set.' } })),
    );
    await step().save();
    fixture.detectChanges();
    expect(el().querySelector('[data-testid="background-models-step-error"]')?.textContent).toContain(
      'No Anthropic API key is set. Choose another model, or add the key in Settings.',
    );
    expect(service.choose).toHaveBeenCalledTimes(1);
    expect(fixture.componentInstance.saved).toBeNull();
  });

  it("starts on the first of the account's models when the default is not among them", () => {
    fixture.componentInstance.defaultModel.set('gone');
    fixture.detectChanges();
    expect(select('memory').value).toBe('luna');
  });
});
