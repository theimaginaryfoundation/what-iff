import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { DEFAULT_RITUAL_VIEW_FILTERS } from '../helpers/ritual-filter.helpers';
import { RitualFilterBarComponent } from './ritual-filter-bar.component';

describe('RitualFilterBarComponent', () => {
  let fixture: ComponentFixture<RitualFilterBarComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [RitualFilterBarComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();

    fixture = TestBed.createComponent(RitualFilterBarComponent);
    fixture.componentRef.setInput('filters', { ...DEFAULT_RITUAL_VIEW_FILTERS });
  });

  it('creates', () => {
    expect(fixture.componentInstance).toBeTruthy();
  });

  it('renders every personality and hotkey option from the @for blocks', () => {
    fixture.componentRef.setInput('personalities', [
      { id: 'p1', label: 'Ada' },
      { id: 'p2', label: 'Grace' },
    ]);
    fixture.detectChanges();
    const selects = fixture.nativeElement.querySelectorAll('select') as NodeListOf<HTMLSelectElement>;

    expect(selects[0].options.length).toBe(3);
    expect(Array.from(selects[0].options).map(option => option.text)).toEqual([
      'All affinities',
      'Ada',
      'Grace',
    ]);
    expect(Array.from(selects[1].options).map(option => option.text)).toEqual([
      'All skills',
      'With hotkeys',
      'Without hotkeys',
    ]);
  });

  it('renders both loading labels', () => {
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('Filters auto-apply');

    fixture.componentRef.setInput('loading', true);
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('Refreshing skills...');
  });
});
