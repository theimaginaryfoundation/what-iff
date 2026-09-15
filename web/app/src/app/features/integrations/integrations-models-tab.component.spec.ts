import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { of } from 'rxjs';

import { Model } from '../../core/models/model.model';
import { ModelService } from '../../core/services/model.service';
import { UserPreferencesService } from '../../core/services/user-preferences.service';
import { IntegrationsModelsTabComponent } from './integrations-models-tab.component';

describe('IntegrationsModelsTabComponent', () => {
  let fixture: ComponentFixture<IntegrationsModelsTabComponent>;

  const model = (id: string): Model =>
    ({ id, name: id, display_name: id, provider: 'openai', description: '' }) as Model;

  async function render(ids: string[], hidden: string[]): Promise<void> {
    const modelService = {
      getAllModelsIncludingHidden: vi.fn().mockReturnValue(of(ids.map(model))),
    };
    const preferences = {
      getUserPreferences: vi.fn().mockReturnValue(of({ hidden_model_ids: hidden })),
      updateUserPreferences: vi.fn().mockReturnValue(of({ hidden_model_ids: hidden })),
    };

    await TestBed.configureTestingModule({
      imports: [IntegrationsModelsTabComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: ModelService, useValue: modelService },
        { provide: UserPreferencesService, useValue: preferences },
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(IntegrationsModelsTabComponent);
    fixture.detectChanges();
  }

  const checkboxes = (): HTMLInputElement[] => Array.from(fixture.nativeElement.querySelectorAll('input[type="checkbox"]'));

  it('locks the last visible model instead of letting the click bounce', async () => {
    await render(['a', 'b'], ['b']);

    const [a, b] = checkboxes();
    expect(a.checked).toBe(true);
    expect(a.disabled).toBe(true); // the only visible one
    expect(b.disabled).toBe(false); // hidden, so it can be shown
  });

  /**
   * The old failure: hiding down to one raised "Keep at least one model
   * visible." as a red alert, and nothing cleared it until a later successful
   * toggle — so it sat there for as long as the user stayed at one model, and
   * the only way out was going back up to two.
   */
  it('states the one-model rule as a hint, not a lingering error', async () => {
    await render(['a', 'b'], ['b']);

    expect(fixture.nativeElement.querySelector('[role="alert"]')).toBeNull();
    const note = fixture.nativeElement.querySelector('[role="note"]');
    expect(note?.textContent).toContain('One model stays visible');
  });

  it('drops the hint once more than one model is visible', async () => {
    await render(['a', 'b'], []);

    expect(fixture.nativeElement.querySelector('[role="note"]')).toBeNull();
    expect(checkboxes().every(c => !c.disabled)).toBe(true);
  });
});
