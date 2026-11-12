import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { ThreadSandboxedChipComponent } from './thread-sandboxed-chip.component';

describe('ThreadSandboxedChipComponent', () => {
  let fixture: ComponentFixture<ThreadSandboxedChipComponent>;
  const host = () => fixture.nativeElement as HTMLElement;

  function render(sandboxed: boolean | null | undefined): void {
    fixture.componentRef.setInput('sandboxed', sandboxed);
    fixture.detectChanges();
  }

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [ThreadSandboxedChipComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();
    fixture = TestBed.createComponent(ThreadSandboxedChipComponent);
  });

  it.each([undefined, null] as const)('is hidden while the state is unknown (%s)', sandboxed => {
    render(sandboxed);
    expect(host().querySelector('button')).toBeNull();
  });

  it('shows a muted "Not sandboxed" chip, so the state is always visible', () => {
    render(false);
    const chip = host().querySelector('button') as HTMLButtonElement;
    expect(chip.textContent).toContain('Not sandboxed');
    expect(chip.classList.contains('chip--open')).toBe(true);
    expect(chip.getAttribute('aria-label')).toContain('Not sandboxed');
    expect(chip.getAttribute('aria-label')).toContain('Open sandbox settings');
  });

  it('shows a Sandboxed chip when sandboxed', () => {
    render(true);
    const chip = host().querySelector('button') as HTMLButtonElement;
    expect(chip.textContent).toContain('Sandboxed');
    expect(chip.getAttribute('aria-label')).toContain('Sandboxed thread');
  });

  it('asks to open sandbox settings when clicked', () => {
    render(true);
    const spy = vi.spyOn(fixture.componentInstance.openSettings, 'emit');
    (host().querySelector('button') as HTMLButtonElement).click();
    expect(spy).toHaveBeenCalledTimes(1);
  });
});
