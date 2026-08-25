import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { ModeCardVm } from '../helpers/mode-vm.helpers';
import { ModeGridComponent } from './mode-grid.component';

describe('ModeGridComponent', () => {
  let fixture: ComponentFixture<ModeGridComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [ModeGridComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();

    fixture = TestBed.createComponent(ModeGridComponent);
    setInputs([], false);
  });

  it('creates', () => {
    expect(fixture.componentInstance).toBeTruthy();
  });

  it('renders loading and empty branches', () => {
    setInputs([], true);
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('.mode-page__loading')).not.toBeNull();
    expect(fixture.nativeElement.textContent).toContain('Loading modes...');

    setInputs([], false);
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('.mode-page__empty')).not.toBeNull();
    expect(fixture.nativeElement.textContent).toContain('No modes found.');
  });

  it('renders one mode card for every card in the populated branch', () => {
    setInputs([card('mode-1', 'Focused'), card('mode-2', 'Creative')], false);
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelectorAll('app-mode-card').length).toBe(2);
    expect(fixture.nativeElement.textContent).toContain('Focused');
    expect(fixture.nativeElement.textContent).toContain('Creative');
  });

  function setInputs(cards: ModeCardVm[], isLoading: boolean): void {
    fixture.componentRef.setInput('cards', cards);
    fixture.componentRef.setInput('isLoading', isLoading);
    fixture.componentRef.setInput('emptyText', 'No modes found.');
    fixture.componentRef.setInput('associationPickerMoodId', null);
    fixture.componentRef.setInput('associationPickerQuery', '');
    fixture.componentRef.setInput('associationDropdownOpen', false);
    fixture.componentRef.setInput('associationOptionsByMoodId', {});
  }

  function card(id: string, name: string): ModeCardVm {
    return {
      mood: {
        id,
        name,
        description: `${name} description`,
        prompt_snippet: '',
        image_ids: [],
        ritual_ids: [],
        personality_ids: [],
        created_at: '',
        updated_at: '',
      },
      title: name,
      description: `${name} description`,
      toolsSilencedLabel: '0 silenced',
      skillsLabel: 'All skills on',
      jobsLabel: 'Jobs on',
      personalities: [],
    };
  }
});
