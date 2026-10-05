import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';

import { MemorySensitivityBadgeComponent } from './memory-sensitivity-badge.component';

describe('MemorySensitivityBadgeComponent', () => {
  async function render(sensitivity: 'public' | 'personal' | 'sensitive'): Promise<HTMLElement> {
    TestBed.resetTestingModule();
    await TestBed.configureTestingModule({
      imports: [MemorySensitivityBadgeComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();
    const fixture = TestBed.createComponent(MemorySensitivityBadgeComponent);
    fixture.componentRef.setInput('sensitivity', sensitivity);
    fixture.detectChanges();
    return (fixture.nativeElement as HTMLElement).querySelector('.sensitivity-badge') as HTMLElement;
  }

  it.each([
    ['public', 'Public'],
    ['personal', 'Personal'],
    ['sensitive', 'Sensitive'],
  ] as const)('renders %s as "%s" with a level data attribute for styling', async (level, label) => {
    const badge = await render(level);
    expect(badge.textContent?.trim()).toBe(label);
    expect(badge.getAttribute('data-sensitivity')).toBe(level);
  });
});
