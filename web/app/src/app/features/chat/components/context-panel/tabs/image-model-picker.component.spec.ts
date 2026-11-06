import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { ImageModelPickerComponent } from './image-model-picker.component';

describe('ImageModelPickerComponent', () => {
  let fixture: ComponentFixture<ImageModelPickerComponent>;

  beforeEach(async () => {
    // The picker persists its choice; start every test from the stored-nothing default.
    window.localStorage.removeItem('whatiff.defaultImageModel');
    await TestBed.configureTestingModule({
      imports: [ImageModelPickerComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();
    fixture = TestBed.createComponent(ImageModelPickerComponent);
    fixture.detectChanges();
  });

  afterEach(() => window.localStorage.removeItem('whatiff.defaultImageModel'));

  const el = (): HTMLElement => fixture.nativeElement;
  const trigger = () => el().querySelector('[data-testid="image-model-trigger"]') as HTMLButtonElement;
  const options = () => Array.from(el().querySelectorAll('[role="option"]')) as HTMLElement[];

  function openList(): void {
    trigger().click();
    fixture.detectChanges();
  }

  it('is a closed dropdown showing only the current default with its pill, and no field label', () => {
    expect(trigger().textContent).toContain('GPT Image 1');
    expect(trigger().querySelector('.pill--on')?.textContent?.trim()).toBe('default');
    expect(trigger().getAttribute('aria-expanded')).toBe('false');
    expect(el().querySelector('[role="listbox"]')).toBeNull();
    expect(el().querySelector('label')).toBeNull();
    expect(el().textContent).not.toContain('Gemini Flash Image');
  });

  it('opens a listbox with every model, the default pill on the current one and a grayed pill on the rest', () => {
    openList();

    expect(trigger().getAttribute('aria-expanded')).toBe('true');
    expect(options().length).toBe(3);
    expect(options()[0].getAttribute('aria-selected')).toBe('true');
    expect(options()[0].querySelector('.pill--on')).not.toBeNull();
    expect(options()[1].querySelector('.pill--off')).not.toBeNull();
    expect(options()[2].querySelector('.pill--off')).not.toBeNull();
  });

  it('makes a clicked model the default and closes', () => {
    openList();
    options()[1].click();
    fixture.detectChanges();

    expect(el().querySelector('[role="listbox"]')).toBeNull();
    expect(trigger().textContent).toContain('GPT Image 1 Mini');
    expect(trigger().getAttribute('aria-expanded')).toBe('false');
  });

  it('supports the keyboard: arrows move, Enter chooses, Escape closes without choosing', () => {
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
    expect(trigger().textContent).toContain('GPT Image 1');

    openList();
    const reopened = el().querySelector('[role="listbox"]') as HTMLElement;
    reopened.dispatchEvent(new KeyboardEvent('keydown', { key: 'End', cancelable: true }));
    reopened.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', cancelable: true }));
    fixture.detectChanges();
    expect(trigger().textContent).toContain('Gemini Flash Image');
  });

  it('restores a stored default on creation', () => {
    window.localStorage.setItem('whatiff.defaultImageModel', 'gemini-flash-image');
    const restored = TestBed.createComponent(ImageModelPickerComponent);
    restored.detectChanges();
    const restoredTrigger = restored.nativeElement.querySelector('[data-testid="image-model-trigger"]') as HTMLButtonElement;
    expect(restoredTrigger.textContent).toContain('Gemini Flash Image');
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

    it('names the trigger with the current model, and reads "default" only for the selected option', () => {
      openList();
      expect(trigger().getAttribute('aria-label')).toBe('Default image model: GPT Image 1');
      expect(options()[0].getAttribute('aria-label')).toBe('GPT Image 1, default');
      expect(options()[1].getAttribute('aria-label')).toBe('GPT Image 1 Mini');
      expect(el().querySelectorAll('.pill[aria-hidden="true"]').length).toBe(4);
    });
  });
});
