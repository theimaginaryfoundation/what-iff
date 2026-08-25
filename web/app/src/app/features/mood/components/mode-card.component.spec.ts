import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { ModeCardComponent } from './mode-card.component';
import { ModeCardVm } from '../helpers/mode-vm.helpers';

describe('ModeCardComponent', () => {
  function makeCard(): ModeCardVm {
    return {
      mood: {
        id: 'mode-1',
        name: 'Focused',
        description: 'desc',
        prompt_snippet: '',
        image_ids: [],
        ritual_ids: [],
        personality_ids: ['p1'],
        created_at: '',
        updated_at: '',
      },
      title: 'Focused',
      description: 'desc',
      toolsSilencedLabel: '0 tools silenced',
      skillsLabel: 'All skills on',
      jobsLabel: 'Jobs on',
      personalities: [
        {
          id: 'p1',
          name: 'Ada',
          accentColor: '#aa5500',
          coverUrl: null,
          initials: 'A',
        },
      ],
    };
  }

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [ModeCardComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();
  });

  it('emits edit with mood id', () => {
    const fixture = TestBed.createComponent(ModeCardComponent);
    fixture.componentRef.setInput('card', makeCard());
    fixture.componentRef.setInput('isAssociationPickerOpen', false);
    fixture.componentRef.setInput('associationPickerQuery', '');
    fixture.componentRef.setInput('associationDropdownOpen', false);
    fixture.componentRef.setInput('associationOptions', []);

    let emittedId = '';
    fixture.componentInstance.edit.subscribe(id => {
      emittedId = id;
    });

    fixture.detectChanges();
    const button = fixture.nativeElement.querySelector('.mode-card__actions button');
    button.click();
    expect(emittedId).toBe('mode-1');
  });

  it('emits add association payload from picker', () => {
    const fixture = TestBed.createComponent(ModeCardComponent);
    fixture.componentRef.setInput('card', makeCard());
    fixture.componentRef.setInput('isAssociationPickerOpen', true);
    fixture.componentRef.setInput('associationPickerQuery', '');
    fixture.componentRef.setInput('associationDropdownOpen', true);
    fixture.componentRef.setInput('associationOptions', [
      {
        id: 'p2',
        name: 'Vera',
        accentColor: '#cc6600',
        coverUrl: null,
        initials: 'V',
      },
    ]);

    let emittedMoodId = '';
    let emittedPersonalityId = '';
    fixture.componentInstance.addAssociation.subscribe((event: any) => {
      emittedMoodId = event.moodId;
      emittedPersonalityId = event.personalityId;
    });

    fixture.detectChanges();
    const optionButton = fixture.nativeElement.querySelector('.mode-card__personality-option');
    optionButton.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    expect(emittedMoodId).toBe('mode-1');
    expect(emittedPersonalityId).toBe('p2');
  });
});
