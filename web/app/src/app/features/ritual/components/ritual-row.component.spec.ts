import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { RitualRowComponent } from './ritual-row.component';
import { RitualRowVm } from '../helpers/ritual-vm.helpers';

describe('RitualRowComponent', () => {
  let fixture: ComponentFixture<RitualRowComponent>;
  let component: RitualRowComponent;

  const vm: RitualRowVm = {
    id: 'r-1',
    name: 'Daily summary',
    description: 'Summarize today',
    content: '...prompt...',
    hotkeys: 'ctrl+d',
    hasHotkey: true,
    personalityId: null,
    affinityLabel: 'All skills',
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-02T00:00:00Z',
    isSystem: false,
  };

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [RitualRowComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();
    fixture = TestBed.createComponent(RitualRowComponent);
    component = fixture.componentInstance;
    fixture.componentRef.setInput('ritual', vm);
    fixture.detectChanges();
  });

  it('renders hotkey badge with aria label', () => {
    const host = fixture.nativeElement as HTMLElement;
    const badge = host.querySelector('[aria-label^="Hotkey"]');
    expect(badge).not.toBeNull();
    expect(badge?.textContent).toContain('Ctrl');
  });

  it('shows locked state for system rituals', () => {
    fixture.componentRef.setInput('ritual', { ...vm, isSystem: true });
    fixture.detectChanges();
    const host = fixture.nativeElement as HTMLElement;
    expect(host.querySelector('[aria-label="Read-only system skill"]')).not.toBeNull();
  });
});
