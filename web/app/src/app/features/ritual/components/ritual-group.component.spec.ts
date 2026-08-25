import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { RitualGroupComponent } from './ritual-group.component';

describe('RitualGroupComponent', () => {
  let fixture: ComponentFixture<RitualGroupComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [RitualGroupComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();
    fixture = TestBed.createComponent(RitualGroupComponent);
    fixture.componentRef.setInput('group', {
      id: 'all-skills',
      label: 'All skills',
      rituals: [
        {
          id: 'r-1',
          name: 'Daily summary',
          description: 'Summarize today',
          content: '...',
          hotkeys: '',
          hasHotkey: false,
          personalityId: null,
          affinityLabel: 'All skills',
          createdAt: '2026-01-01T00:00:00Z',
          updatedAt: '2026-01-01T00:00:00Z',
          isSystem: false,
        },
      ],
    });
    fixture.componentRef.setInput('selectedIds', []);
    fixture.detectChanges();
  });

  it('renders an h3 heading for group title', () => {
    const host = fixture.nativeElement as HTMLElement;
    const heading = host.querySelector('h3');
    expect(heading?.textContent?.trim()).toBe('All skills');
  });
});
