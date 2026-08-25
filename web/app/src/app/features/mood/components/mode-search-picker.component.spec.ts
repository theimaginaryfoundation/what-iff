import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { ModeSearchPickerComponent } from './mode-search-picker.component';

describe('ModeSearchPickerComponent', () => {
  let fixture: ComponentFixture<ModeSearchPickerComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [ModeSearchPickerComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();

    fixture = TestBed.createComponent(ModeSearchPickerComponent);
    fixture.componentRef.setInput('placeholder', 'Find a personality');
    fixture.componentRef.setInput('query', '');
    fixture.componentRef.setInput('dropdownOpen', false);
    fixture.componentRef.setInput('options', []);
    fixture.componentRef.setInput('emptyText', 'No personalities found.');
  });

  it('creates', () => {
    expect(fixture.componentInstance).toBeTruthy();
  });

  it('does not render the dropdown while closed', () => {
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('.mode-modal__dropdown')).toBeNull();
  });

  it('renders every option while open and emits the selected id', () => {
    fixture.componentRef.setInput('dropdownOpen', true);
    fixture.componentRef.setInput('options', [
      { id: 'p1', label: 'Ada' },
      { id: 'p2', label: 'Grace' },
    ]);
    let selected = '';
    fixture.componentInstance.selectOption.subscribe(id => (selected = id));
    fixture.detectChanges();
    const buttons = fixture.nativeElement.querySelectorAll('.mode-modal__dropdown-item') as NodeListOf<HTMLButtonElement>;

    expect(buttons.length).toBe(2);
    expect(fixture.nativeElement.textContent).toContain('Ada');
    expect(fixture.nativeElement.textContent).toContain('Grace');

    buttons[1].dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    expect(selected).toBe('p2');
  });

  it('renders the @empty branch when an open dropdown has no options', () => {
    fixture.componentRef.setInput('dropdownOpen', true);
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.mode-modal__dropdown-empty')).not.toBeNull();
    expect(fixture.nativeElement.textContent).toContain('No personalities found.');
  });
});
