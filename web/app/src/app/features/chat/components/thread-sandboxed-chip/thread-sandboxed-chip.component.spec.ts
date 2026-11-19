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

  it('shows nothing for a thread that is not sandboxed', () => {
    render(false);
    expect(host().querySelector('button')).toBeNull();
  });

  it('shows a shield-only button when sandboxed', () => {
    render(true);
    const chip = host().querySelector('button') as HTMLButtonElement;
    expect(chip.textContent?.trim()).toBe('');
    expect(chip.querySelector('ui-shield-icon')).not.toBeNull();
    expect(chip.getAttribute('aria-label')).toContain('Sandboxed thread');
  });

  it('asks to remove the thread from the sandbox when clicked', () => {
    render(true);
    const spy = vi.spyOn(fixture.componentInstance.remove, 'emit');
    (host().querySelector('button') as HTMLButtonElement).click();
    expect(spy).toHaveBeenCalledTimes(1);
  });
});
