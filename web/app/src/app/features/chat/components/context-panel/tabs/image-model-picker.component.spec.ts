import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { BehaviorSubject, Observable, of, throwError } from 'rxjs';

import { UserPreferences, UserPreferencesUpdateRequest } from '../../../../../core/models/user.model';
import { UserPreferencesService } from '../../../../../core/services/user-preferences.service';
import { ImageModelPickerComponent } from './image-model-picker.component';

/** The preferences service with its cache, a load counter and the patches it was asked to save. */
class PreferencesStub {
  readonly subject = new BehaviorSubject<UserPreferences | null>(null);
  readonly preferences$ = this.subject.asObservable();
  loads = 0;
  patches: Partial<UserPreferencesUpdateRequest>[] = [];
  failNextPatch = false;

  constructor(private stored: UserPreferences) {}

  getUserPreferences(): Observable<UserPreferences> {
    this.loads++;
    this.subject.next(this.stored);
    return of(this.stored);
  }

  patchUserPreferences(patch: Partial<UserPreferencesUpdateRequest>): Observable<UserPreferences> {
    this.patches.push(patch);
    if (this.failNextPatch) {
      this.failNextPatch = false;
      return throwError(() => new Error('save failed'));
    }
    this.stored = { ...this.stored, ...patch } as UserPreferences;
    this.subject.next(this.stored);
    return of(this.stored);
  }
}

const prefs = (over: Partial<UserPreferences> = {}): UserPreferences => ({
  user_id: 'user-1',
  default_model_id: 'model-1',
  image_generation_provider: 'openai',
  image_generation_providers: ['openai', 'google', 'zai'],
  ...over,
});

