import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { SetSensitivityModalComponent } from './set-sensitivity-modal.component';

describe('SetSensitivityModalComponent', () => {
  let fixture: ComponentFixture<SetSensitivityModalComponent>;
  let host: HTMLElement;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [SetSensitivityModalComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();
    fixture = TestBed.createComponent(SetSensitivityModalComponent);
    host = fixture.nativeElement as HTMLElement;
  });

  function open(count: number): void {
    fixture.componentRef.setInput('count', count);
    fixture.componentRef.setInput('open', true);
    fixture.detectChanges();
  }

  const confirmButton = () =>
    Array.from(host.querySelectorAll('button')).find(
      b => b.textContent?.includes('Set to') || b.textContent?.includes('Saving'),
    ) as HTMLButtonElement;

  it('renders nothing while closed', () => {
    fixture.detectChanges();
    expect(host.querySelector('[role="dialog"]')).toBeNull();
  });

  it('pluralizes the title and offers the three levels with Personal preselected', () => {
    open(3);
    expect(host.querySelector('#set-sensitivity-title')?.textContent).toContain('Set sensitivity for 3 memories');
    const radios = Array.from(host.querySelectorAll('input[type="radio"]')) as HTMLInputElement[];
    expect(radios.map(r => r.value)).toEqual(['public', 'personal', 'sensitive']);
    expect(radios.find(r => r.checked)?.value).toBe('personal');
    expect(confirmButton().textContent).toContain('Set to Personal');
  });

  it('uses the singular for one memory', () => {
    open(1);
    expect(host.querySelector('#set-sensitivity-title')?.textContent).toContain('1 memory');
  });

  it('emits the chosen level on confirm', () => {
    open(2);
    const spy = vi.spyOn(fixture.componentInstance.confirm, 'emit');
    (host.querySelector('input[value="sensitive"]') as HTMLInputElement).click();
    fixture.detectChanges();
    expect(confirmButton().textContent).toContain('Set to Sensitive');
    confirmButton().click();
    expect(spy).toHaveBeenCalledWith('sensitive');
  });

  it('emits cancel and disables the confirm button while saving', () => {
    open(2);
    const cancelSpy = vi.spyOn(fixture.componentInstance.cancel, 'emit');
    (Array.from(host.querySelectorAll('button')).find(b => b.textContent?.trim() === 'Cancel') as HTMLButtonElement).click();
    expect(cancelSpy).toHaveBeenCalled();

    fixture.componentRef.setInput('saving', true);
    fixture.detectChanges();
    expect(confirmButton().disabled).toBe(true);
    expect(confirmButton().textContent).toContain('Saving');
  });

  it('resets to Personal each time it reopens', () => {
    open(2);
    (host.querySelector('input[value="public"]') as HTMLInputElement).click();
    fixture.detectChanges();
    fixture.componentRef.setInput('open', false);
    fixture.detectChanges();
    fixture.componentRef.setInput('open', true);
    fixture.detectChanges();
    expect((host.querySelector('input[value="personal"]') as HTMLInputElement).checked).toBe(true);
  });
});
