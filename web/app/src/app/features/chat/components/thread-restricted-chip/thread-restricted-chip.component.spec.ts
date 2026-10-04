import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { MemorySensitivity } from '../../../../core/models/memory.model';
import { ThreadRestrictedChipComponent } from './thread-restricted-chip.component';

describe('ThreadRestrictedChipComponent', () => {
  let fixture: ComponentFixture<ThreadRestrictedChipComponent>;
  const host = () => fixture.nativeElement as HTMLElement;

  function render(limit: MemorySensitivity | null | undefined): void {
    fixture.componentRef.setInput('limit', limit);
    fixture.detectChanges();
  }

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [ThreadRestrictedChipComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();
    fixture = TestBed.createComponent(ThreadRestrictedChipComponent);
  });

  it.each([undefined, null, 'sensitive'] as const)('is hidden when the limit is %s', limit => {
    render(limit);
    expect(host().querySelector('button')).toBeNull();
  });

  it.each(['personal', 'public'] as const)('shows a Restricted chip at limit %s', limit => {
    render(limit);
    const chip = host().querySelector('button') as HTMLButtonElement;
    expect(chip.textContent).toContain('Restricted');
    expect(chip.getAttribute('aria-label')).toContain('Restricted thread');
  });

  it('names the current access in its accessible label', () => {
    render('public');
    expect(host().querySelector('button')!.getAttribute('aria-label')).toContain('Public memories only');
  });

  it('asks to open memory access settings when clicked', () => {
    render('personal');
    const spy = vi.spyOn(fixture.componentInstance.openSettings, 'emit');
    (host().querySelector('button') as HTMLButtonElement).click();
    expect(spy).toHaveBeenCalledTimes(1);
  });
});