describe('ImageModelPickerComponent', () => {
  let fixture: ComponentFixture<ImageModelPickerComponent>;
  let service: PreferencesStub;

  async function create(stored: UserPreferences): Promise<void> {
    service = new PreferencesStub(stored);
    await TestBed.configureTestingModule({
      imports: [ImageModelPickerComponent],
      providers: [provideZonelessChangeDetection(), { provide: UserPreferencesService, useValue: service }],
    }).compileComponents();
    fixture = TestBed.createComponent(ImageModelPickerComponent);
    fixture.detectChanges();
    await fixture.whenStable();
  }

  beforeEach(async () => {
    await create(prefs());
  });

  afterEach(() => {
    // Choosing a model announces it; drop the live region so it doesn't leak into other specs.
    document.querySelectorAll('[data-testid="announcer"]').forEach(n => n.remove());
  });

  const el = (): HTMLElement => fixture.nativeElement;
  const trigger = () => el().querySelector('[data-testid="image-model-trigger"]') as HTMLButtonElement;
  const options = () => Array.from(el().querySelectorAll('[role="option"]')) as HTMLElement[];

  function openList(): void {
    trigger().click();
    fixture.detectChanges();
  }

  async function settle(): Promise<void> {
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
  }

  it('is a closed dropdown showing only the current default with its pill, and no field label', () => {
    expect(trigger().textContent).toContain('OpenAI (GPT Image)');
    expect(trigger().querySelector('.pill--on')?.textContent?.trim()).toBe('default');
    expect(trigger().getAttribute('aria-expanded')).toBe('false');
    expect(el().querySelector('[role="listbox"]')).toBeNull();
    expect(el().querySelector('label')).toBeNull();
    expect(el().textContent).not.toContain('Google (Gemini image)');
  });

  it('loads the preferences when nothing has loaded them yet', () => {
    expect(service.loads).toBe(1);
  });

  it('lists the vendors Settings offers, with the default pill on the current one and a grayed pill on the rest', () => {
    openList();

    expect(trigger().getAttribute('aria-expanded')).toBe('true');
    expect(options().map(o => o.querySelector('.picker__name')?.textContent?.trim())).toEqual([
      'OpenAI (GPT Image)',
      'Google (Gemini image)',
      'Z.ai (GLM-Image)',
    ]);
    expect(options()[0].getAttribute('aria-selected')).toBe('true');
    expect(options()[0].querySelector('.pill--on')).not.toBeNull();
    expect(options()[1].querySelector('.pill--off')).not.toBeNull();
    expect(options()[2].querySelector('.pill--off')).not.toBeNull();
  });

  it('saves a clicked vendor as the image_generation_provider preference and closes', async () => {
    openList();
    options()[1].click();
    await settle();

    expect(service.patches).toEqual([{ image_generation_provider: 'google' }]);
    expect(el().querySelector('[role="listbox"]')).toBeNull();
    expect(trigger().textContent).toContain('Google (Gemini image)');
    expect(trigger().getAttribute('aria-expanded')).toBe('false');
  });

  it('does not save when the current default is picked again', async () => {
    openList();
    options()[0].click();
    await settle();

    expect(service.patches).toEqual([]);
  });

  it('goes back to the saved vendor when the save fails', async () => {
    service.failNextPatch = true;
    openList();
    options()[2].click();
    await settle();

    expect(trigger().textContent).toContain('OpenAI (GPT Image)');
  });

  it('shows the vendor images really use when the stored one is no longer offered', async () => {
    TestBed.resetTestingModule();
    await create(prefs({ image_generation_provider: 'qwen', image_generation_providers: ['google', 'openai'] }));

    expect(trigger().textContent).toContain('Google (Gemini image)');
  });

  it('is hidden when there is no choice to make, like the Settings select', async () => {
    TestBed.resetTestingModule();
    await create(prefs({ image_generation_providers: ['openai'] }));

    expect(trigger()).toBeNull();
  });

  it('supports the keyboard: arrows move, Enter chooses, Escape closes without choosing', async () => {
    trigger().dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', cancelable: true }));
    fixture.detectChanges();
    const list = el().querySelector('[role="listbox"]') as HTMLElement;
    expect(list).not.toBeNull();

    list.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', cancelable: true }));
    fixture.detectChanges();
    expect(list.getAttribute('aria-activedescendant')).toBe(options()[1].id);

    list.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', cancelable: true }));
    fixture.detectChanges();
    expect(el().querySelector('[role="listbox"]')).toBeNull();
    expect(trigger().textContent).toContain('OpenAI (GPT Image)');

    openList();
    const reopened = el().querySelector('[role="listbox"]') as HTMLElement;
    reopened.dispatchEvent(new KeyboardEvent('keydown', { key: 'End', cancelable: true }));
    reopened.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', cancelable: true }));
    await settle();
    expect(trigger().textContent).toContain('Z.ai (GLM-Image)');
    expect(service.patches).toEqual([{ image_generation_provider: 'zai' }]);
  });

  it('stops listening for outside clicks once destroyed', () => {
    const remove = vi.spyOn(document, 'removeEventListener');
    fixture.destroy();
    expect(remove).toHaveBeenCalledWith('click', expect.any(Function), true);
    remove.mockRestore();
  });

  describe('accessibility', () => {
    it('moves focus into the list on open and back to the trigger on choose', async () => {
      document.body.appendChild(el());
      openList();
      await fixture.whenStable();
      expect(document.activeElement).toBe(el().querySelector('[role="listbox"]'));

      (el().querySelector('[role="listbox"]') as HTMLElement).dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Enter', cancelable: true }),
      );
      fixture.detectChanges();
      await fixture.whenStable();
      expect(document.activeElement).toBe(trigger());
      el().remove();
    });

    it('names the trigger with the current vendor, and reads "default" only for the selected option', () => {
      openList();
      expect(trigger().getAttribute('aria-label')).toBe('Default image model: OpenAI (GPT Image)');
      expect(options()[0].getAttribute('aria-label')).toBe('OpenAI (GPT Image), default');
      expect(options()[1].getAttribute('aria-label')).toBe('Google (Gemini image)');
      expect(el().querySelectorAll('.pill[aria-hidden="true"]').length).toBe(4);
    });
  });
});
